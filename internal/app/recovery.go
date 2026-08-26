package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
	"ike/internal/host"
	"ike/internal/ui"
)

// recovery.go is the crash-recovery restore flow (Roadmap 0210, #166). When a
// previous session died with unsaved edits, the backup service (#165) leaves
// snapshot files behind. At launch the root model detects them and, once the
// window is sized, shows a floating prompt listing every recoverable file with a
// per-file Restore / Discard / Skip choice — reusing the conflict-prompt UX.

// recoveryItem is one recoverable snapshot plus whether its on-disk base file
// changed since the snapshot was taken.
type recoveryItem struct {
	snap        backup.Snapshot
	baseChanged bool
}

// recoveryState is the open restore prompt: the undecided items and the cursor.
type recoveryState struct {
	items  []recoveryItem
	cursor int
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

// maybeOpenRecovery shows the restore prompt once the window is sized, if
// startup found leftover snapshots and no prompt is open yet.
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
	m.shell.SetContent(ui.ModelContent{Heading: "Recover unsaved changes", Body: m.recoveryBody})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// recoveryOpen reports whether the restore prompt is showing.
func (m Model) recoveryOpen() bool { return m.recovery != nil && m.shell.IsOpen() }

// recoveryBody renders the file list with the cursor, per-file base-changed
// warning, and the key legend.
func (m Model) recoveryBody() string {
	var b strings.Builder
	b.WriteString("A previous session ended with unsaved changes.\n\n")
	for i, it := range m.recovery.items {
		marker := "  "
		if i == m.recovery.cursor {
			marker = "▸ "
		}
		name := displayPath(it.snap.Path)
		if it.snap.Path == "" {
			name = "[untitled buffer]"
		}
		b.WriteString(marker + name)
		if it.baseChanged {
			b.WriteString("  (changed on disk since backup!)")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString("  [r] restore   [d] discard   [s] skip   [j/k] move   [esc] skip all")
	return b.String()
}

// updateRecovery consumes every key while the restore prompt is open. r/d/s act
// on the highlighted file and drop it from the list; j/k move; esc skips the
// rest. Everything else is swallowed so nothing leaks past the modal.
func (m Model) updateRecovery(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rc := m.recovery
	if rc == nil || len(rc.items) == 0 {
		return m.closeRecovery()
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if m.pickerNav(msg.String(), &rc.cursor, len(rc.items), nil) {
		return m, nil
	}
	switch msg.String() {
	case "r":
		it := rc.items[rc.cursor]
		m.restoreSnapshot(it.snap)
		_ = backupService().Remove(it.snap.Key)
		m.host.Notify(host.Info, "recovered "+recoveryName(it.snap))
		return m.dropRecoveryItem()
	case "d":
		it := rc.items[rc.cursor]
		_ = backupService().Remove(it.snap.Key)
		return m.dropRecoveryItem()
	case "s":
		// Keep the snapshot for next launch; just remove it from this prompt.
		return m.dropRecoveryItem()
	case "esc":
		return m.closeRecovery()
	}
	return m, nil
}

// dropRecoveryItem removes the highlighted item and closes the prompt when the
// list empties.
func (m Model) dropRecoveryItem() (tea.Model, tea.Cmd) {
	rc := m.recovery
	rc.items = append(rc.items[:rc.cursor], rc.items[rc.cursor+1:]...)
	if rc.cursor >= len(rc.items) {
		rc.cursor = len(rc.items) - 1
	}
	if len(rc.items) == 0 {
		return m.closeRecovery()
	}
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
func (m *Model) restoreSnapshot(snap backup.Snapshot) {
	var key string
	if snap.Path != "" {
		if key = m.editorWithFile(snap.Path); key == "" {
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
			_ = inst.Editor().Load(snap.Path)
		}
	} else {
		key = m.spawnEditor()
	}
	if inst := m.activeWS().Panes.Get(key); inst != nil {
		if ed := inst.Editor(); ed != nil {
			ed.RestoreText(snap.Text)
		}
	}
	m.setFocus(key)
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
