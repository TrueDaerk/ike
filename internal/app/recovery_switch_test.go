package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/backup"
	"ike/internal/project"
)

// recovery_switch_test.go covers the switch-then-reopen lifecycle (#2185):
// leaving a project parks its buffers instead of losing them, so the snapshots
// that ride along are *not* crash evidence and must never reach the restore
// prompt — while a snapshot from a session that really died still must.

// openFileAt opens path in m and returns the model plus the editor's pane key.
func openFileAt(t *testing.T, m Model, path string) (Model, string) {
	t.Helper()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	key := m.activeEditorKey()
	if key == "" {
		t.Fatal("open must focus an editor")
	}
	return m, key
}

// projectSnapshots lists the snapshots stored under root's own state dir.
func projectSnapshots(t *testing.T, root string) []backup.Snapshot {
	t.Helper()
	snaps, err := backupServiceFor(root).List()
	if err != nil {
		t.Fatal(err)
	}
	return snaps
}

// TestSwitchBackNoStaleRecoveryPromptDirty is the reported bug: a buffer left
// dirty at switch time keeps its snapshot (crash protection for the parked
// workspace), and coming back must not offer to "recover" the file that is
// already open and dirty.
func TestSwitchBackNoStaleRecoveryPromptDirty(t *testing.T) {
	src, dst := twoProjects(t)
	file := filepath.Join(src, "work.txt")
	if err := os.WriteFile(file, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := switchModel(t)
	m, key := openFileAt(t, m, file)
	m.activeWS().Panes.Get(key).Editor().RestoreText("unsaved edits")

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if m.recoveryOpen() {
		t.Fatal("switching away must not prompt in the incoming project")
	}
	// The parked workspace's edits are still protected against a crash.
	snaps := projectSnapshots(t, src)
	if len(snaps) != 1 || snaps[0].Text != "unsaved edits" {
		t.Fatalf("switch must flush the dirty buffer's snapshot, got %+v", snaps)
	}

	out, _ = m.Update(project.SwitchProjectMsg{Root: src})
	m = out.(Model)
	if m.recoveryOpen() {
		t.Fatalf("switching back must not offer this session's own snapshot: %+v", m.recovery.items)
	}
}

// TestSwitchBackNoStaleRecoveryPromptClean: a buffer saved before the switch
// leaves nothing behind at all — no snapshot, no prompt.
func TestSwitchBackNoStaleRecoveryPromptClean(t *testing.T) {
	src, dst := twoProjects(t)
	file := filepath.Join(src, "clean.txt")
	if err := os.WriteFile(file, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := switchModel(t)
	m, _ = openFileAt(t, m, file)

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: src})
	m = out.(Model)
	if m.recoveryOpen() {
		t.Fatalf("a clean buffer must leave no recoverable snapshot: %+v", m.recovery.items)
	}
	if snaps := projectSnapshots(t, src); len(snaps) != 0 {
		t.Fatalf("clean buffer must not be snapshotted, got %+v", snaps)
	}
}

// TestSwitchIntoCrashedProjectStillPrompts: the session filter is about *this*
// process's snapshots only — entering a project whose previous session died
// with unsaved edits still offers to restore them.
func TestSwitchIntoCrashedProjectStillPrompts(t *testing.T) {
	_, dst := twoProjects(t)
	crashed := filepath.Join(dst, "lost.txt")
	if err := os.WriteFile(crashed, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// switchModel clears the package-wide IKE_CONFIG_DIR redirect, so seed the
	// per-project state dir only once the model (and the env) is in place.
	m := switchModel(t)
	// A snapshot from a session that is gone — what kill -9 leaves behind.
	dead := backup.NewAs(backupDirFor(dst), nil, deadSession)
	if err := dead.Snapshot(backup.Doc{Key: crashed, Path: crashed, Text: "lost edits\n"}); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if !m.recoveryOpen() {
		t.Fatal("a dead session's leftover snapshot must still open the restore prompt")
	}
	if len(m.recovery.items) != 1 || m.recovery.items[0].snap.Path != crashed {
		t.Fatalf("prompt must list the crashed file, got %+v", m.recovery.items)
	}
}

// TestCleanQuitAfterSwitchClearsBothProjects: quitting after a switch removes
// the snapshots of the active *and* the parked project, each from its own
// state directory — so the next launch of either project starts clean.
func TestCleanQuitAfterSwitchClearsBothProjects(t *testing.T) {
	src, dst := twoProjects(t)
	a := filepath.Join(src, "a.txt")
	b := filepath.Join(dst, "b.txt")
	for _, f := range []string{a, b} {
		if err := os.WriteFile(f, []byte("on disk\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := switchModel(t)
	m, key := openFileAt(t, m, a)
	m.activeWS().Panes.Get(key).Editor().RestoreText("edits in src")

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	m, key = openFileAt(t, m, b)
	m.activeWS().Panes.Get(key).Editor().RestoreText("edits in dst")
	if err := m.backupSvc.Snapshot(backup.Doc{Key: b, Path: b, Text: "edits in dst"}); err != nil {
		t.Fatal(err)
	}

	m.backupCleanShutdown()
	for _, root := range []string{src, dst} {
		if snaps := projectSnapshots(t, root); len(snaps) != 0 {
			t.Errorf("clean quit must clear %s, still holds %+v", root, snaps)
		}
	}
}

// TestSwitchBackAfterDiscardingEditsPromptsNothing guards the whole reported
// flow end to end: edit, switch away, come back, and the only thing on screen
// is the resumed buffer — not a modal asking about it.
func TestSwitchBackAfterDiscardingEditsPromptsNothing(t *testing.T) {
	src, dst := twoProjects(t)
	file := filepath.Join(src, "loop.txt")
	if err := os.WriteFile(file, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := switchModel(t)
	m, key := openFileAt(t, m, file)
	m.activeWS().Panes.Get(key).Editor().RestoreText("round one")

	for i := 0; i < 3; i++ {
		out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
		m = out.(Model)
		out, _ = m.Update(project.SwitchProjectMsg{Root: src})
		m = out.(Model)
		if m.recoveryOpen() {
			t.Fatalf("round %d: stale restore prompt after an orderly switch", i)
		}
		if m.shell.IsOpen() {
			t.Fatalf("round %d: unexpected modal open: %q", i, m.shell.View())
		}
	}
}
