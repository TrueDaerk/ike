package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/project"
)

// switchFixture builds two project directories with one file in the first,
// chdirs there and returns the roots plus the file path.
func switchFixture(t *testing.T) (src, dst, file string) {
	t.Helper()
	base := t.TempDir()
	src, dst = filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file = filepath.Join(src, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(src)
	return src, dst, file
}

// TestAutoSaveOnSwitchWritesDirtyBuffer (#2186): with the setting on, an
// orderly switch writes the departing project's dirty buffers and proceeds
// without any prompt.
func TestAutoSaveOnSwitchWritesDirtyBuffer(t *testing.T) {
	_, dst, file := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = openDirty(t, m, file)

	out, cmd := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the switch must return its follow-up batch")
	}
	if m.switchPromptOpen() {
		t.Fatal("a writable dirty buffer must not prompt")
	}
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("the switch must proceed, cwd = %s", cwd(t))
	}
	if data, _ := os.ReadFile(file); string(data) == "one\n" {
		t.Fatalf("the switch must write the dirty buffer, file = %q", data)
	}
}

// TestAutoSaveOnSwitchOffParksDirty (#2186): with the setting off the switch
// behaves as before — the buffer parks unsaved with its workspace.
func TestAutoSaveOnSwitchOffParksDirty(t *testing.T) {
	_, dst, file := switchFixture(t)
	m := switchModel(t) // auto_save_on_switch = false
	m = openDirty(t, m, file)

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("the switch must proceed, cwd = %s", cwd(t))
	}
	if data, _ := os.ReadFile(file); string(data) != "one\n" {
		t.Fatalf("with the gate off nothing may be written, file = %q", data)
	}
}

// TestAutoSaveOnSwitchUnsaveableParksSilently (#2186, reworked by #2396): a
// buffer that cannot be written — here an untitled one — no longer holds the
// switch behind a dialog. It parks dirty with the departing workspace and the
// switch proceeds; the writable buffers were already saved by the gate.
func TestAutoSaveOnSwitchUnsaveableParksSilently(t *testing.T) {
	_, dst, _ := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = dirtyUntitled(t, m)

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if m.shell.IsOpen() {
		t.Fatal("an unsaveable buffer must not open any dialog (#2396)")
	}
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("the switch must proceed, cwd = %s", cwd(t))
	}
}

// TestAutoSaveSwitchStaleBufferIsNotClobbered (#2186, reworked by #2396): a
// buffer whose file changed on disk since the edits is never written by the
// switch — that decision belongs to the conflict guard. The switch proceeds
// and the external change survives; the dirty buffer parks with its
// workspace.
func TestAutoSaveSwitchStaleBufferIsNotClobbered(t *testing.T) {
	_, dst, file := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = openDirty(t, m, file)
	if err := os.WriteFile(file, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The reconcile pass is what marks a dirty buffer stale when its file
	// provably changed (#1515) — the same state an external edit leaves.
	m.reconcileEditors()
	for _, ed := range m.editorViewsForPath(file) {
		if !ed.Stale() {
			t.Fatal("fixture: the buffer must be stale")
		}
	}

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if m.shell.IsOpen() {
		t.Fatal("a stale buffer must not open any dialog (#2396)")
	}
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("the switch must proceed, cwd = %s", cwd(t))
	}
	if data, _ := os.ReadFile(file); string(data) != "external\n" {
		t.Fatalf("the external change must survive, file = %q", data)
	}
}
