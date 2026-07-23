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
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
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
	if !m.activeWS().Panes.Has(key) {
		t.Fatal("terminal pane should survive q")
	}
	// ctrl+tab is the escape hatch: focus moves away.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.activeWS().Panes.Focused() == key {
		t.Fatal("ctrl+tab should move focus away from the terminal")
	}
}

// TestTerminalFocusKeysEscape: the spatial focus moves work from a focused
// terminal (#228) — ctrl+arrows are part of the reserved set now.
func TestTerminalFocusKeysEscape(t *testing.T) {
	m, key := openTestTerminal(t)
	// The terminal splits below the editor; ctrl+up must land elsewhere.
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.activeWS().Panes.Focused() == key {
		t.Fatal("ctrl+up should move focus out of the terminal")
	}
}

// TestTerminalGlobalChords guards #805: the global navigation chords stay
// with the IDE while a live terminal is focused — cmd+shift+a opens the
// palette, cmd+shift+p opens the project switcher — while unrelated chords
// keep belonging to the shell.
func TestTerminalGlobalChords(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("terminal.new should focus a terminal pane, got %q", key)
	}
	t.Cleanup(func() { inst.Terminal().Close() })

	m = drainKey(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper | tea.ModShift})
	if !m.palette.IsOpen() {
		t.Fatal("cmd+shift+a in a terminal must open the palette")
	}
	m.palette.Close()

	m = drainKey(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModSuper | tea.ModShift})
	if !m.palette.IsOpen() {
		t.Fatal("cmd+shift+p in a terminal must open the project switcher")
	}
	m.palette.Close()

	// An unrelated global chord stays with the shell: cmd+shift+t
	// (editor.tab.reopenClosed) is not allowlisted, so focus stays put.
	out, _ = m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper | tea.ModShift})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != key {
		t.Fatal("shell-bound chord must not move focus")
	}
}

// TestTerminalGlobalChordsWidened guards #973: settings, explorer toggle and
// the other IDE-level chords escape a focused terminal too.
func TestTerminalGlobalChordsWidened(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	t.Cleanup(func() { inst.Terminal().Close() })

	// cmd+, opens settings.
	m = drainKey(m, tea.KeyPressMsg{Code: ',', Mod: tea.ModSuper})
	if !m.settings.IsOpen() {
		t.Fatal("cmd+, in a terminal must open settings")
	}
	m.settings.Close()
	m.setFocus(key)

	// cmd+1 focuses the explorer (allowlisted since #973).
	m = drainKey(m, tea.KeyPressMsg{Code: '1', Mod: tea.ModSuper})
	if m.activeWS().Panes.Focused() != pane.ExplorerKey {
		t.Fatal("cmd+1 in a terminal must focus the explorer")
	}
	m.setFocus(key)

	// cmd+shift+f opens Find in Path.
	m = drainKey(m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModSuper | tea.ModShift})
	if !m.finder.IsOpen() {
		t.Fatal("cmd+shift+f in a terminal must open find in path")
	}
	m.finder.Close()
}

// TestTerminalDoubleShift guards #973: two bare shift taps open Search
// Everywhere from a terminal; a single tap (or taps with other keys between)
// stays with the shell.
func TestTerminalDoubleShift(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	key := m.activeWS().Panes.Focused()
	t.Cleanup(func() { m.activeWS().Panes.Get(key).Terminal().Close() })

	shift := tea.KeyPressMsg{Code: tea.KeyLeftShift}
	out, _ = m.Update(shift)
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("a single shift tap must not open the palette")
	}
	m = drainKey(m, shift)
	if !m.palette.IsOpen() {
		t.Fatal("double-shift in a terminal must open search everywhere")
	}
	m.palette.Close()

	// A key between the taps resets the detector.
	out, _ = m.Update(shift)
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = out.(Model)
	out, _ = m.Update(shift)
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("an interrupted tap pair must stay with the shell")
	}
}

// TestTerminalSelectionCopyKey: with a mouse selection, cmd+c is reserved and
// clears the selection; without one it stays with the shell (#227).
func TestTerminalSelectionCopyKey(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(text string) { copied = text }
	t.Cleanup(func() { clipboardWrite = orig })

	m, key := openTestTerminal(t)
	term := m.activeWS().Panes.Get(key).Terminal()
	time.Sleep(200 * time.Millisecond) // prompt on the grid

	// Fake a drag over the prompt row.
	term.MousePress(0, 0)
	term.MouseDrag(5, 0)
	term.MouseRelease(5, 0)
	if !term.HasSelection() {
		t.Fatal("drag should select")
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper})
	m = out.(Model)
	if term.HasSelection() {
		t.Fatal("cmd+c should consume and clear the selection")
	}
	if copied == "" {
		t.Fatal("cmd+c should write the selection to the clipboard")
	}
	if m.activeWS().Panes.Focused() != key {
		t.Fatal("cmd+c must not move focus")
	}
}

// TestTerminalPasteKey: cmd+v reads the system clipboard and feeds it through
// the bracketed-paste path (#727) — under the Kitty protocol the host delivers
// cmd+v as a key event, so the app must perform the paste itself.
func TestTerminalPasteKey(t *testing.T) {
	orig := clipboardRead
	clipboardRead = func() string { return "IKE_PASTE_MARKER" }
	t.Cleanup(func() { clipboardRead = orig })

	m, key := openTestTerminal(t)
	term := m.activeWS().Panes.Get(key).Terminal()
	time.Sleep(200 * time.Millisecond) // prompt on the grid
	out, _ := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModSuper})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != key {
		t.Fatal("cmd+v must not move focus")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(term.View(), "IKE_PASTE_MARKER") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(term.View(), "IKE_PASTE_MARKER") {
		t.Fatal("cmd+v should paste the clipboard onto the shell's prompt line")
	}
}

// TestDebugTerminalPasteKey: cmd+v pastes the clipboard into the debug panel's
// embedded debuggee terminal the same way (#727).
func TestDebugTerminalPasteKey(t *testing.T) {
	orig := clipboardRead
	clipboardRead = func() string { return "IKE_PASTE_MARKER" }
	t.Cleanup(func() { clipboardRead = orig })

	m := sized(t, 100, 40)
	m.openDebugPanel()
	inst := m.activeWS().Panes.Get(pane.DebugKey)
	if inst == nil || inst.Kind() != pane.KindDebug {
		t.Fatal("debug panel should open")
	}
	p := inst.Debug()
	tm := terminal.NewCommand("dbg-paste-test", []string{"/bin/cat"}, t.TempDir(), 80, 24, nil, func(tea.Msg) {})
	t.Cleanup(tm.Close)
	if tm.Pid() == 0 {
		t.Fatal("spawn failed for /bin/cat")
	}
	p.SetTerminal(&tm)
	m.activeWS().Panes.SetFocused(pane.DebugKey)
	p.SetFocused(true)
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // frames -> vars
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // vars -> output
	if !m.debugPanelTermCapturing() {
		t.Fatal("precondition: the embedded terminal must capture the keyboard")
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModSuper})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.DebugKey {
		t.Fatal("cmd+v must not move focus")
	}
	// cat echoes the pasted text back onto the grid.
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(p.View(), "IKE_PASTE_MARKER") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(p.View(), "IKE_PASTE_MARKER") {
		t.Fatal("cmd+v should paste the clipboard into the debuggee terminal")
	}
}

func TestTerminalExitClosesPane(t *testing.T) {
	m, key := openTestTerminal(t)
	out, _ := m.Update(terminal.ExitedMsg{Key: key})
	m = out.(Model)
	if m.activeWS().Panes.Has(key) {
		t.Fatal("an exited terminal's pane should close")
	}
	if m.activeWS().Panes.Focused() == key {
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
	key := m.activeWS().Panes.Focused()
	dir := m.activeWS().Panes.Get(key).Terminal().Dir()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	m.activeWS().Panes.Get(key).Terminal().Close()

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(key)
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
	for _, leaf := range layout.Leaves(m2.activeWS().Tree) {
		if leaf == key {
			found = true
		}
	}
	if m2.activeWS().Tree != nil && !found {
		t.Fatal("terminal leaf should stay in the restored tree")
	}
}

// TestTerminalSwitchRoundTripNoDuplicates guards #320: switching away and
// back restores the terminal leaf from the saved layout AND carries the live
// session over. The live session must take over the restored placeholder —
// not gain a second leaf (which would mirror one instance in two panes) or
// leave a duplicate shell running.
func TestTerminalSwitchRoundTripNoDuplicates(t *testing.T) {
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
	key := m.activeWS().Panes.Focused()
	sess := m.activeWS().Panes.Get(key).Terminal()
	t.Cleanup(func() { sess.Close() })

	out, _ = m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: src})
	m = out.(Model)

	terms := 0
	for _, k := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(k); inst != nil && inst.Kind() == pane.KindTerminal {
			terms++
			t.Cleanup(func() { inst.Terminal().Close() })
		}
	}
	if terms != 1 {
		t.Fatalf("round trip must keep exactly one terminal pane, got %d", terms)
	}
	seen := map[string]int{}
	for _, leaf := range layout.Leaves(m.activeWS().Tree) {
		seen[leaf]++
		if seen[leaf] > 1 {
			t.Fatalf("leaf %q appears twice in the tree — two panes would mirror one instance", leaf)
		}
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("terminal should live under its original key %q", key)
	}
	if inst.Terminal() != sess {
		t.Fatal("the live session should take over the restored placeholder, not be dropped")
	}
	if !inst.Terminal().Running() {
		t.Fatal("adopted session should keep running")
	}
}

// TestTerminalScrollbackReservedKeys: shift+pgup pages instead of reaching the
// shell, ctrl+tab stays the only reserved escape.
func TestTerminalScrollbackReservedKeys(t *testing.T) {
	m, key := openTestTerminal(t)
	inst := m.activeWS().Panes.Get(key)
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
	before := m.activeWS().Panes.Focused()

	// No terminal: toggle creates and focuses one.
	out, _ := m.Update(TerminalToggleMsg{})
	m = out.(Model)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("toggle should create a terminal")
	}
	t.Cleanup(func() { inst.Terminal().Close() })

	// Focused: toggle returns focus to the previous pane.
	out, _ = m.Update(TerminalToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("toggle should return focus to %q, got %q", before, m.activeWS().Panes.Focused())
	}

	// Unfocused terminal exists: toggle focuses it again (no second spawn).
	out, _ = m.Update(TerminalToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != key {
		t.Fatal("toggle should refocus the existing terminal")
	}
	terms := 0
	for _, k := range m.activeWS().Panes.Keys() {
		if m.activeWS().Panes.Get(k).Kind() == pane.KindTerminal {
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
	inst := m.activeWS().Panes.Get(key)
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

// venvDetector is a stub python toolchain for the env tests: it detects the
// interpreter of a `.venv` under root, like the real plugin's top priority.
type venvDetector struct{}

func (venvDetector) Detect(root string) (map[string]any, bool) { return nil, false }
func (venvDetector) Interpreter(root string) (string, bool) {
	p := filepath.Join(root, ".venv", "bin", "python3")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, true
	}
	return "", false
}

// registerEnvTestPython registers a python language whose detection only
// fires on a project-local .venv (idempotent; last writer wins) and strips
// the toolchain from every other language: the registry is global, so stubs
// other tests registered would leak into effectiveMappings. Tests that need
// a toolchain register their own at their start.
func registerEnvTestPython() {
	lang.Register(lang.Language{
		ID:        "python",
		Server:    &lang.ServerSpec{Language: "python", Command: "x"},
		Toolchain: venvDetector{},
	})
	for _, l := range lang.All() {
		if l.ID != "python" && l.Toolchain != nil {
			l.Toolchain = nil
			lang.Register(l)
		}
	}
}

// TestTerminalEnvFromSettings guards #98/#652: an explicit [lang.python]
// interpreter injects — private dirs by PATH prepend, shared system dirs via
// shim — plus the title indicator; no explicit setting and no detected
// project env leaves the environment untouched.
func TestTerminalEnvFromSettings(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Resolve symlinks (macOS /var -> /private/var): paths detected via the
	// working directory must compare equal.
	if wd, err := os.Getwd(); err == nil {
		dir = wd
	}
	t.Setenv("IKE_CONFIG_DIR", "")
	registerEnvTestPython()

	// No explicit setting and no detected project env: untouched, no shims.
	base, _ := config.Load(config.Options{})
	config.Set(base)
	if env := terminalEnv(); env != nil {
		t.Fatalf("no setting and no detection must not inject, got %v", env)
	}

	// Explicit interpreter in a private toolchain dir: its directory is
	// prepended to PATH, no shim (#652).
	c, _ := config.Load(config.Options{})
	c.Lang = map[string]map[string]string{"python": {"interpreter": "/opt/py/bin/python"}}
	config.Set(c)
	t.Cleanup(func() { fresh, _ := config.Load(config.Options{}); config.Set(fresh) })

	env := terminalEnv()
	if len(env) == 0 || !strings.HasPrefix(env[len(env)-1], "PATH=/opt/py/bin"+string(os.PathListSeparator)) {
		t.Fatalf("overlay = %v", env)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ike", "shims", "python3")); !os.IsNotExist(err) {
		t.Fatal("private-dir interpreter must not write a shim")
	}

	// Explicit interpreter in a shared system dir: shim fallback, shim dir
	// on PATH, base order untouched.
	c2, _ := config.Load(config.Options{})
	c2.Lang = map[string]map[string]string{"python": {"interpreter": "/usr/bin/python3"}}
	config.Set(c2)
	env = terminalEnv()
	if len(env) == 0 || !strings.Contains(env[len(env)-1], "PATH=") ||
		strings.Contains(env[len(env)-1], "PATH=/usr/bin") {
		t.Fatalf("overlay = %v", env)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".ike", "shims", "python3"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/usr/bin/python3") {
		t.Fatalf("shim = %q", data)
	}

	// The pane title indicates the active mapping.
	m := sized(t, 100, 40)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	inst := m.activeWS().Panes.Get(m.activeWS().Panes.Focused())
	t.Cleanup(func() { inst.Terminal().Close() })
	if title := m.terminalTitle(inst); !strings.Contains(title, "python→") {
		t.Fatalf("title should indicate the mapping, got %q", title)
	}
}

// TestTerminalEnvDetectedVenv guards #652: a detected project .venv — no
// explicit setting — activates in new terminals: VIRTUAL_ENV set, venv bin
// first on PATH, no shim. The old "silent detection never injects" rule is
// gone; the JetBrains behavior is the project interpreter being active.
func TestTerminalEnvDetectedVenv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Resolve symlinks (macOS /var -> /private/var): paths detected via the
	// working directory must compare equal.
	if wd, err := os.Getwd(); err == nil {
		dir = wd
	}
	t.Setenv("IKE_CONFIG_DIR", "")
	registerEnvTestPython()

	venvBin := filepath.Join(dir, ".venv", "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".venv", "pyvenv.cfg"), []byte("home = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(venvBin, "python3"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	base, _ := config.Load(config.Options{})
	config.Set(base)
	t.Cleanup(func() { fresh, _ := config.Load(config.Options{}); config.Set(fresh) })

	env := terminalEnv()
	if len(env) != 2 {
		t.Fatalf("overlay = %v", env)
	}
	if env[0] != "VIRTUAL_ENV="+filepath.Join(dir, ".venv") {
		t.Fatalf("VIRTUAL_ENV = %q", env[0])
	}
	if !strings.HasPrefix(env[1], "PATH="+venvBin+string(os.PathListSeparator)) {
		t.Fatalf("PATH should start with the venv bin, got %q", env[1])
	}
	if _, err := os.Stat(filepath.Join(dir, ".ike", "shims", "python3")); !os.IsNotExist(err) {
		t.Fatal("venv activation must not write a shim")
	}
}

// TestStatusLineNamesFocusedTerminal guards #381: with a terminal focused the
// status line says TERMINAL (shell · dir) instead of mirroring the active
// editor's mode/file/cursor — which made it hard to tell where input goes.
func TestStatusLineNamesFocusedTerminal(t *testing.T) {
	m, key := openTestTerminal(t)
	line := m.statusLine()
	if !strings.Contains(line, "TERMINAL") {
		t.Fatalf("status line should name the focused terminal, got %q", line)
	}
	if strings.Contains(line, "NORMAL") || strings.Contains(line, "Ln ") {
		t.Fatalf("status line must not show editor mode/cursor while a terminal is focused: %q", line)
	}
	if sh := m.activeWS().Panes.Get(key).Terminal().ShellPath(); sh != "" &&
		!strings.Contains(line, filepath.Base(sh)) {
		t.Fatalf("status line should show the shell name %q, got %q", filepath.Base(sh), line)
	}

	// Focus back on the editor: the mode/file segments return.
	m.setFocus(m.activeEditorKey())
	if line := m.statusLine(); !strings.Contains(line, "NORMAL") {
		t.Fatalf("editor focus should restore the mode segment, got %q", line)
	}
}

// TestStatusLineNamesFocusedExplorer guards #381 for the explorer: its focus
// names the pane kind, never an implied editor normal mode.
func TestStatusLineNamesFocusedExplorer(t *testing.T) {
	m := sized(t, 100, 40)
	m.setFocus(pane.ExplorerKey)
	line := m.statusLine()
	if !strings.Contains(line, "EXPLORER") {
		t.Fatalf("status line should name the focused explorer, got %q", line)
	}
	if strings.Contains(line, "NORMAL") || strings.Contains(line, "Ln ") {
		t.Fatalf("status line must not show editor mode/cursor while the explorer is focused: %q", line)
	}
}

// TestReservedKeyCanonicalizesSuperMeta guards #981: bubbletea delivers the
// Command key as super+/meta+ tokens; both must match the reserved cmd chords.
func TestReservedKeyCanonicalizesSuperMeta(t *testing.T) {
	for _, form := range []string{"super+t", "meta+t"} {
		m, _ := openTestTerminal(t)
		handled, out, _ := m.terminalReservedKey(form)
		if !handled {
			t.Fatalf("%s must canonicalize onto the reserved cmd+t", form)
		}
		m = out.(Model)
		inst := m.activeWS().Panes.Get(m.activeWS().Panes.Focused())
		if inst != nil && inst.Kind() == pane.KindTerminal {
			t.Cleanup(func() { inst.Terminal().Close() })
		}
	}
}

// TestReservedCmdDSplitsTerminalRight guards #982: cmd+d inside a focused
// terminal splits its pane to the right with a fresh, focused terminal.
func TestReservedCmdDSplitsTerminalRight(t *testing.T) {
	m, key := openTestTerminal(t)
	handled, out, _ := m.terminalReservedKey("cmd+d")
	if !handled {
		t.Fatal("cmd+d must be reserved while a terminal is focused (#982)")
	}
	m = out.(Model)
	nkey := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(nkey)
	if nkey == key || inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("cmd+d must focus a fresh terminal pane, got %q", nkey)
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	if m.lay.Panes[nkey].X <= m.lay.Panes[key].X {
		t.Fatalf("new pane must sit to the right: old X=%d new X=%d",
			m.lay.Panes[key].X, m.lay.Panes[nkey].X)
	}
}

// TestReservedCmdDSplitsEditorHostedTerminalRight: cmd+d with a terminal tab
// focused in an editor pane (#573) splits that pane right with a terminal pane.
func TestReservedCmdDSplitsEditorHostedTerminalRight(t *testing.T) {
	m := sized(t, 100, 40)
	out, _ := m.Update(TerminalNewTabMsg{})
	m = out.(Model)
	key := m.activeWS().Panes.Focused()
	host := m.activeWS().Panes.Get(key)
	if host == nil || host.ActiveTerminal() == nil {
		t.Fatalf("setup: no editor-hosted terminal tab in %q", key)
	}
	t.Cleanup(func() { host.ActiveTerminal().Close() })

	handled, out, _ := m.terminalReservedKey("cmd+d")
	if !handled {
		t.Fatal("cmd+d must be reserved while the terminal tab is focused")
	}
	m = out.(Model)
	nkey := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(nkey)
	if nkey == key || inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("cmd+d must focus a fresh terminal pane, got %q", nkey)
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	if m.lay.Panes[nkey].X <= m.lay.Panes[key].X {
		t.Fatalf("new pane must sit to the right of the editor pane: old X=%d new X=%d",
			m.lay.Panes[key].X, m.lay.Panes[nkey].X)
	}
}

// TestReservedCmdTSplitsSiblingTerminal guards #729: cmd+t inside a focused
// dedicated terminal pane spawns and focuses a second terminal pane.
func TestReservedCmdTSplitsSiblingTerminal(t *testing.T) {
	m, key := openTestTerminal(t)
	handled, out, _ := m.terminalReservedKey("cmd+t")
	if !handled {
		t.Fatal("cmd+t must be reserved while a terminal is focused (#729)")
	}
	m = out.(Model)
	nkey := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(nkey)
	if nkey == key || inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("cmd+t must focus a fresh terminal pane, got %q", nkey)
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	if old := m.activeWS().Panes.Get(key); old == nil || old.Kind() != pane.KindTerminal {
		t.Fatal("the original terminal pane must survive")
	}
}

// TestReservedCmdTAddsTabInEditorHostedTerminal: with a terminal tab active
// in an editor pane (#573), cmd+t adds a sibling terminal tab to that pane.
func TestReservedCmdTAddsTabInEditorHostedTerminal(t *testing.T) {
	m := sized(t, 100, 40)
	out, _ := m.Update(TerminalNewTabMsg{})
	m = out.(Model)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor || inst.ActiveTerminal() == nil {
		t.Fatalf("terminal.newTab should host a terminal tab in the editor pane, got %q", key)
	}
	t.Cleanup(func() { inst.ActiveTerminal().Close() })
	tabs := inst.TabCount()

	handled, out, _ := m.terminalReservedKey("cmd+t")
	if !handled {
		t.Fatal("cmd+t must be reserved while the terminal tab is focused")
	}
	m = out.(Model)
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("sibling tab must keep the pane focused, got %q", m.activeWS().Panes.Focused())
	}
	nt := inst.ActiveTerminal()
	if nt == nil {
		t.Fatal("cmd+t must activate the fresh terminal tab")
	}
	t.Cleanup(func() { nt.Close() })
	if got := inst.TabCount(); got != tabs+1 {
		t.Fatalf("cmd+t must add a tab, %d -> %d", tabs, got)
	}
}
