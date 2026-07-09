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

// backupDir returns the snapshot directory, mirroring layoutFile()/sessionFile():
// IKE_CONFIG_DIR when set, else the project's ".ike" directory.
func backupDir() string {
	base := ".ike"
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		base = d
	}
	return backup.Dir(base)
}

// backupService returns a service pointed at the project's snapshot directory.
func backupService() *backup.Service { return backup.New(backupDir(), nil) }

// scanRecovery loads any leftover snapshots at startup. They are held until the
// first window size arrives, then shown as a prompt.
func (m *Model) scanRecovery() {
	snaps, err := backupService().List()
	if err != nil || len(snaps) == 0 {
		return
	}
	m.recoveryPending = snaps
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
		return m.closeRecovery(), nil
	}
	switch msg.String() {
	case "j", "down":
		if rc.cursor < len(rc.items)-1 {
			rc.cursor++
		}
	case "k", "up":
		if rc.cursor > 0 {
			rc.cursor--
		}
	case "r":
		it := rc.items[rc.cursor]
		m.restoreSnapshot(it.snap)
		_ = backupService().Remove(it.snap.Key)
		m.host.Notify(host.Info, "recovered "+recoveryName(it.snap))
		return m.dropRecoveryItem(), nil
	case "d":
		it := rc.items[rc.cursor]
		_ = backupService().Remove(it.snap.Key)
		return m.dropRecoveryItem(), nil
	case "s":
		// Keep the snapshot for next launch; just remove it from this prompt.
		return m.dropRecoveryItem(), nil
	case "esc":
		return m.closeRecovery(), nil
	}
	return m, nil
}

// dropRecoveryItem removes the highlighted item and closes the prompt when the
// list empties.
func (m Model) dropRecoveryItem() tea.Model {
	rc := m.recovery
	rc.items = append(rc.items[:rc.cursor], rc.items[rc.cursor+1:]...)
	if rc.cursor >= len(rc.items) {
		rc.cursor = len(rc.items) - 1
	}
	if len(rc.items) == 0 {
		return m.closeRecovery()
	}
	return m
}

// closeRecovery dismisses the prompt, leaving any undecided snapshots on disk so
// they are offered again next launch.
func (m Model) closeRecovery() tea.Model {
	m.recovery = nil
	m.shell.Close()
	return m
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
			// Establish the path from the base file when it still exists; a deleted
			// base just leaves the recovered text under no path.
			_ = m.panes.Get(key).Editor().Load(snap.Path)
		}
	} else {
		key = m.spawnEditor()
	}
	if inst := m.panes.Get(key); inst != nil {
		inst.Editor().RestoreText(snap.Text)
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
