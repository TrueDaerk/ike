package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/changefeed"
	"ike/internal/diff"
	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/ui"
	"ike/internal/watch"
)

// changefeed.go wires the agent change feed (#2000). Coding agents write
// across the project while the user edits in parallel, and until now those
// writes landed silently: buffers reloaded (#1515) but nothing said *what*
// had changed. Every external watcher event — the watcher already drops
// IKE's own saves through its save epoch (Roadmap 0140) — is recorded into a
// session-scoped feed, and `watch.changeFeed` raises the two-pane panel over
// it: the changed files newest-first on the left, a mini-diff of the selected
// one on the right. From there the file opens, a conflicted buffer reloads,
// and the external change reverts through the local-history restore path,
// behind a confirmation.

// ChangeFeedMsg runs watch.changeFeed: show the external-change feed.
type ChangeFeedMsg struct{}

// changeFeedLimit reads files.change_feed_limit — how many files the feed
// keeps. Zero disables it; an unset or unparsable value selects the package
// default (the config layer validates and clamps, so this is only the
// belt-and-braces path for a config-free test model).
func (m Model) changeFeedLimit() int {
	v, ok := m.host.Config().Get("files.change_feed_limit")
	if !ok {
		return changefeed.DefaultLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return changefeed.DefaultLimit
	}
	return n
}

// feedKind maps a watcher event kind onto the feed's.
func feedKind(k watch.Kind) changefeed.Kind {
	switch k {
	case watch.FileCreated:
		return changefeed.Created
	case watch.FileRemoved:
		return changefeed.Removed
	default:
		return changefeed.Changed
	}
}

// recordChangeFeed records one external file event. It runs on the watcher
// event *before* the event is routed to the editors, so the pre-change content
// is captured while the open buffer still holds it — the auto-reload that
// follows would otherwise leave nothing to diff against. Directory, git and
// config events never reach it: they name no project file.
func (m *Model) recordChangeFeed(msg watch.EventMsg) {
	if m.feed == nil {
		return
	}
	switch msg.Kind {
	case watch.FileChanged, watch.FileCreated, watch.FileRemoved:
	default:
		return
	}
	// The cap is re-read per event rather than cached: the setting hot-reloads
	// like every other, and lowering it has to trim the existing list too.
	m.feed.SetLimit(m.changeFeedLimit())
	// The watcher suppresses IKE's own saves at ingest and again at flush;
	// re-asking here keeps the feed honest for events that reach the model by
	// another route (the poll fallback, a replayed message) — a save must
	// never look like an agent's write.
	if m.watcher.SavedRecently(msg.Path) {
		return
	}
	before, origin := m.changeFeedBefore(msg.Path, msg.Kind)
	if !m.feed.Add(changefeed.Entry{
		Path:   msg.Path,
		Time:   m.clock(),
		Kind:   feedKind(msg.Kind),
		Before: before,
		Origin: origin,
	}) {
		return
	}
	m.syncOpenChangeFeed()
}

// syncOpenChangeFeed folds a just-recorded change into the open panel. The
// panel is modal, but the agent that caused it keeps writing while the user
// reviews — a list frozen at open time would go stale within seconds. New
// entries prepend, so the selection is re-found by path instead of by index.
func (m *Model) syncOpenChangeFeed() {
	if !m.cfPicker {
		return
	}
	selPath := ""
	if e, ok := m.changeFeedSel(); ok {
		selPath = e.Path
	}
	m.cfEntries = m.feed.Entries()
	m.cfSel = 0
	for i, e := range m.cfEntries {
		if e.Path == selPath {
			m.cfSel = i
			break
		}
	}
	m.refreshChangeFeedDiff()
	m.setChangeFeedContent()
}

// changeFeedCapturedMsg delivers the off-loop-resolved pre-change contents of
// a watcher batch (#2176): entries whose Before had to come from the
// local-history store, read on a background goroutine instead of the Update
// loop — a 300-file checkout must not cost 300 disk reads mid-render.
type changeFeedCapturedMsg struct{ entries []changefeed.Entry }

// recordChangeFeedBatch records one watcher flush's file events (#2176). The
// exact pre-change content — an open, clean buffer — is captured inline (it is
// about to be overwritten by the auto-reload that routing triggers), while the
// local-history fallback resolves in the returned command, off the Update
// loop; its entries land via changeFeedCapturedMsg.
func (m *Model) recordChangeFeedBatch(events []watch.EventMsg) tea.Cmd {
	if m.feed == nil {
		return nil
	}
	// The cap is re-read per flush rather than cached: the setting hot-reloads
	// like every other, and lowering it has to trim the existing list too.
	m.feed.SetLimit(m.changeFeedLimit())
	type deferredCapture struct {
		path string
		kind changefeed.Kind
		at   time.Time
	}
	var deferred []deferredCapture
	added := false
	for _, msg := range events {
		switch msg.Kind {
		case watch.FileChanged, watch.FileCreated, watch.FileRemoved:
		default:
			continue
		}
		// The watcher suppresses IKE's own saves at ingest and again at flush;
		// re-asking here keeps the feed honest for events that reach the model
		// by another route — a save must never look like an agent's write.
		if m.watcher.SavedRecently(msg.Path) {
			continue
		}
		if msg.Kind != watch.FileCreated {
			if ed := m.editorForPath(msg.Path); ed == nil || ed.Dirty() || ed.Stale() {
				// No exact buffer content to save from the reload: the honest
				// fallback is the newest local-history snapshot, which reads
				// from disk — deferred off-loop.
				deferred = append(deferred, deferredCapture{path: msg.Path, kind: feedKind(msg.Kind), at: m.clock()})
				continue
			}
		}
		before, origin := m.changeFeedBefore(msg.Path, msg.Kind)
		if m.feed.Add(changefeed.Entry{
			Path:   msg.Path,
			Time:   m.clock(),
			Kind:   feedKind(msg.Kind),
			Before: before,
			Origin: origin,
		}) {
			added = true
		}
	}
	if added {
		m.syncOpenChangeFeed()
	}
	if len(deferred) == 0 {
		return nil
	}
	store := m.lhStore // stateless disk reads — safe off the Update loop
	return func() tea.Msg {
		entries := make([]changefeed.Entry, 0, len(deferred))
		for _, d := range deferred {
			before, origin := "", changefeed.NoBefore
			if snaps := store.List(d.path); len(snaps) > 0 {
				if data, err := store.Read(snaps[0].Hash); err == nil {
					if text, terr := normalizeBufferText(data); terr == nil {
						before, origin = text, changefeed.FromSnapshot
					}
				}
			}
			entries = append(entries, changefeed.Entry{
				Path: d.path, Time: d.at, Kind: d.kind, Before: before, Origin: origin,
			})
		}
		return changeFeedCapturedMsg{entries: entries}
	}
}

// applyChangeFeedCaptured folds the off-loop captures into the feed (#2176).
func (m *Model) applyChangeFeedCaptured(msg changeFeedCapturedMsg) {
	if m.feed == nil {
		return
	}
	added := false
	for _, e := range msg.entries {
		if m.feed.Add(e) {
			added = true
		}
	}
	if added {
		m.syncOpenChangeFeed()
	}
}

// changeFeedBefore resolves what the file held before the external write. The
// open, unmodified buffer is the exact answer — it is the content the write
// replaced. A dirty or stale buffer is not: its text was never on disk, so the
// newest local-history snapshot (what IKE last *wrote* there) is the honest
// fallback. A created file had no previous content at all.
func (m Model) changeFeedBefore(path string, kind watch.Kind) (string, changefeed.Origin) {
	if kind == watch.FileCreated {
		return "", changefeed.NoBefore
	}
	if ed := m.editorForPath(path); ed != nil && !ed.Dirty() && !ed.Stale() {
		return ed.Text(), changefeed.FromBuffer
	}
	entries := m.lhStore.List(path)
	if len(entries) == 0 {
		return "", changefeed.NoBefore
	}
	data, err := m.lhStore.Read(entries[0].Hash)
	if err != nil {
		return "", changefeed.NoBefore
	}
	text, err := normalizeBufferText(data)
	if err != nil {
		return "", changefeed.NoBefore
	}
	return text, changefeed.FromSnapshot
}

// openChangeFeed raises the feed panel over the recorded changes.
func (m *Model) openChangeFeed() {
	if m.feed == nil || m.feed.Len() == 0 {
		m.host.Notify(host.Info, "no external file changes recorded this session")
		return
	}
	m.cfEntries = m.feed.Entries()
	m.cfSel = 0
	m.cfPicker = true
	m.refreshChangeFeedDiff()
	m.setChangeFeedContent()
	m.shell.Open()
}

// changeFeedContent implements ui.Content (not ModelContent) so the body
// learns the shell's width budget and can split it into the file list and the
// mini-diff columns, the local-history panel's layout (#1969).
type changeFeedContent struct{ m Model }

// Title implements ui.Content.
func (c changeFeedContent) Title() string {
	return fmt.Sprintf("EXTERNAL CHANGES — %d file(s)", len(c.m.cfEntries))
}

// Render implements ui.Content.
func (c changeFeedContent) Render(width int) string { return c.m.changeFeedBody(width) }

// setChangeFeedContent (re-)binds the shell body to THIS model copy (#1440):
// the root model is a value model, so content bound once at open time would
// keep rendering the open-time selection.
func (m *Model) setChangeFeedContent() {
	m.shell.SetContent(changeFeedContent{m: *m})
}

// changeFeedOpen reports whether the shell currently shows the feed panel —
// the content check guards against another overlay having taken the shell over
// without the panel's own close path running (the pins pattern).
func (m Model) changeFeedOpen() bool {
	if !m.cfPicker || !m.shell.IsOpen() {
		return false
	}
	_, ok := m.shell.Content().(changeFeedContent)
	return ok
}

// changeFeedSel returns the selected entry, false when the list is empty.
func (m Model) changeFeedSel() (changefeed.Entry, bool) {
	if m.cfSel < 0 || m.cfSel >= len(m.cfEntries) {
		return changefeed.Entry{}, false
	}
	return m.cfEntries[m.cfSel], true
}

// refreshChangeFeedDiff recomputes the mini-diff of the selected entry: its
// captured pre-change content against what the file holds now. "Now" is the
// live buffer where one is open (that is what the user is looking at) and the
// file on disk otherwise; a removed file's right side is empty. A missing
// pre-change content lands in cfErr and renders in place of the diff, so the
// selection can sweep across such an entry without side effects.
func (m *Model) refreshChangeFeedDiff() {
	m.cfDiff, m.cfErr = diff.Result{}, ""
	e, ok := m.changeFeedSel()
	if !ok {
		return
	}
	if !e.HasBefore() {
		switch {
		case e.Origin == changefeed.Dropped:
			m.cfErr = "pre-change content released to stay inside the feed's memory cap"
		case e.Kind == changefeed.Created:
			m.cfErr = "created externally — there is no previous version to diff against"
		default:
			m.cfErr = "no pre-change content: the file was never opened or saved in this session"
		}
		return
	}
	after, err := m.changeFeedAfter(e)
	if err != "" {
		m.cfErr = err
		return
	}
	m.cfDiff = diff.Compute(e.Before, after)
}

// changeFeedAfter resolves the entry's current content, normalized to the
// buffer's native form so both diff sides are comparable. The returned string
// is the error message when the read failed.
func (m Model) changeFeedAfter(e changefeed.Entry) (string, string) {
	// A deleted file's buffer may well still be open — IKE keeps a dirty one
	// as the last copy (#83) — but that text is not what the file holds now:
	// nothing is. Read the disk, which answers "gone" honestly.
	if ed := m.editorForPath(e.Path); ed != nil && e.Kind != changefeed.Removed {
		return ed.Text(), ""
	}
	data, err := os.ReadFile(e.Path)
	if err != nil {
		if e.Kind == changefeed.Removed || os.IsNotExist(err) {
			return "", "" // deleted externally: the whole file reads as removed
		}
		return "", "unreadable now: " + err.Error()
	}
	text, derr := normalizeBufferText(data)
	if derr != nil {
		return "", "undecodable now: " + derr.Error()
	}
	return text, ""
}

// changeFeedBody renders the two-pane panel: the changed files left, the
// selected entry's mini-diff right, plus the key hints.
func (m Model) changeFeedBody(width int) string {
	pal := m.pal()
	sel := lipgloss.NewStyle().Foreground(pal.Foreground).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Hint)

	now := m.clock()
	left := make([]string, 0, len(m.cfEntries))
	leftW := 0
	for i, e := range m.cfEntries {
		// Name before directory: the column is truncated to half the panel,
		// and the file name is the part that must survive that cut.
		line := fmt.Sprintf("  %s %-9s %s  %s%s", e.Kind.Icon(),
			project.RelTime(e.Time, now), baseName(e.Path), changeFeedDir(e.Path), changeFeedCount(e))
		if i == m.cfSel {
			line = "▍" + line[1:]
		}
		leftW = max(leftW, lipgloss.Width(line))
		left = append(left, line)
	}
	if capW := max(20, width/2); leftW > capW {
		leftW = capW
	}
	rightW := max(10, width-leftW-3)
	right := m.renderChangeFeedDiff(rightW)

	var b strings.Builder
	sep := dim.Render(" │ ")
	for i := 0; i < max(len(left), len(right)); i++ {
		l := ""
		if i < len(left) {
			l = ansi.Truncate(left[i], leftW, "…")
			if i == m.cfSel {
				l = sel.Render(l)
			}
		}
		b.WriteString(l + strings.Repeat(" ", max(0, leftW-lipgloss.Width(l))))
		b.WriteString(sep)
		if i < len(right) {
			b.WriteString(right[i])
		}
		b.WriteByte('\n')
	}
	if e, ok := m.changeFeedSel(); ok {
		b.WriteString("\n" + dim.Render(fmt.Sprintf("%s %s · before: %s",
			e.Kind.Label(), e.Time.Local().Format("2006-01-02 15:04:05"), e.Origin.Label())))
	}
	b.WriteString("\nenter open · d diff pane · R reload buffer · r revert change · " +
		"x dismiss · c clear feed · j/k move · esc close")
	return b.String()
}

// changeFeedDir renders the directory column: the file's project-relative
// parent, empty for a file sitting at the project root.
func changeFeedDir(path string) string {
	dir := filepath.Dir(displayPath(path))
	if dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return dir
}

// changeFeedCount renders the repeat-writes suffix, empty for a single event.
func changeFeedCount(e changefeed.Entry) string {
	if e.Count < 2 {
		return ""
	}
	return fmt.Sprintf("  (×%d)", e.Count)
}

// renderChangeFeedDiff renders the selected entry's mini-diff as styled lines,
// git style: @@ hunk headers, a +/- gutter marker per line, added lines green,
// removed red, context plain — the local-history panel's renderer over the
// feed's before/after pair.
func (m Model) renderChangeFeedDiff(width int) []string {
	pal := m.pal()
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	if m.cfErr != "" {
		return []string{lipgloss.NewStyle().Foreground(pal.Hint).Render(ansi.Truncate(m.cfErr, width, "…"))}
	}
	if len(m.cfDiff.Hunks) == 0 {
		return []string{dim.Render(ansi.Truncate(
			"no differences left — the change was already reverted or overwritten", width, "…"))}
	}
	return miniDiffLines(pal, m.cfDiff, width)
}

// updateChangeFeed consumes every key while the panel is open: navigation (the
// mini-diff follows the selection live) and the per-entry actions. Everything
// else is swallowed (the panel is modal).
func (m Model) updateChangeFeed(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Shared list semantics (#1666): steps wrap, page jumps clamp. Every move
	// recomputes the mini-diff before re-binding the shell body.
	if m.pickerNav(msg.String(), &m.cfSel, len(m.cfEntries), func() {
		m.refreshChangeFeedDiff()
		m.setChangeFeedContent()
	}) {
		return m, nil
	}
	switch msg.String() {
	case "esc", "q":
		m.closeChangeFeed()
		return m, nil
	case "c":
		m.feed.Clear()
		m.closeChangeFeed()
		m.host.Notify(host.Info, "change feed cleared")
		return m, nil
	}
	e, ok := m.changeFeedSel()
	if !ok {
		return m, nil
	}
	switch msg.String() {
	case "x":
		m.feed.Remove(e.Path)
		m.cfEntries = m.feed.Entries()
		if m.cfSel >= len(m.cfEntries) {
			m.cfSel = len(m.cfEntries) - 1
		}
		if len(m.cfEntries) == 0 {
			m.closeChangeFeed()
			return m, nil
		}
		m.refreshChangeFeedDiff()
		m.setChangeFeedContent()
		return m, nil
	case "enter":
		m.closeChangeFeed()
		if e.Kind == changefeed.Removed {
			m.host.Notify(host.Warn, displayPath(e.Path)+" no longer exists — r restores it from the feed")
			return m, nil
		}
		return m.openPathInEditor(e.Path)
	case "d":
		// An action that cannot happen leaves the panel up: closing a modal
		// only to toast a refusal costs the user the list they were reading.
		if !e.HasBefore() {
			m.host.Notify(host.Info, changeFeedNoRevert(e))
			return m, nil
		}
		m.closeChangeFeed()
		return m, m.openChangeFeedDiffPane(e)
	case "R":
		if reason, ok := m.changeFeedReloadRefusal(e); !ok {
			m.host.Notify(host.Info, reason)
			return m, nil
		}
		m.closeChangeFeed()
		return m, m.reloadChangeFeedBuffer(e)
	case "r":
		if !e.HasBefore() {
			m.host.Notify(host.Info, changeFeedNoRevert(e))
			return m, nil
		}
		m.closeChangeFeed()
		m.openChangeFeedRevertPrompt(e)
		return m, nil
	}
	return m, nil
}

// closeChangeFeed drops the panel off the shell (an action opening a pane must
// not leave the modal in front of it).
func (m *Model) closeChangeFeed() {
	m.cfPicker = false
	m.cfEntries, m.cfDiff, m.cfErr = nil, diff.Result{}, ""
	m.shell.Close()
}

// changeFeedNoRevert explains why an entry cannot be reverted, naming the
// reason rather than the mechanism — the user needs to know whether anything
// was lost, not which field is empty.
func changeFeedNoRevert(e changefeed.Entry) string {
	switch {
	case e.Kind == changefeed.Created:
		return baseName(e.Path) + " was created externally — there is no previous version to restore"
	case e.Origin == changefeed.Dropped:
		return "the pre-change content of " + baseName(e.Path) +
			" was released to stay inside the feed's memory cap"
	default:
		return "nothing to restore: " + baseName(e.Path) +
			" was never opened or saved in this session, so its previous content is unknown"
	}
}

// openChangeFeedDiffPane lands the entry's before/after pair in the reusable
// diff pane (#60), where intra-line emphasis, hunk navigation and editing the
// right side live. The right side is the working tree, so it stays editable.
func (m *Model) openChangeFeedDiffPane(e changefeed.Entry) tea.Cmd {
	if !e.HasBefore() {
		m.host.Notify(host.Info, "no pre-change content recorded for "+baseName(e.Path))
		return nil
	}
	after, err := m.changeFeedAfter(e)
	if err != "" {
		m.host.Notify(host.Warn, "change feed: "+err)
		return nil
	}
	name := baseName(e.Path)
	m.openDiffTexts(e.Path, name+" @ before external "+e.Kind.Label(), name,
		e.Before, after, e.Kind != changefeed.Removed)
	return nil
}

// reloadChangeFeedBuffer re-reads the entry's file into its open buffer. A
// clean buffer has usually reloaded itself already (files.auto_reload) and the
// reload no-ops on identical content; the action exists for the conflicted
// case — a dirty, stale buffer whose edits the user decided to drop.
func (m *Model) reloadChangeFeedBuffer(e changefeed.Entry) tea.Cmd {
	ed := m.editorForPath(e.Path)
	if ed == nil {
		return nil // guarded by changeFeedReloadRefusal before the panel closes
	}
	cmd := ed.ResolveConflictReload()
	m.host.Notify(host.Info, "reloaded from disk: "+displayPath(e.Path))
	return cmd
}

// changeFeedReloadRefusal reports whether the reload action applies to e, and
// why not when it does not.
func (m Model) changeFeedReloadRefusal(e changefeed.Entry) (string, bool) {
	switch {
	case m.editorForPath(e.Path) == nil:
		return baseName(e.Path) + " is not open — enter opens it", false
	case e.Kind == changefeed.Removed:
		return baseName(e.Path) + " no longer exists — the buffer is the only copy left", false
	}
	return "", true
}

// openChangeFeedRevertPrompt asks before restoring the pre-change content:
// reverting somebody else's write is destructive enough to confirm, and the
// prompt is where the two revert shapes are spelled out — an existing file is
// restored into its buffer (undoable, disk untouched until the save), a
// deleted one is written straight back to disk since there is no buffer left.
func (m *Model) openChangeFeedRevertPrompt(e changefeed.Entry) {
	if !e.HasBefore() {
		m.host.Notify(host.Info, changeFeedNoRevert(e))
		return
	}
	m.cfRevert = e.Path
	detail := "The pre-change content (" + e.Origin.Label() + ") is restored into the buffer as one\n" +
		"undoable edit — undo brings the external version back, and the file on disk\nis untouched until you save."
	if e.Kind == changefeed.Removed {
		detail = "The file is gone, so there is no buffer to restore into: the pre-change\n" +
			"content (" + e.Origin.Label() + ") is written back to disk and opened."
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: "Revert external change",
		Body: func() string {
			return fmt.Sprintf("%s was %s externally %s.\n\n%s\n\n  [enter] revert\n  [esc]   cancel",
				displayPath(e.Path), e.Kind.Label(), project.RelTime(e.Time, m.clock()), detail)
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// changeFeedRevertOpen reports whether the shell shows the revert confirmation.
func (m Model) changeFeedRevertOpen() bool { return m.cfRevert != "" && m.shell.IsOpen() }

// updateChangeFeedRevert consumes every key while the confirmation is open.
func (m Model) updateChangeFeedRevert(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y":
		path := m.cfRevert
		m.cfRevert = ""
		m.shell.Close()
		return m.applyChangeFeedRevert(path)
	case "esc":
		m.cfRevert = ""
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// applyChangeFeedRevert restores the entry's pre-change content. An open (or
// openable) file goes through the local-history restore path — one undoable
// edit, dirty buffer, disk untouched until the save — so a revert is never
// itself destructive. A file deleted externally has no buffer to restore into,
// so it is written back and opened; the write stamps the watcher's save epoch,
// which keeps the restore from echoing back into the feed as a new change.
func (m Model) applyChangeFeedRevert(path string) (tea.Model, tea.Cmd) {
	e, ok := m.feed.Get(path)
	if !ok || !e.HasBefore() {
		m.host.Notify(host.Warn, "the feed entry for "+baseName(path)+" is gone")
		return m, nil
	}
	if e.Kind == changefeed.Removed {
		if err := writeChangeFeedRevert(path, e.Before); err != nil {
			m.host.Notify(host.Error, "revert: "+err.Error())
			return m, nil
		}
		m.watcher.MarkSaved(path)
		m.feed.Remove(path)
		m.host.Notify(host.Info, "restored "+displayPath(path)+" from the change feed")
		return m.openPathInEditor(path)
	}
	if m.editorForPath(path) == nil {
		// Not open: the restore path edits a buffer, so open one first. The
		// open is synchronous enough for the edit below — its command only
		// carries the parse/LSP follow-up.
		tm, openCmd := m.openPathInEditor(path)
		m = tm.(Model)
		if m.editorForPath(path) == nil {
			m.host.Notify(host.Warn, "could not open "+baseName(path)+" to revert it")
			return m, openCmd
		}
		cmd := m.restoreLocalHistory(path, e.Time, e.Before)
		m.feed.Remove(path)
		return m, tea.Batch(openCmd, cmd)
	}
	cmd := m.restoreLocalHistory(path, e.Time, e.Before)
	m.feed.Remove(path)
	return m, cmd
}

// writeChangeFeedRevert writes the restored content back for a file that was
// deleted externally, re-adding the trailing newline the normalized text drops.
func writeChangeFeedRevert(path, text string) error {
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// feedIgnore is the feed's noise filter: the watcher's own ignore rule,
// resolved against whatever root the watcher currently runs on (the service
// pointer outlives project switches, its root does not).
func feedIgnore(w *watch.Service) func(string) bool {
	return func(path string) bool {
		root := w.Root()
		if root == "" {
			// The watcher has not started yet (or files.watch is off): fall
			// back to the project root. Judging an absolute path with no root
			// at all would call every file below a dotted ancestor noise — a
			// checkout living under ~/.config or a git worktree under
			// .claude/worktrees would report nothing at all.
			root, _ = cachedGetwd()
		}
		return watch.Ignored(root, path)
	}
}
