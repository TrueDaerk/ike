package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ctxmenu_float_test.go — the right-click context menu (#1020) draws above
// the popup terminal layer (#3098), matching its mouse precedence.

// assertMenuVisible checks every menu row appears verbatim in the frame at
// the menu's rect.
func assertMenuVisible(t *testing.T, m Model) {
	t.Helper()
	if !m.ctxMenu.IsOpen() {
		t.Fatal("test setup: the context menu must be open")
	}
	x, y := m.ctxMenu.Pos()
	frame := strings.Split(ansi.Strip(m.View().Content), "\n")
	for i, row := range strings.Split(ansi.Strip(m.ctxMenu.View()), "\n") {
		if y+i >= len(frame) {
			t.Fatalf("menu row %d falls off the frame", i)
		}
		got := ansi.Cut(frame[y+i], x, x+ansi.StringWidth(row))
		if got != row {
			t.Fatalf("menu row %d hidden under the popup layer:\nwant %q\ngot  %q", i, row, got)
		}
	}
}

func TestContextMenuAbovePopupBox(t *testing.T) {
	m := openTestPopup(t)
	px, py, _, _ := m.popupTermRect()
	m.ctxMenu.Open(editorContextItems(false), px+2, py+2, m.width, m.height)
	assertMenuVisible(t, m)
	m.popup.blurred = true // the blurred layer (#2309)
	assertMenuVisible(t, m)
}

func TestContextMenuAboveFloatPanel(t *testing.T) {
	m, f := tearOutFirstTab(t, openTestPopup(t))
	m.ctxMenu.Open(editorContextItems(false), f.x+2, f.y+2, m.width, m.height)
	assertMenuVisible(t, m)
}
