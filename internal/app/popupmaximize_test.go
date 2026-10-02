package app

import "testing"

// popupmaximize_test.go covers pane.maximize on the popup terminal layer
// (#2899): the focused popup box or floating panel zooms over the body rect,
// a second invocation restores it, the stored resize/move deltas are never
// touched, and hiding the layer drops the zoom.

func maximizeOnce(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(MaximizePaneMsg{})
	return out.(Model)
}

func TestPopupMaximizeTogglesBox(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	m.winSizes.Put(popupTermPosKey, 3, -2)
	x0, y0, w0, h0 := m.popupTermRect()

	m = maximizeOnce(t, m)
	if !m.popup.maximized || m.zoomed != "" {
		t.Fatalf("pane.maximize with the popup focused must zoom the box, not the pane (zoomed=%q)", m.zoomed)
	}
	body := m.bodyRect()
	x, y, w, h := m.popupTermRect()
	if x != body.X || y != body.Y || w != body.W || h != body.H {
		t.Fatalf("maximized box = %d,%d %dx%d, want the body rect %+v", x, y, w, h, body)
	}
	if tw, th := m.popup.inst.ActiveTerminal().Size(); tw <= paneInterior(w0, paneChromeW) || th <= paneInterior(h0, paneChromeH) {
		t.Fatalf("the hosted shell must grow to the zoomed interior, got %dx%d", tw, th)
	}

	// Resize and move steps are inert on the zoomed box: the stores stay put.
	m.popupTermResize(4, 4, false)
	m.popupTermMoveBy(5, 5, false)
	if dx, dy := m.winSizes.Get(popupTermPosKey); dx != 3 || dy != -2 {
		t.Fatalf("the stored offset must stay untouched while maximized, got %d,%d", dx, dy)
	}

	m = maximizeOnce(t, m)
	if m.popup.maximized {
		t.Fatal("a second pane.maximize must restore the box")
	}
	if x, y, w, h := m.popupTermRect(); x != x0 || y != y0 || w != w0 || h != h0 {
		t.Fatalf("restored box = %d,%d %dx%d, want %d,%d %dx%d", x, y, w, h, x0, y0, w0, h0)
	}
}

func TestPopupMaximizeDroppedOnHide(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	m.winSizes.Put(popupTermPosKey, 2, 1)
	m = maximizeOnce(t, m)
	out, _ := m.Update(TerminalPopupMsg{}) // hide
	m = out.(Model)
	if m.popup.open || m.popup.maximized {
		t.Fatalf("hiding must drop the zoom, got %+v", m.popup)
	}
	if dx, dy := m.winSizes.Get(popupTermPosKey); dx != 2 || dy != 1 {
		t.Fatalf("hiding a maximized popup must leave the stored offset alone, got %d,%d", dx, dy)
	}
}

func TestPinnedPopupMaximizeRestoresDock(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	out, _ := m.Update(TerminalPopupPinMsg{})
	m = out.(Model)
	x0, y0, w0, h0 := m.popupTermRect()

	m = maximizeOnce(t, m)
	body := m.bodyRect()
	if x, y, w, h := m.popupTermRect(); x != body.X || y != body.Y || w != body.W || h != body.H {
		t.Fatalf("maximized pinned box = %d,%d %dx%d, want the body rect %+v", x, y, w, h, body)
	}
	m = maximizeOnce(t, m)
	if x, y, w, h := m.popupTermRect(); x != x0 || y != y0 || w != w0 || h != h0 {
		t.Fatalf("restore must return to the dock strip %d,%d %dx%d, got %d,%d %dx%d", x0, y0, w0, h0, x, y, w, h)
	}
}

func TestFloatPanelMaximizeToggles(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	m.tearOutPopupTab(m.popup.inst, 0, 30, 20)
	f := m.floatFocused()
	if f == nil {
		t.Fatal("test setup: the torn-out panel should own the keyboard")
	}
	t.Cleanup(func() { f.inst.CloseTerminalTabs() })
	x0, y0, w0, h0 := f.x, f.y, f.w, f.h

	m = maximizeOnce(t, m)
	body := m.bodyRect()
	if f.restore == nil || f.x != body.X || f.y != body.Y || f.w != body.W || f.h != body.H {
		t.Fatalf("maximized panel = %d,%d %dx%d, want the body rect %+v", f.x, f.y, f.w, f.h, body)
	}
	if m.zoomed != "" {
		t.Fatal("maximizing a panel must not zoom the layout pane underneath")
	}
	m.resizeFloatTerm(f, -5, -5)
	if f.w != body.W || f.h != body.H {
		t.Fatal("resize steps must be inert on a maximized panel")
	}

	m = maximizeOnce(t, m)
	if f.restore != nil || f.x != x0 || f.y != y0 || f.w != w0 || f.h != h0 {
		t.Fatalf("restored panel = %d,%d %dx%d, want %d,%d %dx%d", f.x, f.y, f.w, f.h, x0, y0, w0, h0)
	}

	// Hiding the layer drops a panel zoom too, restoring its geometry.
	m = maximizeOnce(t, m)
	out, _ := m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if f.restore != nil || f.x != x0 || f.w != w0 {
		t.Fatal("hiding the layer must restore a maximized panel")
	}
}

func TestMaximizeWithoutPopupZoomsPane(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	out, _ := m.Update(TerminalPopupMsg{}) // hide
	m = out.(Model)
	m = maximizeOnce(t, m)
	if m.popup.maximized || m.zoomed == "" {
		t.Fatalf("with no popup focused pane.maximize must zoom the layout pane (zoomed=%q)", m.zoomed)
	}
}
