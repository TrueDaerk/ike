package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/layout"
	ilsp "ike/internal/lsp"
)

// lsp_popup_test.go covers the app-side placement of the framed LSP popups
// (#316): they may overflow the owning pane, never the terminal.

// TestLSPPopupOverflowsPaneButNotTerminal opens a hover popup wider than its
// (split, narrow) pane and asserts the framed box crosses the pane border via
// the shift-left rule while every rendered row stays inside the terminal.
func TestLSPPopupOverflowsPaneButNotTerminal(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, "a.txt", "aaa\n")
	m := openApp(t, p)
	m.SplitFocused(layout.ZoneRight) // focus lands on the fresh right editor
	tm, _ := m.openPath(p, false)
	m = tm.(Model)
	r := m.lay.Panes[m.activeWS().Panes.Focused()]

	wide := strings.Repeat("x", 70) // wider than the split pane
	m = dispatch(t, m, ilsp.HoverMsg{Path: p, Contents: wide})
	if !m.activeWS().Panes.FocusedInstance().Editor().HoverOpen() {
		t.Fatal("setup: hover popup should be open")
	}

	rows := strings.Split(m.render(), "\n")
	found := false
	for _, row := range rows {
		if w := lipgloss.Width(row); w > m.width {
			t.Fatalf("row width %d exceeds the terminal width %d", w, m.width)
		}
		plain := ansi.Strip(row)
		idx := strings.Index(plain, wide)
		if idx < 0 {
			continue
		}
		found = true
		// The popup content starts left of the owning pane's left border:
		// it overflowed the pane instead of being wrapped into it.
		if col := len([]rune(plain[:idx])); col >= r.X {
			t.Fatalf("popup content starts at col %d, want left of the pane border %d", col, r.X)
		}
	}
	if !found {
		t.Fatal("hover content should render un-wrapped across the pane border")
	}
}

// TestLSPPopupsCarryRoundedFrame asserts the popup views themselves ship the
// rounded overlay frame (#316).
func TestLSPPopupsCarryRoundedFrame(t *testing.T) {
	dir := t.TempDir()
	p := writeTemp(t, dir, "a.txt", "aaa\n")
	m := openApp(t, p)
	m = dispatch(t, m, ilsp.HoverMsg{Path: p, Contents: "info"})
	v := ansi.Strip(m.activeWS().Panes.FocusedInstance().Editor().HoverView())
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(v, corner) {
			t.Fatalf("hover popup misses frame corner %q:\n%s", corner, v)
		}
	}
}
