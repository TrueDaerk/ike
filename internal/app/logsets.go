package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/largefile"
	"ike/internal/logset"
	"ike/internal/pane"
	"ike/internal/watch"
)

// logsets.go opens a rotated log set as one chronological timeline (#1996).
// A rotation set — `app.log` plus the `app.log.1`, `app.log.2026-08-01`,
// `app.log.2.gz` next to it — is one log split across files by logrotate, and
// an investigation at the rotation boundary needs it as one buffer. The
// detection, ordering and merging live in internal/logset; this file is the
// command, the buffer install, the origin toast and the follow wiring.
//
// The merged buffer is read-only, like every other preview of content that has
// no writable home on disk (#1762/#1763), and its path is virtual in the same
// shape: `<stem>!merged/<name>`, whose tail is the set's own file name, so the
// language lookup and the log rendering resolve from it with no special casing.
//
// Merging reads files, so it runs off the Update loop as a command and lands
// as mergedLogMsg — a set of a dozen members must never stall a keystroke —
// and both large-file thresholds (#149) bound what it reads.

// OpenMergedLogMsg asks the root model to merge the rotated log set the focused
// buffer belongs to and open it as one timeline (#1996). Dispatched by the
// log.openRotatedSet command; on a merged buffer it re-merges the set.
type OpenMergedLogMsg struct{}

// mergedLogMsg carries an assembled timeline back to the Update loop. refollow
// marks the re-merge of an already open, following view (a rotation moved the
// newest member) as opposed to a fresh open.
type mergedLogMsg struct {
	set      logset.Set
	merged   logset.Merged
	err      error
	refollow bool
}

// mergedMarker separates the set's stem from the synthetic member name in the
// virtual path of a merged timeline: "/var/log/app.log!merged/app.log". The
// tail is a file name, so `filepath.Ext` — and with it the language, the
// highlighting and the tab label — resolves exactly as for the live log, while
// nothing on disk answers to the whole string, which is what keeps the buffer
// read-only.
const mergedMarker = entrySep + "merged/"

// mergedLogPath is the virtual path of the merged timeline of the set stem
// names.
func mergedLogPath(stem string) string {
	return stem + mergedMarker + filepath.Base(stem)
}

// mergedLogStem reverses mergedLogPath: the set's stem, and whether vpath
// names a merged timeline at all.
func mergedLogStem(vpath string) (string, bool) {
	i := strings.Index(vpath, mergedMarker)
	if i <= 0 {
		return "", false
	}
	return vpath[:i], true
}

// mergedLogTitle renders the tab/pane label of a merged timeline:
// "app.log (merged)". A path that names none is not ours.
func mergedLogTitle(vpath string) (string, bool) {
	stem, ok := mergedLogStem(vpath)
	if !ok {
		return "", false
	}
	return filepath.Base(stem) + " (merged)", true
}

// openMergedLogSet handles log.openRotatedSet: it resolves the rotation set of
// the focused buffer — of the underlying set when that buffer already *is* a
// merged timeline, which makes the command its refresh — and merges it off the
// loop. A file with no rotated siblings says so instead of opening a second
// view of the same content.
func (m *Model) openMergedLogSet() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "merging a rotated log set needs an open log file")
		return nil
	}
	path := ed.Path()
	if stem, ok := mergedLogStem(path); ok {
		path = stem
	}
	set, ok := logset.Detect(path)
	if !ok || !set.Rotated() {
		m.host.Notify(host.Info, "no rotated log set next to "+baseName(path))
		return nil
	}
	return mergeLogSetCmd(set, m.largeFileLimits(), false)
}

// remergeLogSet handles the editor's MergeLogSetMsg (#1996): the newest member
// of a followed merged view was truncated, removed or replaced, so the whole
// set is read again — the replacement's lines belong after the ones already in
// the buffer, which no incremental append can express.
func (m *Model) remergeLogSet(vpath string) tea.Cmd {
	stem, ok := mergedLogStem(vpath)
	if !ok {
		return nil
	}
	set, ok := logset.Detect(stem)
	if !ok {
		// Mid-rotation the directory can hold nothing that reduces to the
		// stem: keep the view as it is; the next event asks again.
		return nil
	}
	return mergeLogSetCmd(set, m.largeFileLimits(), true)
}

// mergeLogSetCmd assembles the timeline off the Update loop. Reading (and
// decompressing) a dozen files must not stall the UI, so the result arrives as
// a message like every other I/O in the app.
func mergeLogSetCmd(set logset.Set, lim largefile.Limits, refollow bool) tea.Cmd {
	return func() tea.Msg {
		merged, err := logset.Merge(set, lim)
		return mergedLogMsg{set: set, merged: merged, err: err, refollow: refollow}
	}
}

// installMergedLog shows an assembled timeline: a fresh open lands in a
// read-only tab (the archive-preview tab policy), a re-merge replaces the
// content of every view already showing that set — keeping follow mode armed
// across the rotation that triggered it.
func (m *Model) installMergedLog(msg mergedLogMsg) tea.Cmd {
	vpath := mergedLogPath(msg.set.Stem)
	views := m.mergedLogViews(vpath)
	if msg.err != nil {
		m.host.Notify(host.Error, "cannot merge "+baseName(msg.set.Stem)+": "+msg.err.Error())
		// A failed re-merge must not stall the followers on a request that
		// never lands: re-anchor them at the newest member's current end, so
		// what was written meanwhile is skipped but the tail keeps streaming.
		if msg.refollow {
			for _, ed := range views {
				if st, err := os.Stat(ed.FollowSource()); err == nil {
					ed.ReanchorFollow(st.Size(), true)
				}
			}
		}
		return nil
	}
	// Only a plain newest member can be followed: a compressed one has no byte
	// offset an append could resume from.
	src, off, term := "", int64(0), false
	if newest, ok := msg.set.Newest(); ok && !newest.Gz {
		src, off, term = newest.Path, msg.merged.Tail, msg.merged.TailTerm
		if m.watcher != nil {
			// The poll fallback compares the file that exists on disk, not the
			// virtual path of the buffer (#1763 does the same for a .gz).
			m.watcher.Track(newest.Path)
		}
	}
	var cmds []tea.Cmd
	if msg.refollow {
		if len(views) == 0 {
			return nil // the view was closed while the merge ran
		}
		for _, ed := range views {
			ed.ShowMergedLog(vpath, msg.merged.Text, src, off, term)
			cmds = append(cmds, ed.Reparse())
		}
		if src == "" {
			// The set's newest member is compressed now: there is nothing left
			// to tail, and ShowMergedLog left follow mode for that reason.
			m.host.Notify(host.Warn, "rotated log set re-merged — follow stopped, its newest member is compressed")
			return tea.Batch(cmds...)
		}
		m.host.Notify(host.Info, "rotated log set re-merged — "+mergedSummary(msg.merged))
		return tea.Batch(cmds...)
	}
	if len(views) > 0 {
		// A timeline of this set is already open — in this pane or another one:
		// refresh it in place and focus it, which is what re-running the
		// command means. Opening a second view of the same set would only
		// duplicate it.
		for _, ed := range views {
			ed.ShowMergedLog(vpath, msg.merged.Text, src, off, term)
			cmds = append(cmds, ed.Reparse())
		}
		m.focusMergedLogView(vpath)
	} else if cmd := m.showMergedLogBuffer(vpath, msg.merged.Text, src, off, term); cmd != nil {
		cmds = append(cmds, cmd)
	}
	m.host.Notify(host.Info, "merged rotated log set — "+mergedSummary(msg.merged))
	if warn := mergedWarning(msg.merged); warn != "" {
		m.host.Notify(host.Warn, warn)
	}
	return tea.Batch(cmds...)
}

// showMergedLogBuffer installs the timeline in a fresh read-only tab, following
// the archive-preview tab policy (#156): an empty scratch tab is filled in
// place, anything else gets a tab of its own.
func (m *Model) showMergedLogBuffer(vpath, text, src string, off int64, term bool) tea.Cmd {
	key := m.fileEditorKey()
	if key == "" {
		key = m.spawnEditor()
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return nil
	}
	if ed := inst.Editor(); ed == nil || !ed.IsEmpty() {
		inst.AddTab()
		m.installEmitter(key)
	}
	ed := inst.Editor()
	if ed == nil {
		return nil
	}
	ed.ShowMergedLog(vpath, text, src, off, term)
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return ed.Reparse()
}

// focusMergedLogView activates the tab holding the timeline vpath and focuses
// its pane — the refresh path's counterpart to opening a new tab.
func (m *Model) focusMergedLogView(vpath string) {
	for _, key := range m.editorKeysForPath(vpath) {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		if idx := inst.TabForPath(vpath); idx >= 0 {
			m.activateTab(inst, idx)
			m.setFocus(key)
			return
		}
	}
}

// mergedLogViews collects every editor tab showing the merged timeline vpath,
// background tabs and other panes included — the merged buffer's path names no
// file, so the ordinary path routing never reaches it.
func (m *Model) mergedLogViews(vpath string) []*editor.Model {
	var out []*editor.Model
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.MergedLog() && ed.Path() == vpath {
				out = append(out, ed)
			}
		}
	}
	return out
}

// routeMergedLogFollow forwards a watcher event to every merged view tailing
// the file it names (#1996). A merged buffer's path is virtual, so the
// path-matching route cannot reach it; a removed source is re-tracked here for
// the same reason the single-file follower is — the poll dropped the entry when
// it reported the removal, and the replacement must still be seen.
func (m *Model) routeMergedLogFollow(msg watch.EventMsg) tea.Cmd {
	var cmds []tea.Cmd
	tracked := false
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			ed := inst.TabEditor(i)
			if ed == nil || !ed.MergedLog() || ed.FollowSource() != msg.Path {
				continue
			}
			if msg.Kind == watch.FileRemoved && m.watcher != nil && !tracked {
				m.watcher.Track(msg.Path)
				tracked = true
			}
			if cmd := ed.FollowExternalChange(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// notifyRotatedSet offers the merged timeline when a freshly opened log file
// turns out to belong to a rotation set (#1996) — the discoverability half of
// the feature, since nothing else in the UI says that the file next to this one
// holds the hour before it. Once per path per session, like the large-file
// toast, and only for log buffers: a rotated `.csv` or `.conf` is no timeline.
func (m Model) notifyRotatedSet(ed *editor.Model) {
	if ed == nil || !ed.HasFile() || ed.MergedLog() {
		return
	}
	path := ed.Path()
	if m.logsetToasted[path] || highlight.Lang(path) != "log" {
		return
	}
	set, ok := logset.Detect(path)
	if !ok || !set.Rotated() {
		return
	}
	m.logsetToasted[path] = true
	m.host.Notify(host.Info, "rotated log set: "+strconv.Itoa(len(set.Members)-1)+
		" more file(s) — Open Rotated Log Set shows them as one timeline")
}

// mergedSummary describes an assembled timeline for the toast: how many files
// and lines it holds.
func mergedSummary(merged logset.Merged) string {
	lines := 0
	for _, r := range merged.Regions {
		lines += r.Lines
	}
	return fmt.Sprintf("%d files, %d lines", len(merged.Regions), lines)
}

// mergedWarning is the caveat toast of a timeline the large-file thresholds cut
// (#149) or a member refused to be read for, empty when nothing was lost.
func mergedWarning(merged logset.Merged) string {
	var parts []string
	if merged.Omitted > 0 {
		parts = append(parts, fmt.Sprintf("%d older file(s) omitted at the large-file limit", merged.Omitted))
	} else if merged.Truncated {
		parts = append(parts, "a region was cut at the large-file limit")
	}
	if len(merged.Failed) > 0 {
		parts = append(parts, "unreadable: "+strings.Join(merged.Failed, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return "merged timeline incomplete — " + strings.Join(parts, "; ")
}
