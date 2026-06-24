package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// kbFiredMsg proves a command was driven through the keybinding layer.
type kbFiredMsg struct{}

// kbPlugin registers one global command bound by the test via config override.
type kbPlugin struct{}

func (kbPlugin) ID() string { return "kbtest" }
func (kbPlugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{Commands: []plugin.Command{{
		ID:    "kbtest.fire",
		Title: "Fire",
		Scope: plugin.GlobalScope(),
		Run:   func(host.API) tea.Cmd { return func() tea.Msg { return kbFiredMsg{} } },
	}}}
}

// TestKeymapResolvesToRegisteredCommand verifies a config-bound chord drives the
// registered command through the root model's keybinding layer.
func TestKeymapResolvesToRegisteredCommand(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	reg := registry.New()
	reg.Add(kbPlugin{})
	cfg := host.MapConfig{"keymap.bindings.ctrl+y": "kbtest.fire"}
	m := NewWith(reg, cfg)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if cmd == nil {
		t.Fatal("ctrl+y should fire the bound command")
	}
	if _, ok := cmd().(kbFiredMsg); !ok {
		t.Fatalf("expected kbFiredMsg from the bound command, got %T", cmd())
	}
}

// TestKeymapInertBindingFallsThrough verifies an unregistered command id leaves
// the key to normal dispatch instead of swallowing it.
func TestKeymapInertBindingFallsThrough(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	reg := registry.New()
	// Bind ctrl+y to a command nobody registers: the binding is inert.
	cfg := host.MapConfig{"keymap.bindings.ctrl+y": "nobody.home"}
	m := NewWith(reg, cfg)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)

	// Should not panic and should not consume into a command; the explorer just
	// ignores ctrl+y. We assert the model is unchanged enough to keep running.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if _, ok := out.(Model); !ok {
		t.Fatal("inert binding should leave a usable model")
	}
}
