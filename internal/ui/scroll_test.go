package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestScrollBoundsClamp(t *testing.T) {
	s := newScroller(20, 3)
	s.SetContent(strings.Repeat("line\n", 50))
	if !s.scrollable() {
		t.Fatal("content taller than viewport should be scrollable")
	}
	if !s.vp.AtTop() {
		t.Fatal("SetContent should reset to top")
	}
	// scroll up at the top stays clamped at top
	s.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if !s.vp.AtTop() {
		t.Fatal("scroll up at top should clamp")
	}
	// G jumps to bottom
	s.Update(tea.KeyPressMsg{Text: "G", Code: 'G'})
	if !s.vp.AtBottom() {
		t.Fatal("G should jump to bottom")
	}
	// scrolling down at the bottom stays clamped
	s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if !s.vp.AtBottom() {
		t.Fatal("scroll down at bottom should clamp")
	}
	// g jumps back to top
	s.Update(tea.KeyPressMsg{Text: "g", Code: 'g'})
	if !s.vp.AtTop() {
		t.Fatal("g should jump to top")
	}
}

func TestScrollIndicatorOnlyWhenOverflowing(t *testing.T) {
	s := newScroller(20, 10)
	s.SetContent("one\ntwo")
	if s.scrollable() {
		t.Fatal("short content should not be scrollable")
	}
	if strings.Contains(s.View(), "%") {
		t.Fatal("non-overflowing content should not show a position indicator")
	}
	s.SetContent(strings.Repeat("line\n", 50))
	if !strings.Contains(s.View(), "%") {
		t.Fatal("overflowing content should show a position indicator")
	}
}
