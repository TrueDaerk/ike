package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/registry"
)

// closeCurrentFixture: dirty buffer in a (active), clean b parked in the
// background — the project.close scenarios (#1355) start here.
func closeCurrentFixture(t *testing.T) (m Model, aRoot, bRoot, dirtyPath string) {
	t.Helper()
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dirtyPath = filepath.Join(a, "f.txt")
	if err := os.WriteFile(dirtyPath, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(a)
	m = switchModel(t)
	tm, _ := m.openPath(dirtyPath, false)
	m = tm.(Model)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"}, {Code: 'X', Text: "X"}, {Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: m.ws.Background()[0]})
	m = out.(Model) // back in a (dirty, active); b parked
	if len(m.ws.Background()) != 1 {
		t.Fatalf("background = %v, want the parked b", m.ws.Background())
	}
	return m, a, m.ws.Background()[0], dirtyPath
}

// TestCloseProjectResumesMRUBackground (#1355): closing a clean current
// project drops its workspace and resumes the most recently used background
// workspace in place.
func TestCloseProjectResumesMRUBackground(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(a)
	m := switchModel(t)
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	closedRoot := m.activeWS().Root

	out, cmd := m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if !sameDir(t, cwd(t), a) {
		t.Fatalf("close must resume the parked project, cwd = %s", cwd(t))
	}
	if len(m.ws.Background()) != 0 {
		t.Fatalf("closed workspace must not stay parked, background = %v", m.ws.Background())
	}
	if m.ws.Peek(closedRoot) != nil {
		t.Fatal("the closed workspace must be dropped")
	}
	if cmd == nil {
		t.Fatal("close should return the follow-up batch")
	}
	if _, err := os.Stat(filepath.Join(b, ".ike", "session.json")); err != nil {
		t.Errorf("closed project's session should persist: %v", err)
	}
}

// TestCloseProjectLastQuits (#1355): with no background workspace,
// project.close quits the IDE (clean state: no guard).
func TestCloseProjectLastQuits(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := switchModel(t)

	out, cmd := m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("closing the last project must quit")
	}
	if msg := cmd(); msg == nil {
		t.Fatalf("expected the quit command, got nil msg")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %#v", msg)
	}
}

// TestCloseProjectLastDirtyOpensQuitGuard (#1355): the quit degradation still
// runs through the #287 guard when buffers are dirty.
func TestCloseProjectLastDirtyOpensQuitGuard(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	m := switchModel(t)
	m = openDirty(t, m, file)

	out, _ := m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if !m.closePromptOpen() {
		t.Fatal("closing the last project with dirty buffers must open the quit guard")
	}
}

// TestCloseProjectBusyPromptsAndCancel (#1355): a dirty active workspace
// prompts before closing; esc keeps everything untouched; d closes discarding
// and resumes the background workspace.
func TestCloseProjectBusyPromptsAndCancel(t *testing.T) {
	m, a, bRoot, dirtyPath := closeCurrentFixture(t)

	out, _ := m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if !m.projectClosePromptOpen() {
		t.Fatal("busy close-current must prompt")
	}
	if len(m.ws.Background()) != 1 {
		t.Fatal("prompting must not touch any workspace")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.projectClosePromptOpen() || !sameDir(t, cwd(t), a) {
		t.Fatal("esc must cancel with the project untouched")
	}

	out, _ = m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = out.(Model)
	if !sameDir(t, m.activeWS().Root, bRoot) {
		t.Fatalf("d must close and resume b, active = %s", m.activeWS().Root)
	}
	if len(m.ws.Background()) != 0 {
		t.Fatalf("closed workspace must not stay parked, background = %v", m.ws.Background())
	}
	if data, _ := os.ReadFile(dirtyPath); string(data) != "one\n" {
		t.Fatalf("d discards, never writes, file = %q", data)
	}
}

// TestCloseProjectSaveThenClose (#1355): s writes the active workspace's
// dirty buffers, then closes it and resumes the background workspace.
func TestCloseProjectSaveThenClose(t *testing.T) {
	m, _, bRoot, dirtyPath := closeCurrentFixture(t)

	out, _ := m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if !m.projectClosePromptOpen() {
		t.Fatal("busy close-current must prompt")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = out.(Model)
	if !sameDir(t, m.activeWS().Root, bRoot) {
		t.Fatalf("s must close after saving, active = %s", m.activeWS().Root)
	}
	data, err := os.ReadFile(dirtyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("s must write the dirty buffer, got %q", data)
	}
}

// TestCloseProjectChordEscapesTerminal (#1360): cmd+shift+w resolves
// project.close even while a live terminal is focused — the running shell
// makes the active workspace busy, so the close guard opens instead of the
// chord being swallowed by the shell.
func TestCloseProjectChordEscapesTerminal(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(a)
	m := sizedWith(t, registry.Global(), 100, 40)
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	out, _ = m.Update(TerminalNewMsg{})
	m = out.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("terminal.new must focus a terminal pane")
	}
	t.Cleanup(func() { inst.Terminal().Close() })

	m = drainKey(m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModSuper | tea.ModShift})
	if !m.projectClosePromptOpen() {
		t.Fatal("cmd+shift+w in a terminal must trigger project.close (busy guard)")
	}
}
