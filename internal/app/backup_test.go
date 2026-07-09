package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
	"ike/internal/config"
	"ike/internal/editor"
)

// backupClock returns a service clock pinned to t, for seeding aged snapshots.
func backupClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// openDirty opens path in the sized model m and makes its buffer dirty,
// returning the updated model and the editor's pane key.
func openDirtyKey(t *testing.T, m Model, path string) (Model, string) {
	t.Helper()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	key := m.activeEditorKey()
	if key == "" {
		t.Fatal("open must focus an editor")
	}
	m.panes.Get(key).Editor().RestoreText("unsaved edits")
	return m, key
}

func TestBackupDisabledPurgesAtStartup(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		_ = os.WriteFile(filepath.Join(dir, "settings.toml"), []byte("[backup]\nenable = false\n"), 0o644)
		f := filepath.Join(dir, "gone.txt")
		_ = os.WriteFile(f, []byte("x\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: f, Path: f, Text: "r\n"})
	})
	if m.recoveryOpen() {
		t.Fatal("disabled backup must not open the recovery prompt")
	}
	if remainingSnapshots(t) != 0 {
		t.Fatal("disabled backup must purge existing snapshots")
	}
}

func TestBackupPromptCloseRunsAgeGC(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		old := backup.New(backup.Dir(dir), backupClock(time.Now().Add(-8*24*time.Hour)))
		f := filepath.Join(dir, "old.txt")
		_ = os.WriteFile(f, []byte("x\n"), 0o644)
		_ = old.Snapshot(backup.Doc{Key: f, Path: f, Text: "stale\n"})
		g := filepath.Join(dir, "fresh.txt")
		_ = os.WriteFile(g, []byte("y\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: g, Path: g, Text: "recent\n"})
	})
	// Both snapshots are offered first — GC never runs before the prompt.
	if !m.recoveryOpen() || len(m.recovery.items) != 2 {
		t.Fatal("prompt must list every snapshot, aged ones included")
	}
	// Skipping everything closes the prompt; only then does the GC prune.
	m = answer(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	snaps, err := backupService().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || filepath.Base(snaps[0].Path) != "fresh.txt" {
		t.Fatalf("GC must prune only snapshots past max_age_days, got %+v", snaps)
	}
}

func TestBackupChangeSeamMarksAndSnapshotsDirtyBuffer(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {})
	file := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "work.txt")
	if err := os.WriteFile(file, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, key := openDirtyKey(t, m, file)

	// The change seam marks the dirty buffer on the debouncer.
	tm, _ := m.Update(editor.SyncMsg{Path: file, FromKey: key})
	m = tm.(Model)
	if m.backupDeb.Pending() != 1 {
		t.Fatalf("change on a dirty buffer must arm the debounce, pending = %d", m.backupDeb.Pending())
	}

	// Once the deadline passes, the tick snapshots the buffer.
	cmd := m.snapshotDueBackups(time.Now().Add(time.Minute))
	if cmd == nil {
		t.Fatal("expired debounce must produce a snapshot command")
	}
	cmd()
	snaps, err := backupService().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].Text != "unsaved edits" || snaps[0].Path != file {
		t.Fatalf("snapshot must hold the dirty text, got %+v", snaps)
	}
}

func TestBackupSaveRemovesSnapshot(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {})
	file := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "work.txt")
	if err := os.WriteFile(file, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, key := openDirtyKey(t, m, file)
	if err := backupService().Snapshot(backup.Doc{Key: file, Path: file, Text: "unsaved edits"}); err != nil {
		t.Fatal(err)
	}

	// Saving cleans the buffer; the seam then cancels the mark and drops the
	// snapshot.
	tm, _ := m.Update(editor.ActionMsg{Action: "write"})
	m = tm.(Model)
	if m.panes.Get(key).Editor().Dirty() {
		t.Fatal("write action must clean the buffer")
	}
	if cmd := m.backupOnSync(key, file); cmd != nil {
		cmd()
	}
	if remainingSnapshots(t) != 0 {
		t.Fatal("save must remove the buffer's snapshot")
	}
	if m.backupDeb.Pending() != 0 {
		t.Fatal("save must cancel the pending debounce mark")
	}
}

func TestBackupCloseWithDiscardRemovesSnapshot(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {})
	file := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "work.txt")
	if err := os.WriteFile(file, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = openDirtyKey(t, m, file)
	if err := backupService().Snapshot(backup.Doc{Key: file, Path: file, Text: "unsaved edits"}); err != nil {
		t.Fatal(err)
	}

	m.closeFocused()
	if remainingSnapshots(t) != 0 {
		t.Fatal("closing a dirty buffer discards it; its snapshot must go too")
	}
}

func TestBackupCleanQuitRemovesOpenBuffersOnly(t *testing.T) {
	var foreign string
	m := recoverySeed(t, func(svc *backup.Service, dir string) {
		// A snapshot skipped at the restore prompt: its file is never opened.
		foreign = filepath.Join(dir, "skipped.txt")
		_ = os.WriteFile(foreign, []byte("x\n"), 0o644)
		_ = svc.Snapshot(backup.Doc{Key: foreign, Path: foreign, Text: "keep\n"})
	})
	m = answer(m, tea.KeyPressMsg{Code: 's', Text: "s"}) // skip it

	file := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "open.txt")
	if err := os.WriteFile(file, []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = openDirtyKey(t, m, file)
	if err := backupService().Snapshot(backup.Doc{Key: file, Path: file, Text: "unsaved edits"}); err != nil {
		t.Fatal(err)
	}

	if _, cmd := m.quit(); cmd == nil {
		t.Fatal("quit must return the exit command")
	}
	snaps, err := backupService().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].Path != foreign {
		t.Fatalf("clean quit must drop open buffers' snapshots and keep skipped ones, got %+v", snaps)
	}
}

func TestBackupDisableViaReloadPurges(t *testing.T) {
	m := recoverySeed(t, func(svc *backup.Service, dir string) {})
	file := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "work.txt")
	if err := os.WriteFile(file, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backupService().Snapshot(backup.Doc{Key: file, Path: file, Text: "unsaved edits"}); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(config.Options{})
	cfg.Backup.Enable = false
	tm, _ := m.Update(config.ConfigReloadedMsg{Config: cfg})
	m = tm.(Model)
	if remainingSnapshots(t) != 0 {
		t.Fatal("disabling backup via live reload must purge snapshots")
	}
	// And the seam goes quiet: no marks while disabled.
	key := m.activeEditorKey()
	if key == "" {
		key = m.spawnEditor()
	}
	m.panes.Get(key).Editor().RestoreText("dirty again")
	tm, _ = m.Update(editor.SyncMsg{Path: "", FromKey: key})
	m = tm.(Model)
	if m.backupDeb.Pending() != 0 {
		t.Fatal("disabled backup must not mark buffers")
	}
}
