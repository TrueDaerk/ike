package app

// timeline.go wires the per-file Timeline (#1916): `file.timeline` merges the
// focused file's local-history snapshots (#1023) and the git commits that
// touched it into one chronological list in the modal shell — one place to
// answer "what happened to this file, committed or not". The list is
// keyboard-first and reuses the existing plumbing: the diff pane (#60) for
// every comparison and the local-history restore path for snapshots.
//
// The git half loads incrementally: the picker opens on the snapshots it
// already has (a synchronous store read) and the first `git log --follow`
// window arrives as a vcs.FileLogMsg; older windows load on demand while the
// selection walks toward the end of the list.

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/localhistory"
	"ike/internal/project"
	"ike/internal/timeline"
	"ike/internal/ui"
	"ike/internal/vcs"
)

// TimelineMsg runs file.timeline: show the focused file's merged history.
type TimelineMsg struct{}

// timelineHeading prefixes the shell heading, so the open-guard recognizes the
// picker however its body was last re-bound (the localHistoryOpen pattern).
const timelineHeading = "TIMELINE — "

// timelineGitPage is how many commits one `git log --follow` window loads.
// Small enough that a file with thousands of commits opens instantly, large
// enough that a normal file's history arrives in one window.
const timelineGitPage = 50

// timelineState is the open Timeline's data. The merged list is derived
// (Merge over local/git under the filter) and recomputed on every change, so
// the two halves stay independently reloadable.
type timelineState struct {
	path   string // absolute path of the file the list describes
	rel    string // its repo-relative path; empty when git has nothing to say
	root   string // repo root; empty when the file is not in a tracked repo
	local  []timeline.Entry
	git    []timeline.Entry
	merged []timeline.Entry
	sel    int
	filter timeline.Filter
	// mark is the entry pinned for a two-entry diff, identified by source and
	// hash so a re-merge (filter change, loaded git window) cannot stale it.
	mark    timeline.Entry
	marked  bool
	more    bool // older commits remain past what git returned so far
	loading bool // a git window is in flight
}

// timelineDiffMsg carries the resolved texts of one Timeline comparison back
// onto the update loop (blob reads and git calls run in the command).
type timelineDiffMsg struct {
	path       string
	leftTitle  string
	rightTitle string
	left       string
	right      string
	editable   bool // the right side is the live buffer
	err        error
}

// timelineRestoreMsg carries a snapshot's text back for the restore action.
type timelineRestoreMsg struct {
	path string
	at   time.Time
	text string
	err  error
}

// openTimeline collects the focused file's local history and starts the git
// half. It returns the command loading the first commit window (nil when the
// file has no git history to load).
func (m *Model) openTimeline() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "no file for the timeline")
		return nil
	}
	path := ed.Path()
	st := timelineState{
		path:   path,
		local:  timeline.FromSnapshots(m.lhStore.List(path)),
		filter: timeline.ParseFilter(m.configString("history.timeline_source")),
	}
	// The git half needs a repository that already tracks the file: an
	// untracked file has no commits, so the Timeline is local-only (#1916).
	if snap := m.vcs.snap; snap != nil && snap.Status(path) != vcs.StatusUntracked {
		if rel, err := filepath.Rel(snap.Root, path); err == nil && !strings.HasPrefix(rel, "..") {
			st.root, st.rel = snap.Root, filepath.ToSlash(rel)
		}
	}
	if len(st.local) == 0 && st.rel == "" {
		m.host.Notify(host.Info, "no timeline for "+baseName(path)+
			" — it has neither snapshots nor commits")
		return nil
	}
	m.tl = st
	m.tlPicker = true
	m.refreshTimeline()
	m.shell.Open()
	if st.rel == "" {
		return nil
	}
	m.tl.loading = true
	return vcs.FileLogCmd(st.root, st.rel, 0, timelineGitPage)
}

// configString reads one config value as a string, empty when unset.
func (m Model) configString(key string) string {
	if v, ok := m.host.Config().Get(key); ok {
		return v
	}
	return ""
}

// timelineFileLog lands one git window in the open Timeline. A message for
// another file (or arriving after the picker closed) is ignored.
func (m *Model) timelineFileLog(msg vcs.FileLogMsg) {
	if !m.tlPicker || msg.Path != m.tl.rel {
		return
	}
	m.tl.loading = false
	if msg.Err != nil {
		m.tl.more = false
		m.host.Notify(host.Warn, "timeline: "+msg.Err.Error())
		m.refreshTimeline()
		return
	}
	m.tl.git = append(m.tl.git, timeline.FromCommits(msg.Entries)...)
	m.tl.more = msg.HasMore
	m.refreshTimeline()
}

// loadMoreTimeline requests the next commit window, or nil when there is
// nothing more to load (or a request is already in flight).
func (m *Model) loadMoreTimeline() tea.Cmd {
	if !m.tl.more || m.tl.loading || m.tl.rel == "" {
		return nil
	}
	m.tl.loading = true
	m.refreshTimeline()
	return vcs.FileLogCmd(m.tl.root, m.tl.rel, len(m.tl.git), timelineGitPage)
}

// refreshTimeline re-merges the two halves under the current filter, keeps the
// selection in range and re-binds the shell body to THIS model copy (#1440):
// the root model is a value model, so a closure bound once at open time would
// keep rendering the open-time state.
func (m *Model) refreshTimeline() {
	m.tl.merged = timeline.Merge(m.tl.local, m.tl.git, m.tl.filter)
	if m.tl.sel >= len(m.tl.merged) {
		m.tl.sel = len(m.tl.merged) - 1
	}
	if m.tl.sel < 0 {
		m.tl.sel = 0
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: timelineHeading + baseName(m.tl.path),
		Body:    m.renderTimeline,
	})
}

// timelineOpen reports whether the shell currently shows the Timeline — the
// content check guards against another overlay having taken the shell over
// without the picker's own close path running (the pins pattern).
func (m Model) timelineOpen() bool {
	if !m.tlPicker || !m.shell.IsOpen() {
		return false
	}
	c, ok := m.shell.Content().(ui.ModelContent)
	return ok && strings.HasPrefix(c.Heading, timelineHeading)
}

// renderTimeline draws the merged list plus the key hints.
func (m *Model) renderTimeline() string {
	var b strings.Builder
	now := time.Now()
	for i, e := range m.tl.merged {
		sel, mark := "  ", " "
		if i == m.tl.sel {
			sel = "▍ "
		}
		if m.timelineIsMarked(e) {
			mark = "*"
		}
		fmt.Fprintf(&b, "%s%s%s %-9s %s\n", sel, mark, e.Source.Icon(),
			project.RelTime(e.Time, now), timelineDetail(e))
	}
	if len(m.tl.merged) == 0 {
		b.WriteString("  (nothing to show under this filter)\n")
	}
	if m.tl.loading {
		b.WriteString("\n  loading git history…\n")
	} else if m.tl.more {
		b.WriteString("\n  (older commits not loaded yet — L loads more)\n")
	}
	fmt.Fprintf(&b, "\nenter diff against buffer · m mark · d diff marked ↔ selected · "+
		"r restore snapshot · y copy hash · f filter (%s) · L load more · j/k move · esc close",
		m.tl.filter.Label())
	return b.String()
}

// timelineDetail renders the source-specific columns of one row: a snapshot
// shows its absolute time and its label where one was recorded, a commit its
// short hash, author and subject.
func timelineDetail(e timeline.Entry) string {
	if e.Source == timeline.Local {
		detail := e.Time.Local().Format("2006-01-02 15:04:05") + "  snapshot"
		if e.Label != "" {
			detail += " · " + e.Label
		}
		return detail
	}
	return fmt.Sprintf("%-9s %s %s", e.ShortHash, truncatePad(e.Author, 14), e.Subject)
}

// timelineIsMarked reports whether e is the entry pinned for a two-entry diff.
// Identity is source plus hash, so the mark survives re-merges.
func (m Model) timelineIsMarked(e timeline.Entry) bool {
	return m.tl.marked && m.tl.mark.Source == e.Source && m.tl.mark.Hash == e.Hash
}

// updateTimeline consumes every key while the Timeline is open: navigation,
// the entry actions, the source filter and incremental loading. Everything
// else is swallowed (the picker is modal).
func (m Model) updateTimeline(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.pickerNav(key, &m.tl.sel, len(m.tl.merged), m.refreshTimeline) {
		// Incremental loading (#1916): walking toward the end of the list
		// pulls the next commit window in before the user hits the bottom.
		if m.tl.sel >= len(m.tl.merged)-3 {
			// The command is taken into a local first: loadMoreTimeline
			// mutates m, and a return statement's operand order is
			// unspecified.
			cmd := m.loadMoreTimeline()
			return m, cmd
		}
		return m, nil
	}
	switch key {
	case "esc", "q":
		m.tlPicker = false
		m.shell.Close()
		return m, nil
	case "f":
		m.tl.filter = m.tl.filter.Next()
		m.refreshTimeline()
		return m, nil
	case "L":
		cmd := m.loadMoreTimeline()
		if cmd != nil {
			return m, cmd
		}
		m.host.Notify(host.Info, "the whole git history of this file is loaded")
		return m, nil
	}
	if len(m.tl.merged) == 0 {
		return m, nil
	}
	sel := m.tl.merged[m.tl.sel]
	switch key {
	case "enter":
		path := m.tl.path
		m.closeTimeline()
		return m, m.timelineDiffCmd(path, sel, timeline.Entry{}, true)
	case "m":
		if m.timelineIsMarked(sel) {
			m.tl.marked = false
		} else {
			m.tl.mark, m.tl.marked = sel, true
		}
		m.refreshTimeline()
		return m, nil
	case "d":
		if !m.tl.marked {
			m.host.Notify(host.Info, "mark an entry with m first — d diffs it against the selected one")
			return m, nil
		}
		if m.timelineIsMarked(sel) {
			m.host.Notify(host.Info, "select a second entry — the marked one cannot diff against itself")
			return m, nil
		}
		// The older entry goes left, so the diff reads forward in time.
		older, newer := m.tl.mark, sel
		if newer.Time.Before(older.Time) {
			older, newer = newer, older
		}
		path := m.tl.path
		m.closeTimeline()
		return m, m.timelineDiffCmd(path, older, newer, false)
	case "r":
		if sel.Source != timeline.Local {
			m.host.Notify(host.Info, "restore works on snapshots — a commit can be diffed (enter) or reverted via VCS")
			return m, nil
		}
		path := m.tl.path
		m.closeTimeline()
		return m, m.timelineRestoreCmd(path, sel)
	case "y":
		if sel.Source != timeline.Git {
			m.host.Notify(host.Info, "no commit hash on a local-history snapshot")
			return m, nil
		}
		m.copyToClipboard(sel.Hash)
		m.host.Notify(host.Info, "copied "+sel.ShortHash)
		return m, nil
	}
	return m, nil
}

// closeTimeline drops the picker off the shell (an action opening a pane must
// not leave the modal in front of it).
func (m *Model) closeTimeline() {
	m.tlPicker = false
	m.shell.Close()
}

// timelineDiffCmd resolves the texts of one comparison off the update loop:
// left is always an entry, right is either a second entry or — when buffer is
// set — the live buffer of path. Snapshot blobs come from the local-history
// store, commit blobs from `git show <hash>:<path at that commit>`.
func (m Model) timelineDiffCmd(path string, left, right timeline.Entry, buffer bool) tea.Cmd {
	store, root, name := m.lhStore, m.tl.root, baseName(path)
	// The buffer text is read here, on the loop, where the editors live.
	bufferText := readFileOrEmpty(path)
	if ed := m.editorForPath(path); ed != nil {
		bufferText = ed.Text()
	}
	return func() tea.Msg {
		out := timelineDiffMsg{
			path:      path,
			leftTitle: name + " @ " + timelineRevLabel(left),
			editable:  buffer,
		}
		if out.left, out.err = timelineEntryText(store, root, left); out.err != nil {
			return out
		}
		if buffer {
			out.rightTitle, out.right = name, bufferText
			return out
		}
		out.rightTitle = name + " @ " + timelineRevLabel(right)
		out.right, out.err = timelineEntryText(store, root, right)
		return out
	}
}

// timelineRestoreCmd reads a snapshot's text off the update loop for the
// restore action.
func (m Model) timelineRestoreCmd(path string, entry timeline.Entry) tea.Cmd {
	store, root := m.lhStore, m.tl.root
	return func() tea.Msg {
		text, err := timelineEntryText(store, root, entry)
		return timelineRestoreMsg{path: path, at: entry.Time, text: text, err: err}
	}
}

// timelineEntryText resolves one entry's content, normalized to the buffer's
// native form so both entry types diff against the buffer on equal terms.
func timelineEntryText(store *localhistory.Store, root string, e timeline.Entry) (string, error) {
	if e.Source == timeline.Local {
		data, err := store.Read(e.Hash)
		if err != nil {
			return "", fmt.Errorf("snapshot unreadable (pruned?): %w", err)
		}
		return normalizeBufferText(data)
	}
	content, err := vcs.RevContent(root, e.Hash, e.Path)
	if err != nil {
		return "", fmt.Errorf("commit blob unreadable: %w", err)
	}
	return normalizeBufferText([]byte(content))
}

// timelineRevLabel names an entry for a diff column title.
func timelineRevLabel(e timeline.Entry) string {
	if e.Source == timeline.Local {
		return project.RelTime(e.Time, time.Now())
	}
	return e.ShortHash
}

// openTimelineDiff lands a resolved comparison in the diff pane.
func (m *Model) openTimelineDiff(msg timelineDiffMsg) {
	if msg.err != nil {
		m.host.Notify(host.Warn, "timeline: "+msg.err.Error())
		return
	}
	m.openDiffTexts(msg.path, msg.leftTitle, msg.rightTitle, msg.left, msg.right, msg.editable)
}

// applyTimelineRestore restores a snapshot's text into the buffer through the
// local-history restore path (one undoable edit).
func (m *Model) applyTimelineRestore(msg timelineRestoreMsg) tea.Cmd {
	if msg.err != nil {
		m.host.Notify(host.Warn, "timeline: "+msg.err.Error())
		return nil
	}
	return m.restoreLocalHistory(msg.path, msg.at, msg.text)
}
