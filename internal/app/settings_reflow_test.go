package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// settings_reflow_test.go guards #1664: resizing the terminal while the
// floating settings panel is open must recompute the panel size (and thereby
// its three-column grid) from the new window, like every other floating pane.

func TestSettingsPanelReflowsOnWindowResize(t *testing.T) {
	m := sized(t, 120, 40)
	m = step(m, OpenSettingsMsg{})
	if !m.settings.IsOpen() {
		t.Fatal("setup: settings must be open")
	}
	w0, h0 := m.settings.Size()

	m = step(m, tea.WindowSizeMsg{Width: 90, Height: 26})
	w1, h1 := m.settings.Size()
	want, wantH := m.settingsSize()
	if w1 != want || h1 != wantH {
		t.Fatalf("panel size = (%d,%d), want the recomputed (%d,%d)", w1, h1, want, wantH)
	}
	if w1 == w0 && h1 == h0 {
		t.Fatalf("resize must change the panel size, still (%d,%d)", w1, h1)
	}
	if !m.settings.IsOpen() {
		t.Fatal("resize must keep the panel open")
	}

	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if w2, h2 := m.settings.Size(); w2 != w0 || h2 != h0 {
		t.Fatalf("growing back must restore (%d,%d), got (%d,%d)", w0, h0, w2, h2)
	}
}
