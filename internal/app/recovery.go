package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/backup"
	"ike/internal/diff"
	"ike/internal/host"
	"ike/internal/ui"
)

// recovery.go is the crash-recovery restore flow (Roadmap 0210, #166, #2160).
// When a previous session died with unsaved edits, the backup service (#165)
// leaves snapshot files behind. At launch the root model detects them and, once
// the window is sized, raises a centered floating-shell dialog: the recoverable
// files on the left, an inline diff of the selected file's on-disk content
// against its snapshot on the right (#2160), so the user sees what restoring
// would change before deciding. Per file r/d/s restore, discard or skip; R/D
// answer for every file at once; esc defers the whole decision to the next
// launch, keeping the snapshots.

// recoveryItem is one recoverable snapshot plus whether its on-disk base file
// changed since the snapshot was taken.
type recoveryItem struct {
	snap        backup.Snapshot
	baseChanged bool
}

// recoveryState is the open restore dialog: the undecided items, the cursor,
// and the selected item's cached diff (on-disk content vs snapshot). The diff
// is recomputed on every cursor move and item drop, never per frame — the
// dialog body re-renders on every View (#409).
type recoveryState struct {
	items   []recoveryItem
	cursor  int
	diff    diff.Result
	diffErr string
}

// backupDir returns the current project's snapshot directory, mirroring
// layoutFile()/sessionFile(): IKE_CONFIG_DIR when set, else the project's
// ".ike" directory.
func backupDir() string { return backupDirFor("") }

// backupDirFor returns the snapshot directory of the project rooted at root
// ("" = the current working directory). The path is made absolute (#2185):
// snapshot writes and removals run off the Update loop as commands, and a
// project switch chdirs underneath them — a relative ".ike/backups" would
// resolve against whichever project happened to be current when the command
// finally ran, so a parked project's snapshots were removed from, or written
// into, the wrong directory. IKE_CONFIG_DIR still wins when set: it redirects
// every project's state to one directory (tests, sandboxes).
func backupDirFor(root string) string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return backup.Dir(d)
	}
	if root == "" {
		root = "."
	}
	// Resolve symlinks, not just Abs: the active project's directory is
	// derived from the working directory, which the OS reports fully resolved
	// (/var → /private/var on macOS), while a workspace Root is whatever path
	// the user picked. Both must name the same directory, or one project's
	// snapshots would split across two state dirs.
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return backup.Dir(filepath.Join(root, ".ike"))
}

// backupService returns a service pointed at the current project's snapshot
// directory.
func backupService() *backup.Service { return backupServiceFor("") }

// backupServiceFor returns a service pointed at the snapshot directory of the
// project rooted at root — the single seam every snapshot walk resolves its
// service through, so the quit path, a project switch and a workspace close
// all reach a *parked* workspace's own project directory instead of the
// active project's.
func backupServiceFor(root string) *backup.Service { return backup.New(backupDirFor(root), nil) }

// scanRecovery loads any leftover snapshots at startup. They are held until the
// first window size arrives, then shown as a prompt. With [backup] disabled the
// subsystem is fully off: no prompt, and existing snapshots (they hold file
// contents) are purged instead.
//
// buildModel runs this on every project switch too, not just at launch, so
// only snapshots written by a *dead* session count as crash evidence (#2185):
// this session's own snapshots belong to buffers it still holds — the parked
// workspace of the project being re-entered — and offering to "recover" a file
// that is about to reopen dirty anyway was the stale prompt on every switch.
func (m *Model) scanRecovery() {
	svc := backupService()
	if !m.backupEnabled() {
		_, _ = svc.Purge()
		return
	}
	snaps, err := svc.List()
	if err != nil {
		return
	}
	pending := make([]backup.Snapshot, 0, len(snaps))
	for _, s := range snaps {
		if s.FromCurrentSession() {
			continue
		}
		pending = append(pending, s)
	}
	if len(pending) == 0 {
		return
	}
	m.recoveryPending = pending
}

// maybeOpenRecovery raises the restore dialog once the window is sized, if
// startup found leftover snapshots and no dialog is open yet.
func (m *Model) maybeOpenRecovery() {
	if len(m.recoveryPending) == 0 || m.recovery != nil || m.width == 0 || m.height == 0 {
		return
	}
	items := make([]recoveryItem, 0, len(m.recoveryPending))
	for _, s := range m.recoveryPending {
		items = append(items, recoveryItem{snap: s, baseChanged: baseChanged(s)})
	}
	m.recoveryPending = nil
	m.recovery = &recoveryState{items: items}
	m.refreshRecoveryDiff()
	m.shell.SetContent(recoveryContent{m: *m})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// recoveryContent implements ui.Content (not ModelContent) so the dialog body
// learns the shell's width budget and can clip the file paths and the diff
// preview to it. The model copy is bound once at open time; the dialog's own
// state lives behind the recovery pointer, so cursor moves and item drops show
// on the very next frame (#409).
type recoveryContent struct{ m Model }

// Title implements ui.Content.
func (c recoveryContent) Title() string { return "Recover unsaved changes" }

// Render implements ui.Content.
func (c recoveryContent) Render(width int) string { return c.m.recoveryBody(width) }

// recoveryOpen reports whether the restore dialog is showing.
func (m Model) recoveryOpen() bool { return m.recovery != nil && m.shell.IsOpen() }

// recoverySel returns the highlighted item, false when the list is empty.
func (m Model) recoverySel() (recoveryItem, bool) {
	rc := m.recovery
	if rc == nil || rc.cursor < 0 || rc.cursor >= len(rc.items) {
		return recoveryItem{}, false
	}
	return rc.items[rc.cursor], true
}

// refreshRecoveryDiff recomputes the selected file's preview: the on-disk
// content on the left, the snapshot's text on the right, so the "+" side is
// exactly what restoring would put into the buffer. An untitled snapshot has
// no base file and diffs against the empty side; an unreadable or undecodable
// base leaves an explanatory note instead of a diff.
func (m *Model) refreshRecoveryDiff() {
	rc := m.recovery
	if rc == nil {
		return
	}
	rc.diff, rc.diffErr = diff.Result{}, ""
	it, ok := m.recoverySel()
	if !ok {
		return
	}
	right, err := normalizeBufferText([]byte(it.snap.Text))
	if err != nil {
		rc.diffErr = "snapshot undecodable: " + err.Error()
		return
	}
	left := ""
	if it.snap.Path != "" {
		data, rerr := os.ReadFile(it.snap.Path)
		switch {
		case rerr != nil && os.IsNotExist(rerr):
			rc.diffErr = "no file on disk — restoring re-creates its content"
		case rerr != nil:
			rc.diffErr = "file unreadable: " + rerr.Error()
			return
		default:
			if left, err = normalizeBufferText(data); err != nil {
				rc.diffErr = "file on disk undecodable: " + err.Error()
				return
			}
		}
	}
	rc.diff = diff.Compute(left, right)
}

// recoveryBody renders the dialog: the recoverable files (cursor, base-changed
// warning) stacked above the selected file's diff preview, then the key
// legend. The list is full width on purpose — a recovered file is identified
// by its path, and truncating it into a side column is what the preview is
// there to avoid.
func (m Model) recoveryBody(width int) string {
	cur, ok := m.recoverySel()
	if !ok {
		return "" // the dialog closes with its last item; nothing left to show
	}
	pal := m.pal()
	sel := lipgloss.NewStyle().Foreground(pal.Foreground).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	warn := lipgloss.NewStyle().Foreground(pal.Warning)

	var b strings.Builder
	b.WriteString("A previous session ended with unsaved changes.\n\n")
	for i, it := range m.recovery.items {
		marker := "  "
		if i == m.recovery.cursor {
			marker = "▸ "
		}
		line := recoveryLabel(it.snap)
		if it.baseChanged {
			line += "  (changed on disk since backup!)"
		}
		line = marker + clipPathLeft(line, max(1, width-2))
		switch {
		case i == m.recovery.cursor:
			line = sel.Render(line)
		case it.baseChanged:
			line = warn.Render(line)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	b.WriteString(dim.Render("  "+clipPathLeft(
		"diff for "+recoveryLabel(cur.snap)+
			" — what restoring would change (- on disk, + recovered)", max(1, width-2))) + "\n")
	for _, line := range m.renderRecoveryDiff(max(10, width-2)) {
		b.WriteString("  " + line + "\n")
	}

	b.WriteString("\n")
	b.WriteString(recoveryLegend(width))
	return b.String()
}

// recoveryLegend lays the key hints out over as many lines as width needs.
// It must fit: content wider than the shell's budget grows the box past the
// terminal, and a box that does not fit is dropped by the compositor — the
// dialog would simply not appear on a narrow terminal.
func recoveryLegend(width int) string {
	hints := []string{
		"[r] restore", "[d] discard", "[s] skip", "[R] restore all",
		"[D] discard all", "[j/k] move", "[esc] decide next launch",
	}
	const sep = "   "
	var lines []string
	line := ""
	for _, h := range hints {
		switch {
		case line == "":
			line = h
		case lipgloss.Width("  "+line+sep+h) <= width:
			line += sep + h
		default:
			lines = append(lines, "  "+line)
			line = h
		}
	}
	return strings.Join(append(lines, "  "+line), "\n")
}

// clipPathLeft fits a path-bearing line into w columns by dropping columns
// from the LEFT: a recovered file is identified by its name and the
// base-changed warning that follows it, both of which a right-side truncation
// would eat first on a long path.
func clipPathLeft(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.TruncateLeft(s, lipgloss.Width(s)-w+1, "…")
}

// recoveryLabel names a snapshot for the file list.
func recoveryLabel(snap backup.Snapshot) string {
	if snap.Path == "" {
		return "[untitled buffer]"
	}
	return displayPath(snap.Path)
}

// renderRecoveryDiff renders the selected snapshot's diff against the on-disk
// file as styled lines — the local-history/change-feed inline renderer, with a
// note in place of an empty diff.
func (m Model) renderRecoveryDiff(width int) []string {
	pal := m.pal()
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	rc := m.recovery
	var out []string
	if rc.diffErr != "" {
		out = append(out, lipgloss.NewStyle().Foreground(pal.Warning).
			Render(ansi.Truncate(rc.diffErr, width, "…")))
	}
	if len(rc.diff.Hunks) == 0 {
		if rc.diffErr == "" {
			out = append(out, dim.Render(ansi.Truncate(
				"no differences — the snapshot matches the file on disk", width, "…")))
		}
		return out
	}
	return append(out, miniDiffLines(pal, rc.diff, width)...)
}

// updateRecovery consumes every key while the restore dialog is open. r/d/s act
// on the highlighted file and drop it from the list, R/D answer for every
// remaining file at once; j/k move (the preview follows the selection live);
// esc defers the rest. Everything else is swallowed so nothing leaks past the
// modal.
func (m Model) updateRecovery(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rc := m.recovery
	if rc == nil || len(rc.items) == 0 {
		return m.closeRecovery()
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp. Every move
	// recomputes the preview against the newly selected file.
	if m.pickerNav(msg.String(), &rc.cursor, len(rc.items), m.refreshRecoveryDiff) {
		return m, nil
	}
	switch msg.String() {
	case "r":
		it := rc.items[rc.cursor]
		cmd := m.restoreSnapshot(it.snap)
		_ = backupService().Remove(it.snap.Key)
		m.host.Notify(host.Info, "recovered "+recoveryName(it.snap)+" — undo reverts, save keeps it")
		tm, dropCmd := m.dropRecoveryItem()
		return tm, tea.Batch(cmd, dropCmd)
	case "d":
		it := rc.items[rc.cursor]
		_ = backupService().Remove(it.snap.Key)
		return m.dropRecoveryItem()
	case "s":
		// Keep the snapshot for next launch; just remove it from this dialog.
		return m.dropRecoveryItem()
	case "R":
		return m.restoreAllRecovery()
	case "D":
		return m.discardAllRecovery()
	case "esc":
		return m.closeRecovery()
	}
	return m, nil
}

// restoreAllRecovery restores every remaining snapshot, then closes the dialog.
// The files open in list order, so the focus lands on the last one restored.
func (m Model) restoreAllRecovery() (tea.Model, tea.Cmd) {
	items := m.recovery.items
	cmds := make([]tea.Cmd, 0, len(items))
	for _, it := range items {
		cmds = append(cmds, m.restoreSnapshot(it.snap))
		_ = backupService().Remove(it.snap.Key)
	}
	m.host.Notify(host.Info, fmt.Sprintf("recovered %d file(s) — undo reverts, save keeps them", len(items)))
	m.recovery.items = nil
	tm, cmd := m.closeRecovery()
	return tm, tea.Batch(append(cmds, cmd)...)
}

// discardAllRecovery deletes every remaining snapshot, then closes the dialog.
func (m Model) discardAllRecovery() (tea.Model, tea.Cmd) {
	items := m.recovery.items
	for _, it := range items {
		_ = backupService().Remove(it.snap.Key)
	}
	m.host.Notify(host.Info, fmt.Sprintf("discarded %d recovery snapshot(s)", len(items)))
	m.recovery.items = nil
	return m.closeRecovery()
}

// dropRecoveryItem removes the highlighted item, refreshes the preview for the
// item that takes its place, and closes the dialog when the list empties.
func (m Model) dropRecoveryItem() (tea.Model, tea.Cmd) {
	rc := m.recovery
	rc.items = append(rc.items[:rc.cursor], rc.items[rc.cursor+1:]...)
	if rc.cursor >= len(rc.items) {
		rc.cursor = len(rc.items) - 1
	}
	if len(rc.items) == 0 {
		return m.closeRecovery()
	}
	m.refreshRecoveryDiff()
	return m, nil
}

// closeRecovery dismisses the prompt, leaving any undecided snapshots on disk so
// they are offered again next launch. The prompt has had its say now, so the
// age-based GC (#167) may prune what remains — never silently before it.
func (m Model) closeRecovery() (tea.Model, tea.Cmd) {
	m.recovery = nil
	m.shell.Close()
	if m.backupEnabled() {
		_, _ = backupService().Prune(backupMaxAge(m.host.Config()))
	}
	// The shell is free again: the first-run welcome tour (#658) and then the
	// first-start onboarding dialog (#301) that were waiting behind the
	// recovery prompt may open now (the tour's command persists ui.onboarded).
	cmd := m.maybeOpenTour()
	m.maybeOpenOnboarding()
	return m, cmd
}

// restoreSnapshot opens the recovered text as a dirty buffer: onto the base file
// (titled) or into a fresh untitled editor.
//
// A buffer that holds the on-disk file takes the recovered text through the
// normal edit path (#2160): RestoreContent replaces the whole buffer as ONE
// undo step, so plain undo brings the on-disk version back and the file is
// only written when the user saves. Without a base file on disk there is
// nothing to undo back to, so the text is seeded with RestoreText — which also
// marks the buffer never-saved, keeping it dirty however far undo runs.
func (m *Model) restoreSnapshot(snap backup.Snapshot) tea.Cmd {
	var key string
	loaded := false
	if snap.Path != "" {
		if key = m.editorWithFile(snap.Path); key != "" {
			loaded = true
		} else {
			if key = m.activeEditorKey(); key == "" {
				key = m.spawnEditor()
			}
			// The pane's active tab can be a terminal (#573), leaving
			// Editor() nil (#931) — restore into a fresh tab instead.
			inst := m.activeWS().Panes.Get(key)
			if inst.Editor() == nil {
				inst.AddTab()
				m.installEmitter(key)
			}
			// Establish the path from the base file when it still exists; a deleted
			// base just leaves the recovered text under no path.
			loaded = inst.Editor().Load(snap.Path) == nil
		}
	} else {
		key = m.spawnEditor()
	}
	var cmd tea.Cmd
	if inst := m.activeWS().Panes.Get(key); inst != nil {
		if ed := inst.Editor(); ed != nil {
			if loaded {
				// The buffer already shows the on-disk version: the recovered
				// text lands as an undoable edit against it. An unchanged
				// buffer (snapshot == disk) is left clean, as it should be.
				if ed.RestoreContent(snap.Text) {
					// The edit bypassed the editor's Update loop; drop its
					// stale highlight/conceal caches (#1683).
					cmd = ed.ReparseEdits()
				}
			} else {
				ed.RestoreText(snap.Text)
			}
		}
	}
	m.setFocus(key)
	return cmd
}

// baseChanged reports whether snap's on-disk base file differs from the version
// the snapshot was taken against (hash preferred, mtime as a fallback). A missing
// or unreadable base counts as changed.
func baseChanged(snap backup.Snapshot) bool {
	if !snap.HasBase {
		return false
	}
	mtime, hash, ok := backup.BaseInfo(snap.Path)
	if !ok {
		return true
	}
	if snap.BaseHash != "" && hash != "" {
		return hash != snap.BaseHash
	}
	return !mtime.Equal(snap.BaseMTime)
}

// recoveryName is a short label for notifications.
func recoveryName(snap backup.Snapshot) string {
	if snap.Path == "" {
		return "untitled buffer"
	}
	return filepath.Base(snap.Path)
}

// recoveryClickRow maps a body row of the restore dialog onto a snapshot
// (#2275): the intro line and its blank spacer occupy rows 0 and 1, the
// recoverable files follow, and the diff preview plus the key legend below
// them are inert. The dialog has no enter action — r restores, d discards — so
// like the doctor panes a click selects and nothing more.
func (m Model) recoveryClickRow(row int) (tea.Model, tea.Cmd) {
	rc := m.recovery
	if rc == nil {
		return m, nil
	}
	const recoveryHeadRows = 2 // "A previous session…" plus its blank spacer
	i, ok := ui.RowAt(row, 0, recoveryHeadRows, len(rc.items), len(rc.items))
	if !ok || i == rc.cursor {
		return m, nil
	}
	rc.cursor = i
	m.refreshRecoveryDiff()
	return m, nil
}
