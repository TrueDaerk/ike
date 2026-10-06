package app

import "testing"

// popupzen_test.go covers view.zenMode on the popup terminal layer (#2905):
// the focused popup box or floating panel zooms over the chrome-free body,
// the layout pane underneath is never zoomed, a second invocation restores
// geometry and chrome, and hiding the layer drops the zen state.

func zenOnce(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(ZenModeMsg{})
	return out.(Model)
}

// fullBody is the body rect with the status row reclaimed.
func fullBody(m Model) (x, y, w, h int) {
	return 0, m.menuHeight(), m.width, m.height - m.menuHeight()
}

func TestPopupZenTogglesBox(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	m.winSizes.Put(popupTermPosKey, 3, -2)
	x0, y0, w0, h0 := m.popupTermRect()
	normalBodyH := m.bodyRect().H

	m = zenOnce(t, m)
	if !m.popupZenActive() || !m.popup.maximized {
		t.Fatal("view.zenMode with the popup focused must zoom the box")
	}
	if m.zen || m.zoomed != "" {
		t.Fatalf("popup zen must not zoom the layout pane underneath (zen=%v zoomed=%q)", m.zen, m.zoomed)
	}
	bx, by, bw, bh := fullBody(m)
	if x, y, w, h := m.popupTermRect(); x != bx || y != by || w != bw || h != bh {
		t.Fatalf("zen box = %d,%d %dx%d, want %d,%d %dx%d", x, y, w, h, bx, by, bw, bh)
	}
	if h := m.bodyRect().H; h != normalBodyH+1 {
		t.Fatalf("popup zen must hide the status line, body H=%d want %d", h, normalBodyH+1)
	}
	if !m.chromeHidden() {
		t.Fatal("popup zen must hide the chrome")
	}

	m = zenOnce(t, m)
	if m.popupZen || m.popup.maximized || m.chromeHidden() {
		t.Fatal("a second view.zenMode must leave popup zen")
	}
	if x, y, w, h := m.popupTermRect(); x != x0 || y != y0 || w != w0 || h != h0 {
		t.Fatalf("restored box = %d,%d %dx%d, want %d,%d %dx%d", x, y, w, h, x0, y0, w0, h0)
	}
	if m.bodyRect().H != normalBodyH {
		t.Fatal("leaving popup zen must bring the status line back")
	}
	if dx, dy := m.winSizes.Get(popupTermPosKey); dx != 3 || dy != -2 {
		t.Fatalf("popup zen must not write the stored offset, got %d,%d", dx, dy)
	}
}

func TestPopupZenKeepsPriorMaximize(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	m = maximizeOnce(t, m)
	m = zenOnce(t, m)
	if !m.popupZenActive() {
		t.Fatal("zen on a maximized box must still enter popup zen")
	}
	m = zenOnce(t, m)
	if !m.popup.maximized || m.chromeHidden() {
		t.Fatal("leaving zen must keep a maximize that predates it and restore the chrome")
	}
	body := m.bodyRect()
	if x, y, w, h := m.popupTermRect(); x != body.X || y != body.Y || w != body.W || h != body.H {
		t.Fatalf("still-maximized box = %d,%d %dx%d, want the body rect %+v", x, y, w, h, body)
	}
}

func TestPopupZenMaximizeLeavesZen(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	x0, y0, w0, h0 := m.popupTermRect()
	m = zenOnce(t, m)
	m = maximizeOnce(t, m)
	if m.popupZen || m.popup.maximized || m.chromeHidden() {
		t.Fatal("pane.maximize in popup zen must leave zen and unzoom the box")
	}
	if x, y, w, h := m.popupTermRect(); x != x0 || y != y0 || w != w0 || h != h0 {
		t.Fatalf("box = %d,%d %dx%d, want %d,%d %dx%d", x, y, w, h, x0, y0, w0, h0)
	}
}

func TestPopupZenDroppedOnHide(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	normalBodyH := m.bodyRect().H
	m = zenOnce(t, m)
	out, _ := m.Update(TerminalPopupMsg{}) // hide
	m = out.(Model)
	if m.popup.open || m.popup.maximized || m.popupZen || m.chromeHidden() {
		t.Fatalf("hiding must drop popup zen, got popupZen=%v %+v", m.popupZen, m.popup)
	}
	if m.bodyRect().H != normalBodyH {
		t.Fatal("hiding the popup in zen must bring the status line back")
	}
	if r, ok := m.lay.Panes[m.activeWS().Panes.Focused()]; ok && r.Y+r.H > normalBodyH+m.menuHeight() {
		t.Fatalf("panes must be laid out above the status line again, got %+v", r)
	}
}

func TestFloatPanelZenToggles(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	m.tearOutPopupTab(m.popup.inst, 0, 30, 20)
	f := m.floatFocused()
	if f == nil {
		t.Fatal("test setup: the torn-out panel should own the keyboard")
	}
	t.Cleanup(func() { f.inst.CloseTerminalTabs() })
	x0, y0, w0, h0 := f.x, f.y, f.w, f.h

	m = zenOnce(t, m)
	bx, by, bw, bh := fullBody(m)
	if !m.popupZenActive() || f.restore == nil || f.x != bx || f.y != by || f.w != bw || f.h != bh {
		t.Fatalf("zen panel = %d,%d %dx%d, want %d,%d %dx%d", f.x, f.y, f.w, f.h, bx, by, bw, bh)
	}
	if m.zen || m.zoomed != "" || m.popup.maximized {
		t.Fatal("panel zen must zoom neither the layout pane nor the popup box")
	}

	m = zenOnce(t, m)
	if m.popupZen || m.chromeHidden() || f.restore != nil || f.x != x0 || f.y != y0 || f.w != w0 || f.h != h0 {
		t.Fatalf("restored panel = %d,%d %dx%d, want %d,%d %dx%d", f.x, f.y, f.w, f.h, x0, y0, w0, h0)
	}

	// Hiding the layer in panel zen restores both the panel and the chrome.
	m = zenOnce(t, m)
	out, _ := m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if m.popupZen || m.chromeHidden() || f.restore != nil || f.x != x0 || f.w != w0 {
		t.Fatal("hiding the layer must drop panel zen")
	}
}

func TestPopupZenClosedTargetRestoresChrome(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	m.tearOutPopupTab(m.popup.inst, 0, 30, 20)
	f := m.floatFocused()
	if f == nil {
		t.Fatal("test setup: the torn-out panel should own the keyboard")
	}
	m = zenOnce(t, m)
	f.inst.CloseTerminalTabs()
	m.removeFloatTerm(f)
	if m.popupZen || m.chromeHidden() {
		t.Fatal("closing the zen panel must drop popup zen")
	}
}

func TestZenWithoutPopupFocusZoomsPane(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 120, 40)))
	out, _ := m.Update(TerminalPopupMsg{}) // hide
	m = out.(Model)
	m = zenOnce(t, m)
	if !m.zen || m.zoomed == "" || m.popupZen {
		t.Fatal("without popup focus view.zenMode must zen the layout pane")
	}
}
