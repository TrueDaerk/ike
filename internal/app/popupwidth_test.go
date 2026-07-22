package app

import (
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/palette"
)

// popupwidth_test.go covers ui.popup_max_width (#932): centered popups stop
// growing at the configured cap on large terminals; extra width adds margin.

func TestSettingsWidthCappedOnLargeTerminal(t *testing.T) {
	m := sized(t, 250, 50) // fullscreen-like
	m = step(m, OpenSettingsMsg{})
	if w, _ := m.settingsSize(); w != 110 {
		t.Fatalf("settings width = %d, want the default cap 110", w)
	}
	v := m.settings.View()
	if got := lipgloss.Width(v); got > 112 { // cap + border columns
		t.Fatalf("rendered settings width = %d, exceeds the cap", got)
	}
}

func TestPaletteWidthCappedOnLargeTerminal(t *testing.T) {
	m := sized(t, 250, 50)
	m.palette.SetMaxWidth(110)
	m.palette.Open(palette.Context{ContextID: "editor", Root: "."})
	if got := lipgloss.Width(m.palette.View()); got > 110 {
		t.Fatalf("palette width = %d, exceeds the 110 cap (terminal 250)", got)
	}
	// On a small terminal the cap is irrelevant: the box keeps its normal
	// terminal-bound sizing.
	s := sized(t, 80, 30)
	s.palette.SetMaxWidth(110)
	s.palette.Open(palette.Context{ContextID: "editor", Root: "."})
	if got := lipgloss.Width(s.palette.View()); got > 80-4 {
		t.Fatalf("small-terminal palette width = %d, exceeds the terminal room", got)
	}
}

func TestPaletteResizeDeltaAppliesOnTopOfCap(t *testing.T) {
	// The user's #774 width delta wins over the cap within terminal bounds.
	m := sized(t, 250, 50)
	m.palette.SetMaxWidth(110)
	m.winSizes.Adjust("palette", 20, 0)
	m.palette.Open(palette.Context{ContextID: "editor", Root: "."})
	got := lipgloss.Width(m.palette.View())
	if got <= 110 {
		t.Fatalf("palette width = %d, want the +20 delta on top of the cap", got)
	}
}

func TestPopupMaxWidthZeroDisablesCap(t *testing.T) {
	m := sized(t, 250, 50)
	m.palette.SetMaxWidth(0)
	m.palette.Open(palette.Context{ContextID: "editor", Root: "."})
	if got := lipgloss.Width(m.palette.View()); got < 120 {
		t.Fatalf("uncapped palette width = %d, want the terminal-scaled width", got)
	}
}
