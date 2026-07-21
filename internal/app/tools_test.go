package app

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/terminal"
	"ike/internal/theme"
)

// withTools installs a config carrying the given tool entries, restoring the
// previous one on cleanup.
func withTools(t *testing.T, entries ...config.ToolEntry) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Tools.Custom = entries
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

// sleepTool is a tool entry whose process stays alive for the test.
func sleepTool(name string) config.ToolEntry {
	return config.ToolEntry{Name: name, Command: "sleep", Args: []string{"60"}}
}

func TestToolCommandsFromConfig(t *testing.T) {
	withTools(t,
		config.ToolEntry{Name: "lazygit", Command: "lazygit"},
		config.ToolEntry{Name: "My Tool", Command: "mytool"},
		config.ToolEntry{Name: "", Command: "nameless"},   // skipped
		config.ToolEntry{Name: "no-command", Command: ""}, // skipped
	)
	cmds := toolCommands()
	if len(cmds) != 2 {
		t.Fatalf("toolCommands = %d entries, want 2", len(cmds))
	}
	if cmds[0].ID != "tool.lazygit" || cmds[0].Title != "Tool: lazygit" {
		t.Fatalf("first command = %q / %q", cmds[0].ID, cmds[0].Title)
	}
	if cmds[1].ID != "tool.my-tool" {
		t.Fatalf("slugged id = %q, want tool.my-tool", cmds[1].ID)
	}
}

func TestToolSlug(t *testing.T) {
	for in, want := range map[string]string{
		"lazygit":   "lazygit",
		"My Tool":   "my-tool",
		"k9s":       "k9s",
		"a__b!!c":   "a-b-c",
		"Trailing ": "trailing",
	} {
		if got := toolSlug(in); got != want {
			t.Fatalf("toolSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolOpenSpawnsFocusesAndReturns(t *testing.T) {
	withTools(t, sleepTool("watcher"))
	m := sized(t, 100, 40)
	editorKey := m.activeWS().Panes.Focused()

	out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	inst := m.toolPane("watcher")
	if inst == nil {
		t.Fatal("tool.watcher must open a pane")
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	if m.activeWS().Panes.Focused() != inst.Key() {
		t.Fatalf("tool pane must take focus, focused %q", m.activeWS().Panes.Focused())
	}
	if inst.Terminal().Tool() != "watcher" {
		t.Fatalf("tool marker = %q", inst.Terminal().Tool())
	}

	// Re-invoking while focused returns focus to the remembered pane.
	out, _ = m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != editorKey {
		t.Fatalf("second invoke must return focus, got %q want %q", m.activeWS().Panes.Focused(), editorKey)
	}

	// Re-invoking from elsewhere focuses the existing pane, no second spawn.
	out, _ = m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != inst.Key() {
		t.Fatal("third invoke must focus the existing pane")
	}
	count := 0
	for _, key := range m.activeWS().Panes.Keys() {
		if p := m.activeWS().Panes.Get(key); p != nil && p.Kind() == pane.KindTerminal && p.Terminal().Tool() == "watcher" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("tool panes = %d, want 1 (toggle, not respawn)", count)
	}
}

func TestToolPaneChromeIsNotATerminal(t *testing.T) {
	withTools(t, sleepTool("statuswatch"))
	m := sized(t, 100, 40)
	out, _ := m.Update(ToolOpenMsg{Name: "statuswatch"})
	m = out.(Model)
	inst := m.toolPane("statuswatch")
	if inst == nil {
		t.Fatal("tool pane must open")
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	v := m.render()
	if !strings.Contains(v, "⚙ STATUSWATCH") {
		t.Fatal("tool pane chrome must title the tool")
	}
	if strings.Contains(v, "TERMINAL") {
		t.Fatal("tool pane chrome must not look like a terminal")
	}
}

// TestToolExitKeepsPaneOpen guards #810: a tool pane survives its program's
// exit, keeping its layout slot, and shows the footer actions.
func TestToolExitKeepsPaneOpen(t *testing.T) {
	withTools(t, sleepTool("shortlived"))
	m := sized(t, 100, 40)
	out, _ := m.Update(ToolOpenMsg{Name: "shortlived"})
	m = out.(Model)
	inst := m.toolPane("shortlived")
	if inst == nil {
		t.Fatal("tool pane must open")
	}
	key := inst.Key()
	sessKey := inst.Terminal().SessionKey()
	inst.Terminal().Close()
	out, _ = m.Update(terminal.ExitedMsg{Key: sessKey})
	m = out.(Model)
	if !m.activeWS().Panes.Has(key) {
		t.Fatal("tool pane must stay open when its program exits (#810)")
	}
	view := inst.Terminal().View()
	if !strings.Contains(view, "[ Restart (r) ]") || !strings.Contains(view, "[ Close (ctrl+w) ]") {
		t.Fatalf("dead tool pane must show the exit dialog actions, view: %q", view)
	}
	if !strings.Contains(view, "shortlived exited") {
		t.Fatal("exit dialog must name the tool and its exit")
	}
}

// TestToolExitRestartInPlace: r (and the footer click) rerun the command in
// the same pane; the close click removes it.
func TestToolExitRestartAndClose(t *testing.T) {
	withTools(t, sleepTool("shortlived"))
	m := sized(t, 100, 40)
	out, _ := m.Update(ToolOpenMsg{Name: "shortlived"})
	m = out.(Model)
	inst := m.toolPane("shortlived")
	if inst == nil {
		t.Fatal("tool pane must open")
	}
	key := inst.Key()
	t.Cleanup(func() { inst.Terminal().Close() })
	inst.Terminal().Close()
	out, _ = m.Update(terminal.ExitedMsg{Key: inst.Terminal().SessionKey()})
	m = out.(Model)

	// r restarts in place: same pane key, session running again.
	inst.Terminal().Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if !inst.Terminal().Running() {
		t.Fatal("r must restart the tool in place")
	}
	if m.activeWS().Panes.Focused() != key && !m.activeWS().Panes.Has(key) {
		t.Fatal("restart must keep the pane's layout slot")
	}

	// Exit again, then the footer close action removes the pane.
	inst.Terminal().Close()
	out, _ = m.Update(terminal.ExitedMsg{Key: inst.Terminal().SessionKey()})
	m = out.(Model)
	r := m.lay.Panes[key]
	w, gh := inst.Terminal().Size()
	cx, cy := -1, -1
	for y := 0; y <= gh && cx < 0; y++ {
		for x := 0; x < w; x++ {
			if inst.Terminal().DeadActionHit(x, y) == "close" {
				cx, cy = x, y
				break
			}
		}
	}
	if cx < 0 {
		t.Fatal("dead tool pane must expose a close hit zone")
	}
	out, _ = m.paneClick(key, mouseEvent{Mouse: tea.Mouse{X: r.X + paneContentX + cx, Y: r.Y + paneContentY + cy, Button: tea.MouseLeft}, action: mousePress})
	m = out.(Model)
	if m.activeWS().Panes.Has(key) {
		t.Fatal("footer close click must remove the pane")
	}
}

func TestToolUnknownNameIsNoop(t *testing.T) {
	withTools(t)
	m := sized(t, 100, 40)
	before := m.activeWS().Panes.Len()
	out, _ := m.Update(ToolOpenMsg{Name: "ghost"})
	m = out.(Model)
	if m.activeWS().Panes.Len() != before {
		t.Fatal("unknown tool must not open a pane")
	}
}

func TestToolIdentityPersistsAndRestores(t *testing.T) {
	withTools(t, sleepTool("persisted"))
	m := sized(t, 100, 40)
	dir := os.Getenv("IKE_CONFIG_DIR")
	out, _ := m.Update(ToolOpenMsg{Name: "persisted"})
	m = out.(Model)
	inst := m.toolPane("persisted")
	if inst == nil {
		t.Fatal("tool pane must open")
	}
	t.Cleanup(func() { inst.Terminal().Close() })

	// The open already saved the layout; its identity must say "tool".
	_, ids, ok := loadLayout()
	if !ok {
		t.Fatal("layout must be saved")
	}
	id, found := ids[inst.Key()]
	if !found || id.Kind != "tool" || id.Tool != "persisted" {
		t.Fatalf("persisted identity = %+v", id)
	}

	// A fresh model over the same store restores the tool pane, restarting
	// the configured program.
	t.Setenv("IKE_CONFIG_DIR", dir)
	m2 := NewWith(registry.New(), host.MapConfig{})
	out2, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m2 = out2.(Model)
	restored := m2.toolPane("persisted")
	if restored == nil {
		t.Fatal("restore must recreate the tool pane")
	}
	t.Cleanup(func() { restored.Terminal().Close() })
	if restored.Key() != inst.Key() {
		t.Fatalf("restored key = %q, want %q", restored.Key(), inst.Key())
	}
}

func TestToolSpawnEnvCarriesTheme(t *testing.T) {
	pal := theme.DefaultPalette()
	env := toolSpawnEnv(pal)
	var name, bg string
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "IKE_THEME_NAME="); ok {
			name = v
		}
		if v, ok := strings.CutPrefix(e, "IKE_THEME_BACKGROUND="); ok {
			bg = v
		}
	}
	if name == "" {
		t.Fatal("env must carry IKE_THEME_NAME")
	}
	if !strings.HasPrefix(bg, "#") || len(bg) != 7 {
		t.Fatalf("IKE_THEME_BACKGROUND = %q, want #rrggbb", bg)
	}
}

// terminal.toggle must ignore tool panes (#772): with only a tool pane open
// it spawns a new regular terminal instead of focusing the tool.
func TestTerminalToggleIgnoresToolPanes(t *testing.T) {
	withTools(t, sleepTool("watcher"))
	m := sized(t, 100, 40)

	out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	tool := m.toolPane("watcher")
	if tool == nil {
		t.Fatal("tool.watcher must open a pane")
	}
	t.Cleanup(func() { tool.Terminal().Close() })

	out, _ = m.Update(TerminalToggleMsg{})
	m = out.(Model)
	focused := m.activeWS().Panes.FocusedInstance()
	if focused == nil || focused.Kind() != pane.KindTerminal {
		t.Fatalf("toggle must open and focus a terminal, focused %v", m.activeWS().Panes.Focused())
	}
	if focused.Terminal().Tool() != "" {
		t.Fatal("toggle must not focus the tool pane; want a regular terminal")
	}
	t.Cleanup(func() { focused.Terminal().Close() })

	// A second toggle from the terminal returns focus (unchanged semantics).
	out, _ = m.Update(TerminalToggleMsg{})
	m = out.(Model)
	if got := m.activeWS().Panes.FocusedInstance(); got != nil && got.Kind() == pane.KindTerminal && got.Terminal().Tool() == "" {
		t.Fatal("second toggle must leave the regular terminal")
	}
}

// TestQuitPromptsForRunningTool (#821): a running tool pane gates the quit
// (idle shells never do); d quits anyway, s is inert without dirty buffers.
func TestQuitPromptsForRunningTool(t *testing.T) {
	withTools(t, sleepTool("busy"))
	m := sized(t, 100, 40)
	out, _ := m.Update(ToolOpenMsg{Name: "busy"})
	m = out.(Model)
	inst := m.toolPane("busy")
	if inst == nil {
		t.Fatal("tool pane must open")
	}
	t.Cleanup(func() { inst.Terminal().Close() })

	out, _ = m.guardedQuit()
	m = out.(Model)
	if !m.closePromptOpen() {
		t.Fatal("quit must prompt while a tool runs")
	}
	// s without dirty buffers is inert (no save option offered).
	out, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = out.(Model)
	if !m.closePromptOpen() {
		t.Fatal("s must be inert on a running-only prompt")
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = out.(Model)
	quits := false
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			quits = true
		}
	}
	if !quits {
		t.Fatal("d must quit")
	}
}
