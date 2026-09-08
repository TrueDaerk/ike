package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/palette"
)

// palette_hint_test.go covers the learnable-shortcuts hint (#2549): a palette
// pick of a bound command toasts its chord, the third pick of an unbound one
// offers palette.bindLastPick, and that command opens the settings keymap
// page on the command.

// pickFromPalette runs the command the query resolves to, the way a user does:
// open, type, enter.
func pickFromPalette(t *testing.T, m Model, query string) Model {
	t.Helper()
	m.palette.SetSize(100, 40)
	m.palette.Open(m.paletteContext())
	for _, r := range query {
		tm, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	// Enter hands back the run message as a command; deliver it like the
	// runtime would (the pick itself is recorded at that point).
	for _, msg := range cmdMsgs(cmd) {
		if run, ok := msg.(palette.RunCommandMsg); ok {
			tm, _ = m.Update(run)
			m = tm.(Model)
		}
	}
	return m
}

// toastsWith counts the toasts whose text contains needle.
func toastsWith(m Model, needle string) int {
	n := 0
	for _, tt := range m.toasts {
		if strings.Contains(tt.text, needle) {
			n++
		}
	}
	return n
}

func TestPaletteHintKeybindDefaultOn(t *testing.T) {
	if got := config.Defaults()["palette.hint_keybind"]; got != "true" {
		t.Fatalf("default palette.hint_keybind = %q, want \"true\"", got)
	}
	if !paletteHintKeybind(nil) || !paletteHintKeybind(host.MapConfig{}) {
		t.Fatal("no config must mean hints on")
	}
	if paletteHintKeybind(host.MapConfig{"palette.hint_keybind": "false"}) {
		t.Fatal("\"false\" must switch the hint off")
	}
}

// TestPalettePickToastsBoundChord is the acceptance criterion: a pick of a
// command bound in the focused context lands an "also: <chord>" toast.
func TestPalettePickToastsBoundChord(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{"keymap.bindings.ctrl+y": "tm.fire"})
	m = pickFromPalette(t, m, "fire")
	if toastsWith(m, "also: ctrl+y") != 1 {
		t.Fatalf("want one \"also: ctrl+y\" toast, got %+v", m.toasts)
	}
	if m.lastPalettePick != "tm.fire" {
		t.Fatalf("lastPalettePick = %q, want tm.fire", m.lastPalettePick)
	}
}

// The setting silences the hint (and the offer), the pick still runs.
func TestPalettePickHintOff(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{
		"keymap.bindings.ctrl+y": "tm.fire",
		"palette.hint_keybind":   "false",
	})
	m = pickFromPalette(t, m, "fire")
	if toastsWith(m, "also:") != 0 {
		t.Fatalf("hint off must not toast, got %+v", m.toasts)
	}
	for i := 0; i < unboundPickOfferAt; i++ {
		m = pickFromPalette(t, m, "slow")
	}
	if toastsWith(m, "bind one") != 0 {
		t.Fatalf("hint off must not offer a key either, got %+v", m.toasts)
	}
	if m.lastPalettePick != "tm.slow" {
		t.Fatalf("lastPalettePick = %q, want tm.slow — the command stays usable with the hint off", m.lastPalettePick)
	}
}

// A binding that only applies in another pane is neither a hint (it would
// not have worked here) nor an unbound command (it has a key).
func TestPalettePickOtherContextBindingIsSilent(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{"keymap.bindings.explorer.ctrl+y": "tm.fire"})
	m.setFocus(m.activeEditorKey())
	for i := 0; i < unboundPickOfferAt; i++ {
		m = pickFromPalette(t, m, "fire")
	}
	if toastsWith(m, "also:") != 0 || toastsWith(m, "bind one") != 0 {
		t.Fatalf("an explorer-only binding must neither hint nor offer in the editor, got %+v", m.toasts)
	}
}

// TestPalettePickOffersBindAfterThree: the third pick of a never-bound command
// raises the offer once, naming palette.bindLastPick's chord; earlier and
// later picks stay quiet.
func TestPalettePickOffersBindAfterThree(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	for i := 1; i < unboundPickOfferAt; i++ {
		m = pickFromPalette(t, m, "slow")
		if toastsWith(m, "bind one") != 0 {
			t.Fatalf("pick %d must not offer yet, got %+v", i, m.toasts)
		}
	}
	m = pickFromPalette(t, m, "slow")
	if toastsWith(m, "Slow: picked 3× from the palette without a key — bind one") != 1 {
		t.Fatalf("the third pick must offer a key, got %+v", m.toasts)
	}
	chord, ok := m.bindings.Binding(bindLastPickCommand)
	if !ok {
		t.Fatal("palette.bindLastPick must ship with a default chord")
	}
	if toastsWith(m, ": "+chord) != 1 {
		t.Fatalf("the offer must name %s's chord %q, got %+v", bindLastPickCommand, chord, m.toasts)
	}
	m = pickFromPalette(t, m, "slow")
	if toastsWith(m, "bind one") != 1 {
		t.Fatalf("the offer must not repeat on the fourth pick, got %+v", m.toasts)
	}

	// Accepting the offer opens the settings keymap page on that command.
	tm, _ := m.Update(BindLastPaletteCommandMsg{})
	m = tm.(Model)
	if !m.settings.IsOpen() {
		t.Fatal("palette.bindLastPick must open the settings panel")
	}
	if v := m.settings.View(); !strings.Contains(v, "filter: tm.slow") {
		t.Fatalf("settings must open the keymap page narrowed to tm.slow:\n%s", v)
	}
}

// Before any palette pick the command explains itself instead of opening an
// empty keymap list.
func TestBindLastPickWithoutPick(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	tm, _ := m.Update(palette.RunCommandMsg{ID: bindLastPickCommand})
	m = tm.(Model)
	tm, _ = m.Update(BindLastPaletteCommandMsg{})
	m = tm.(Model)
	if m.settings.IsOpen() {
		t.Fatal("no pick yet: settings must stay closed")
	}
	if m.lastPalettePick != "" {
		t.Fatalf("the bind command must not record itself as the last pick, got %q", m.lastPalettePick)
	}
	if toastsWith(m, "no command run from the palette yet") != 1 {
		t.Fatalf("want the explanation toast, got %+v", m.toasts)
	}
}
