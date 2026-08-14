package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

// ctrlT is the per-context acceptance chord of #1794.
var ctrlT = tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}

// TestCtrlTTerminalContextNewTab (#1794): ctrl+t with a live terminal focused
// resolves the terminal-context binding (terminal.newTab) before PTY
// forwarding — the dedicated pane converts into a tab host (#983) with the
// fresh session focused, exactly like the reserved cmd+t.
func TestCtrlTTerminalContextNewTab(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	key := m.activeWS().Panes.Focused()
	if inst := m.activeWS().Panes.Get(key); inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("terminal.new should focus a terminal pane, got %q", key)
	}

	m = drainKey(m, ctrlT)
	if got := m.activeWS().Panes.Focused(); got != key {
		t.Fatalf("ctrl+t must keep the converted pane focused, got %q", got)
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Fatal("ctrl+t must convert the terminal pane into a tab host (#983)")
	}
	t.Cleanup(inst.CloseTerminalTabs)
	if inst.TabCount() != 2 || inst.ActiveTab() != 1 {
		t.Fatalf("tabs=%d active=%d, want 2 terminal tabs with the new one active",
			inst.TabCount(), inst.ActiveTab())
	}
	for i := 0; i < inst.TabCount(); i++ {
		if inst.TabTerminal(i) == nil {
			t.Fatalf("tab %d must be a terminal tab", i)
		}
	}
}

// TestCtrlTEditorContextNewEmptyTab (#1794): the same chord with an editor
// focused resolves the editor-context binding (editor.tab.new) instead — a
// fresh empty editor tab joins the pane; a second ctrl+t reuses the empty tab
// (#641) instead of stacking blanks.
func TestCtrlTEditorContextNewEmptyTab(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	key := m.activeEditorKey()
	if key == "" {
		t.Fatal("the default layout should include an editor pane")
	}
	m.setFocus(key)
	if ctx := m.focusContext(); ctx != "editor" {
		t.Fatalf("focus context = %q, want editor", ctx)
	}
	inst := m.activeWS().Panes.Get(key)
	// Seed the initial tab so it is not empty and ctrl+t must append.
	inst.Editor().RestoreText("seed")
	tabs := inst.TabCount()

	m = drainKey(m, ctrlT)
	if got := inst.TabCount(); got != tabs+1 {
		t.Fatalf("ctrl+t must add an editor tab, %d -> %d", tabs, got)
	}
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("ctrl+t must keep the editor pane focused, got %q", m.activeWS().Panes.Focused())
	}
	ed := inst.Editor()
	if ed == nil || !ed.IsEmpty() {
		t.Fatal("the new active tab must be an empty editor")
	}

	// The empty tab is reused, not duplicated.
	m = drainKey(m, ctrlT)
	if got := inst.TabCount(); got != tabs+1 {
		t.Fatalf("ctrl+t on an empty tab must reuse it, %d -> %d", tabs+1, got)
	}
	_ = m
}

// TestTerminalContextChordRespectsShellEssentials (#1794): a user override may
// put any chord under the terminal context, but the shell keeps its essential
// strokes — ctrl+c stays with the PTY even when bound, while a harmless chord
// like ctrl+y intercepts and fires.
func TestTerminalContextChordRespectsShellEssentials(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	reg := registry.New()
	reg.Add(kbPlugin{})
	cfg := host.MapConfig{
		"keymap.bindings.terminal.ctrl+c": "kbtest.fire",
		"keymap.bindings.terminal.ctrl+y": "kbtest.fire",
	}
	m := NewWith(reg, cfg)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	out, _ = m.Update(TerminalNewMsg{})
	m = out.(Model)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("terminal.new should focus a terminal pane, got %q", key)
	}
	t.Cleanup(func() { inst.Terminal().Close() })

	handled, _ := m.terminalContextChord(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if handled {
		t.Fatal("ctrl+c must stay with the shell even when terminal-bound")
	}
	handled, cmd := m.terminalContextChord(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if !handled {
		t.Fatal("a terminal-context override must intercept its chord")
	}
	fired := false
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(kbFiredMsg); ok {
			fired = true
		}
	}
	if !fired {
		t.Fatal("the terminal-context binding must dispatch its command")
	}

	// An unmodified key is typing, never a terminal-context interception.
	if handled, _ := m.terminalContextChord(tea.KeyPressMsg{Code: 'q', Text: "q"}); handled {
		t.Fatal("unmodified keys must always reach the shell")
	}
}
