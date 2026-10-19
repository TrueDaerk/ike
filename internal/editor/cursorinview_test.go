package editor

import (
	"strings"
	"testing"

	"ike/internal/host"
)

// TestCursorInView guards the anchor gate of the intention popup: the caret
// counts as visible only while its display cell lies inside the viewport.
func TestCursorInView(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString(strings.Repeat("x", 120) + "\n")
	}
	m, _ := loaded(t, b.String())
	m.Configure(host.MapConfig{"editor.wrap": "false", "editor.line_numbers": "false"})
	m.SetSize(40, 10)

	m.SetCursor(3, 5)
	if !m.CursorInView() {
		t.Fatal("a caret inside the window must be in view")
	}
	m.SetScroll(20, 0)
	if m.CursorInView() {
		t.Fatal("a caret above the window must not be in view")
	}
	m.SetScroll(0, 0)
	m.SetCursor(50, 5)
	m.SetScroll(0, 0)
	if m.CursorInView() {
		t.Fatal("a caret below the window must not be in view")
	}
	m.SetCursor(3, 2)
	m.SetScroll(0, 60)
	if m.CursorInView() {
		t.Fatal("a caret left of the horizontal window must not be in view")
	}
	m.SetCursor(3, 110)
	m.SetScroll(0, 0)
	if m.CursorInView() {
		t.Fatal("a caret right of the horizontal window must not be in view")
	}
}

// TestCursorInViewSoftWrap: under soft wrap there is no horizontal window,
// so a stale horizontal offset never hides the caret.
func TestCursorInViewSoftWrap(t *testing.T) {
	m := wrapLoaded(t, strings.Repeat("a", 50)+"\nnext\n", 20, 10)
	m.SetCursor(0, 45)
	m.SetScroll(0, 30)
	if !m.CursorInView() {
		t.Fatal("a wrapped caret on a visible row must be in view")
	}
}
