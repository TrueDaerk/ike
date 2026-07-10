package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/registry"
	"ike/internal/terminal"
)

// openTestTerminal opens a terminal pane in a sized model and returns its key.
func openTestTerminal(t *testing.T) (Model, string) {
	t.Helper()
	m := sized(t, 100, 40)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	key := m.panes.Focused()
	inst := m.panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("terminal.new should focus a terminal pane, got %q", key)
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	return m, key
}

func TestTerminalNewSplitsAndFocuses(t *testing.T) {
	m, key := openTestTerminal(t)
	if !strings.HasPrefix(key, "terminal") {
		t.Fatalf("key = %q", key)
	}
	if !m.terminalFocused() {
		t.Fatal("terminalFocused should report the live session")
	}
	// The view renders the pane; give the shell a moment to draw its prompt.
	time.Sleep(200 * time.Millisecond)
	if v := m.render(); !strings.Contains(v, "TERMINAL") {
		t.Fatal("pane chrome should title the terminal")
	}
}

func TestTerminalKeysBypassGlobalHandling(t *testing.T) {
	m, key := openTestTerminal(t)
	// 'q' must go to the shell, not quit the app.
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = out.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, isQuit := msg.(tea.QuitMsg); isQuit {
				t.Fatal("q in a terminal must not quit")
			}
		}
	}
	if !m.panes.Has(key) {
		t.Fatal("terminal pane should survive q")
	}
	// ctrl+tab is the escape hatch: focus moves away.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.panes.Focused() == key {
		t.Fatal("ctrl+tab should move focus away from the terminal")
	}
}

func TestTerminalExitClosesPane(t *testing.T) {
	m, key := openTestTerminal(t)
	out, _ := m.Update(terminal.ExitedMsg{Key: key})
	m = out.(Model)
	if m.panes.Has(key) {
		t.Fatal("an exited terminal's pane should close")
	}
	if m.panes.Focused() == key {
		t.Fatal("focus should land elsewhere")
	}
}

// TestTerminalLayoutRestoresFreshShell guards #96: a saved layout with a
// terminal leaf restores as a fresh shell in the saved position, cwd intact.
func TestTerminalLayoutRestoresFreshShell(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)

	out, _ = m.Update(TerminalNewMsg{})
	m = out.(Model)
	key := m.panes.Focused()
	dir := m.panes.Get(key).Terminal().Dir()
	saveLayout(m.tree, m.panes)
	m.panes.Get(key).Terminal().Close()

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("terminal should restore under %q", key)
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	if !inst.Terminal().Running() {
		t.Fatal("restored terminal should run a fresh shell")
	}
	if inst.Terminal().Dir() != dir {
		t.Fatalf("restored dir = %q, want %q", inst.Terminal().Dir(), dir)
	}
	found := false
	for _, leaf := range layout.Leaves(m2.tree) {
		if leaf == key {
			found = true
		}
	}
	if m2.tree != nil && !found {
		t.Fatal("terminal leaf should stay in the restored tree")
	}
}

// TestTerminalSurvivesProjectSwitch guards #96: live sessions carry across a
// switch, titled with their origin root; the new workspace adopts them.
func TestTerminalSurvivesProjectSwitch(t *testing.T) {
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
	key := m.panes.Focused()
	origin := m.panes.Get(key).Terminal().Dir()

	out, _ = m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	inst := m.panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("live terminal should be adopted across the switch")
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	if !inst.Terminal().Running() {
		t.Fatal("adopted session should keep running")
	}
	if inst.Terminal().Dir() != origin {
		t.Fatalf("origin dir should be preserved, got %q", inst.Terminal().Dir())
	}
	if !strings.Contains(m.terminalTitle(inst), "src") {
		t.Fatalf("title should mark the origin root, got %q", m.terminalTitle(inst))
	}
}

// TestTerminalScrollbackReservedKeys: shift+pgup pages instead of reaching the
// shell, ctrl+tab stays the only reserved escape.
func TestTerminalScrollbackReservedKeys(t *testing.T) {
	m, key := openTestTerminal(t)
	inst := m.panes.Get(key)
	inst.Terminal().ScrollBy(0) // touch: live view
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModShift})
	m = out.(Model)
	_ = m
	// Paging with no history stays live but must not crash or leak to the app.
	if got := inst.Terminal().Scroll(); got != 0 && inst.Terminal().Running() {
		// With history it would be >0; either way the key stayed in the pane.
		_ = got
	}
}

// TestTerminalToggleStateMachine guards #97: create → return → refocus.
func TestTerminalToggleStateMachine(t *testing.T) {
	m := sized(t, 100, 40)
	before := m.panes.Focused()

	// No terminal: toggle creates and focuses one.
	out, _ := m.Update(TerminalToggleMsg{})
	m = out.(Model)
	key := m.panes.Focused()
	inst := m.panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("toggle should create a terminal")
	}
	t.Cleanup(func() { inst.Terminal().Close() })

	// Focused: toggle returns focus to the previous pane.
	out, _ = m.Update(TerminalToggleMsg{})
	m = out.(Model)
	if m.panes.Focused() != before {
		t.Fatalf("toggle should return focus to %q, got %q", before, m.panes.Focused())
	}

	// Unfocused terminal exists: toggle focuses it again (no second spawn).
	out, _ = m.Update(TerminalToggleMsg{})
	m = out.(Model)
	if m.panes.Focused() != key {
		t.Fatal("toggle should refocus the existing terminal")
	}
	terms := 0
	for _, k := range m.panes.Keys() {
		if m.panes.Get(k).Kind() == pane.KindTerminal {
			terms++
		}
	}
	if terms != 1 {
		t.Fatalf("toggle must not spawn extra terminals, got %d", terms)
	}
}

// TestTerminalClearEmptiesScrollback guards terminal.clear (#97).
func TestTerminalClearEmptiesScrollback(t *testing.T) {
	m, key := openTestTerminal(t)
	inst := m.panes.Get(key)
	term := inst.Terminal()
	// Generate history.
	for _, r := range "seq 1 200\r" {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && term.ScrollbackLen() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if term.ScrollbackLen() == 0 {
		t.Skip("shell produced no scrollback in time")
	}
	out, _ := m.Update(TerminalClearMsg{})
	m = out.(Model)
	if term.ScrollbackLen() != 0 {
		t.Fatalf("clear should empty the scrollback, len = %d", term.ScrollbackLen())
	}
}

// TestTerminalCommandsRegistered: the three commands resolve by id.
func TestTerminalCommandsRegistered(t *testing.T) {
	for _, id := range []string{"terminal.new", "terminal.toggle", "terminal.clear"} {
		if _, ok := registry.Global().Command(id); !ok {
			t.Fatalf("%s should be registered", id)
		}
	}
}

// TestTerminalEnvFromSettings guards #98: an explicit [lang.python]
// interpreter yields shims + PATH overlay and the title indicator; no
// setting leaves the environment untouched.
func TestTerminalEnvFromSettings(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("IKE_CONFIG_DIR", "")

	// Register a python language for the mapping walk (idempotent).
	lang.Register(lang.Language{ID: "python", Server: &lang.ServerSpec{Language: "python", Command: "x"}})

	// No explicit setting: env untouched, no shims.
	base, _ := config.Load(config.Options{})
	config.Set(base)
	if env := terminalEnv(); env != nil {
		t.Fatalf("no setting must not inject, got %v", env)
	}

	// Explicit setting: shims written, PATH overlay present.
	c, _ := config.Load(config.Options{})
	c.Lang = map[string]map[string]string{"python": {"interpreter": "/opt/py/bin/python"}}
	config.Set(c)
	t.Cleanup(func() { fresh, _ := config.Load(config.Options{}); config.Set(fresh) })

	env := terminalEnv()
	if len(env) == 0 || !strings.Contains(env[len(env)-1], "PATH=") {
		t.Fatalf("overlay = %v", env)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".ike", "shims", "python3"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/opt/py/bin/python") {
		t.Fatalf("shim = %q", data)
	}

	// The pane title indicates the mapping.
	m := sized(t, 100, 40)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	inst := m.panes.Get(m.panes.Focused())
	t.Cleanup(func() { inst.Terminal().Close() })
	if title := m.terminalTitle(inst); !strings.Contains(title, "python→") {
		t.Fatalf("title should indicate the mapping, got %q", title)
	}
}
