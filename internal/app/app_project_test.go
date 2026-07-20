package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
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

// TestSwitchWithDirtyBufferIsSeamless (#777): dirty buffers no longer gate
// the switch — the workspace (buffers included) parks in the background, the
// file stays unwritten, and switching back resumes the edit in place.
func TestSwitchWithDirtyBufferIsSeamless(t *testing.T) {
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
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("dirty switch must proceed seamlessly, cwd = %s", cwd(t))
	}
	if m.switchPromptOpen() {
		t.Fatal("seamless switch must not open the unsaved-changes prompt")
	}
	if cmd == nil {
		t.Fatal("switch should return the follow-up batch")
	}
	if data, _ := os.ReadFile(file); string(data) != "one\n" {
		t.Fatalf("parking must not write the buffer, file = %q", data)
	}

	// Switching back resumes the parked workspace: the edit is still there,
	// still unsaved.
	out, _ = m.Update(project.SwitchProjectMsg{Root: src})
	m = out.(Model)
	found := false
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			ed := inst.TabEditor(i)
			if ed == nil || ed.Path() != file {
				continue
			}
			found = true
			if !ed.Dirty() {
				t.Fatal("resumed buffer must still be dirty")
			}
			if !strings.HasPrefix(ed.Text(), "Xone") {
				t.Fatalf("resumed buffer lost the edit, text = %q", ed.Text())
			}
		}
	}
	if !found {
		t.Fatal("resumed workspace must still hold the dirty buffer")
	}
}

// TestSwitchRoundTripResumesWorkspaceLive (#777): the parked workspace comes
// back as the same live unit — same registry, same running terminal session.
func TestSwitchRoundTripResumesWorkspaceLive(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(src)
	m := switchModel(t)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	srcPanes := m.activeWS().Panes
	termKey := srcPanes.Focused()
	sess := srcPanes.Get(termKey).Terminal()
	t.Cleanup(func() { sess.Close() })

	out, _ = m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if m.activeWS().Panes == srcPanes {
		t.Fatal("the new project must get its own pane registry")
	}
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindTerminal {
			t.Fatal("the src terminal must stay parked, not follow into dst")
		}
	}
	if !sess.Running() {
		t.Fatal("the parked terminal must keep running in the background")
	}

	out, _ = m.Update(project.SwitchProjectMsg{Root: src})
	m = out.(Model)
	if m.activeWS().Panes != srcPanes {
		t.Fatal("switching back must resume the parked registry, not rebuild")
	}
	inst := m.activeWS().Panes.Get(termKey)
	if inst == nil || inst.Kind() != pane.KindTerminal || inst.Terminal() != sess {
		t.Fatal("the resumed workspace must hold the same live terminal session")
	}
	if !inst.Terminal().Running() {
		t.Fatal("the resumed session must still be running")
	}
}
