package app

import (
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/palette"
	"ike/internal/ui"
)

// floatresize_test.go covers mouse-drag resizing of floating windows (#933):
// press on the border ring grabs an edge/corner, motion resizes, release
// persists via the #774 size store.

// overlayBox returns the centered overlay's top-left and dimensions.
func overlayBox(m Model, view string) (bx, by, w, h int) {
	w, h = lipgloss.Width(view), lipgloss.Height(view)
	return (m.width - w) / 2, (m.height - h) / 2, w, h
}

func TestSettingsMouseResizeRightEdge(t *testing.T) {
	m := sized(t, 120, 40)
	m = step(m, OpenSettingsMsg{})
	if !m.settings.IsOpen() {
		t.Fatal("setup: settings must be open")
	}
	w0, h0 := m.settingsSize()
	bx, by, w, h := overlayBox(m, m.settings.View())

	m = step(m, press(bx+w-1, by+h/2)) // grab the right edge
	if m.floatDrag == nil || m.floatDrag.kind != "settings" || m.floatDrag.sx != 1 || m.floatDrag.sy != 0 {
		t.Fatalf("right-edge press must start a horizontal drag, got %+v", m.floatDrag)
	}
	m = step(m, motion(bx+w-1+4, by+h/2))
	w1, h1 := m.settingsSize()
	if w1 != w0+4 || h1 != h0 {
		t.Fatalf("drag +4 cols: size = (%d,%d), want (%d,%d)", w1, h1, w0+4, h0)
	}
	m = step(m, release(bx+w-1+4, by+h/2))
	if m.floatDrag != nil {
		t.Fatal("release must end the drag")
	}
	if dw, dh := m.winSizes.Get("settings"); dw != 4 || dh != 0 {
		t.Fatalf("persisted delta = (%d,%d), want (4,0)", dw, dh)
	}
}

func TestSettingsMouseResizeCorner(t *testing.T) {
	m := sized(t, 120, 40)
	m = step(m, OpenSettingsMsg{})
	w0, h0 := m.settingsSize()
	bx, by, w, h := overlayBox(m, m.settings.View())

	m = step(m, press(bx+w-1, by+h-1)) // bottom-right corner
	if m.floatDrag == nil || m.floatDrag.sx != 1 || m.floatDrag.sy != 1 {
		t.Fatalf("corner press must grab both axes, got %+v", m.floatDrag)
	}
	m = step(m, motion(bx+w-1-3, by+h-1+2))
	w1, h1 := m.settingsSize()
	if w1 != w0-3 || h1 != h0+2 {
		t.Fatalf("corner drag: size = (%d,%d), want (%d,%d)", w1, h1, w0-3, h0+2)
	}
	m = step(m, release(bx+w-1-3, by+h-1+2))
	if m.floatDrag != nil {
		t.Fatal("release must end the drag")
	}
}

func TestSettingsInteriorClickDoesNotStartDrag(t *testing.T) {
	m := sized(t, 120, 40)
	m = step(m, OpenSettingsMsg{})
	bx, by, w, h := overlayBox(m, m.settings.View())
	// One cell inside the border is content, not a resize handle.
	m = step(m, press(bx+1, by+h/2))
	if m.floatDrag != nil {
		t.Fatal("a click just inside the border must not start a resize")
	}
	m = step(m, press(bx+w/2, by+1))
	if m.floatDrag != nil {
		t.Fatal("an interior click must not start a resize")
	}
	if !m.settings.IsOpen() {
		t.Fatal("interior clicks must stay with the panel")
	}
}

func TestSettingsMouseResizeClampsToTerminal(t *testing.T) {
	m := sized(t, 120, 40)
	m = step(m, OpenSettingsMsg{})
	bx, by, w, h := overlayBox(m, m.settings.View())
	m = step(m, press(bx+w-1, by+h/2))
	m = step(m, motion(bx+w-1+500, by+h/2)) // drag far past the terminal edge
	if w1, _ := m.settingsSize(); w1 > m.width-2 {
		t.Fatalf("width %d must clamp to the terminal (max %d)", w1, m.width-2)
	}
}

func TestPaletteMouseResizeWidth(t *testing.T) {
	m := sized(t, 120, 40)
	m.palette.Open(palette.Context{ContextID: "editor", Root: "."})
	v := m.palette.View()
	bx, by, w, h := overlayBox(m, v)
	m = step(m, press(bx+w-1, by+h/2)) // right edge
	if m.floatDrag == nil || m.floatDrag.kind != "palette" || m.floatDrag.sx != 1 {
		t.Fatalf("palette right-edge press must start a drag, got %+v", m.floatDrag)
	}
	m = step(m, motion(bx+w-1+6, by+h/2))
	if dw, _ := m.winSizes.Get("palette"); dw != 6 {
		t.Fatalf("palette width delta = %d, want 6", dw)
	}
	m = step(m, release(bx+w-1+6, by+h/2))
	if m.floatDrag != nil {
		t.Fatal("release must end the drag")
	}
	if !m.palette.IsOpen() {
		t.Fatal("a resize drag must not close the palette")
	}
}

func TestShellMouseResize(t *testing.T) {
	m := sized(t, 120, 40)
	m.shell.SetContent(ui.ModelContent{Heading: "TEST", Body: func() string {
		return "line one line one line one\nline two\nline three"
	}})
	m.shell.Open()
	v := m.shell.View()
	bx, by, _, h := overlayBox(m, v)
	m = step(m, press(bx, by+h/2)) // left edge
	if m.floatDrag == nil || m.floatDrag.kind != "shell" || m.floatDrag.sx != -1 {
		t.Fatalf("shell left-edge press must start a drag, got %+v", m.floatDrag)
	}
	m = step(m, motion(bx-5, by+h/2)) // dragging left widens
	m = step(m, release(bx-5, by+h/2))
	if m.floatDrag != nil {
		t.Fatal("release must end the drag")
	}
	if !m.shell.IsOpen() {
		t.Fatal("a resize drag must not close the shell")
	}
}
