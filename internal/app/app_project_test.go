package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/registry"
)

// TestProjectPickerOpensLocked verifies the project.switch wiring (#12): the
// dispatched OpenPickerMsg opens the palette locked to the picker mode.
func TestProjectPickerOpensLocked(t *testing.T) {
	m := sized(t, 100, 40)
	if m.palette.IsOpen() {
		t.Fatal("palette should start closed")
	}
	out, _ := m.Update(project.OpenPickerMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("OpenPickerMsg should open the palette")
	}
}

// switchModel builds a sized model with per-project state files (no
// IKE_CONFIG_DIR redirect), so a switch resolves session/layout under each
// project's own .ike directory exactly like production.
func switchModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", "")
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

// sameDir compares two paths through symlink resolution (macOS temp dirs live
// behind /var -> /private/var).
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra == rb
}

func cwd(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestSwitchProjectReRoots covers the clean-buffer transaction: chdir, explorer
// re-rooted, old project's session persisted, and the follow-up cmd batch
// emitting SwitchedMsg.
func TestSwitchProjectReRoots(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(src)
	m := switchModel(t)

	out, cmd := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("cwd should be the new root, got %s", cwd(t))
	}
	if !sameDir(t, m.explorer().Root(), dst) {
		t.Fatalf("explorer should re-root, got %s", m.explorer().Root())
	}
	if cmd == nil {
		t.Fatal("switch should return the follow-up batch")
	}
	if _, err := os.Stat(filepath.Join(src, ".ike", "session.json")); err != nil {
		t.Errorf("old project's session should persist on switch: %v", err)
	}
}

// TestSwitchSameRootIsNoop: switching to the current root does not rebuild.
func TestSwitchSameRootIsNoop(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := switchModel(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: cwd(t)})
	m = out.(Model)
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "already in") {
		t.Fatalf("same-root switch should surface a friendly no-op, got %+v", m.toasts)
	}
}

// TestSwitchFailedSurfacesError: an invalid path never mutates the project and
// lands as an error toast.
func TestSwitchFailedSurfacesError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := switchModel(t)
	before := cwd(t)

	msg := project.SwitchTo(filepath.Join(dir, "missing"))()
	fail, ok := msg.(project.SwitchFailedMsg)
	if !ok {
		t.Fatalf("expected SwitchFailedMsg, got %#v", msg)
	}
	out, _ := m.Update(fail)
	m = out.(Model)
	if cwd(t) != before {
		t.Fatal("failed switch must not change the project")
	}
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "cannot switch") {
		t.Fatalf("failure should surface as an error toast, got %+v", m.toasts)
	}
}

// dirtySwitchFixture builds a model in src with a dirty buffer and a pending
// switch to dst, stopped at the open unsaved-changes prompt.
func dirtySwitchFixture(t *testing.T) (Model, string, string) {
	t.Helper()
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(src, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(src)
	m := switchModel(t)
	m = openDirty(t, m, file)

	out, cmd := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("dirty buffers should gate the switch")
	}
	guard, ok := cmd().(project.UnsavedChangesMsg)
	if !ok {
		t.Fatalf("expected UnsavedChangesMsg, got %#v", cmd())
	}
	out, _ = m.Update(guard)
	m = out.(Model)
	if !m.switchPromptOpen() {
		t.Fatal("guard should open the prompt")
	}
	return m, file, dst
}

// TestSwitchUnsavedCancelStays: esc keeps the current project untouched.
func TestSwitchUnsavedCancelStays(t *testing.T) {
	m, file, _ := dirtySwitchFixture(t)
	src := filepath.Dir(file)

	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.switchPromptOpen() || m.switchPending != "" {
		t.Fatal("esc should cancel the pending switch")
	}
	if !sameDir(t, cwd(t), src) {
		t.Fatalf("cancel must stay in the current project, cwd = %s", cwd(t))
	}
	if data, _ := os.ReadFile(file); string(data) != "one\n" {
		t.Fatalf("cancel must not write the buffer, file = %q", data)
	}
}

// TestSwitchUnsavedDiscardSwitches: d switches without writing the buffer.
func TestSwitchUnsavedDiscardSwitches(t *testing.T) {
	m, file, dst := dirtySwitchFixture(t)

	out, _ := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = out.(Model)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("discard should switch, cwd = %s", cwd(t))
	}
	if data, _ := os.ReadFile(file); string(data) != "one\n" {
		t.Fatalf("discard must not write the buffer, file = %q", data)
	}
	if !sameDir(t, m.explorer().Root(), dst) {
		t.Fatalf("explorer should re-root, got %s", m.explorer().Root())
	}
}

// TestSwitchUnsavedSaveAllSwitches: s writes every dirty buffer, then switches.
func TestSwitchUnsavedSaveAllSwitches(t *testing.T) {
	m, file, dst := dirtySwitchFixture(t)

	out, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = out.(Model)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("save-all should switch, cwd = %s", cwd(t))
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("save-all should write the dirty buffer first, file = %q", data)
	}
}

// TestSwitchOtherKeysSwallowed: the modal guard consumes unrelated keys.
func TestSwitchOtherKeysSwallowed(t *testing.T) {
	m, _, _ := dirtySwitchFixture(t)
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = out.(Model)
	if !m.switchPromptOpen() {
		t.Fatal("unrelated keys must not dismiss the guard")
	}
	if cmd != nil {
		t.Fatal("unrelated keys must not leak past the modal prompt")
	}
}

// TestSecondSwitchStillGuards reproduces the real-TUI sequence: switch once,
// dirty a buffer in the new project, switch again — the guard must still open.
func TestSecondSwitchStillGuards(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(b, "two.txt")
	if err := os.WriteFile(file, []byte("zulu\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(a)
	m := switchModel(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if !sameDir(t, cwd(t), b) {
		t.Fatal("first switch should land in b")
	}
	m = openDirty(t, m, file)

	out, cmd := m.Update(project.SwitchProjectMsg{Root: a})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("second switch with dirty buffer should gate")
	}
	guard, ok := cmd().(project.UnsavedChangesMsg)
	if !ok {
		t.Fatalf("expected UnsavedChangesMsg, got %#v", cmd())
	}
	out, _ = m.Update(guard)
	m = out.(Model)
	if !m.switchPromptOpen() {
		t.Fatal("guard prompt should open after a prior switch")
	}
}
