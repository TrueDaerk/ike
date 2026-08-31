package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/pane"
)

// popupwheel_test.go — the wheel outside the popup/float layer's boxes scrolls
// the pane under the pointer while the layer keeps the keyboard (#2343).

// longFileEditor opens a 400-line file in the editor pane and returns the
// model plus a content cell inside that pane, well below its tab bar row.
func longFileEditor(t *testing.T, m Model) (Model, int, int) {
	t.Helper()
	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	path := filepath.Join(t.TempDir(), "long.txt")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	r, ok := m.lay.Panes[ctxEditor]
	if !ok {
		t.Fatal("no editor pane in the layout")
	}
	return m, r.X + paneContentX, r.Y + paneContentY + 2
}

// editorTop reads the editor pane's viewport offset.
func editorTop(t *testing.T, m Model) int {
	t.Helper()
	inst := m.activeWS().Panes.Get(ctxEditor)
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Fatal("the editor pane is gone")
	}
	return inst.Editor().ScrollTop()
}

// TestPopupWheelOutsideScrollsPaneBelow guards #2343: with the popup terminal
// focused, a notch over the editor pane scrolls that editor — and leaves the
// layer focused, visible and unblurred, because scrolling is a reading gesture
// and not a focus gesture.
func TestPopupWheelOutsideScrollsPaneBelow(t *testing.T) {
	m := openTestPopup(t)
	m, ex, ey := longFileEditor(t, m)
	if m.popupLayerHit(ex, ey) {
		t.Fatalf("test setup: cell (%d,%d) must lie outside the popup box", ex, ey)
	}
	if top := editorTop(t, m); top != 0 {
		t.Fatalf("test setup: the editor starts scrolled to %d", top)
	}
	focus := m.activeWS().Panes.Focused()

	m = step(m, tea.MouseWheelMsg{X: ex, Y: ey, Button: tea.MouseWheelDown})

	if top := editorTop(t, m); top == 0 {
		t.Fatal("the wheel outside the popup did not scroll the editor below it")
	}
	if !m.popup.open || m.popup.blurred || !m.popupLayerFocused() {
		t.Fatalf("the wheel must not blur the layer: open=%v blurred=%v focused=%v",
			m.popup.open, m.popup.blurred, m.popupLayerFocused())
	}
	if got := m.activeWS().Panes.Focused(); got != focus {
		t.Fatalf("the wheel moved the pane focus to %q, want %q", got, focus)
	}
	// The keyboard still belongs to the popup's shell: a plain key neither
	// hides the layer nor reaches the panes.
	out, _ := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = out.(Model)
	if !m.popup.open || m.popup.blurred {
		t.Fatal("keys after the wheel must still go to the focused layer")
	}
	// Scrolling back up clamps at the first line.
	for i := 0; i < 60; i++ {
		m = step(m, tea.MouseWheelMsg{X: ex, Y: ey, Button: tea.MouseWheelUp})
	}
	if top := editorTop(t, m); top != 0 {
		t.Fatalf("scrolling up parked at %d, want the first line", top)
	}
}

// TestPopupWheelOverBoxScrollsScrollback guards #2343's other side: over a
// layer box the wheel is unchanged — it pages that box's scrollback and never
// reaches the pane below.
func TestPopupWheelOverBoxScrollsScrollback(t *testing.T) {
	m := openTestPopup(t)
	m, _, _ = longFileEditor(t, m)
	px, py, pw, ph := m.popupTermRect()
	cx, cy := px+pw/2, py+ph/2
	if !m.popupLayerHit(cx, cy) {
		t.Fatalf("test setup: cell (%d,%d) must lie inside the popup box", cx, cy)
	}
	top := editorTop(t, m)

	m = step(m, tea.MouseWheelMsg{X: cx, Y: cy, Button: tea.MouseWheelDown})

	if got := editorTop(t, m); got != top {
		t.Fatalf("a notch over the popup box scrolled the editor to %d, want %d", got, top)
	}
	if !m.popup.open || m.popup.blurred || !m.popupLayerFocused() {
		t.Fatal("the layer must keep focus for its own wheel")
	}
}

// TestFloatPanelWheelOutsideScrollsPaneBelow guards #2343 for the layer's
// other shape: a torn-out floating panel (#1793) passes the wheel through
// outside its box just like the popup box does.
func TestFloatPanelWheelOutsideScrollsPaneBelow(t *testing.T) {
	m := openTestPopup(t)
	m, f := tearOutFirstTab(t, m)
	m.setFloatFocus(f)
	m, ex, ey := longFileEditor(t, m)
	if m.popupLayerHit(ex, ey) {
		t.Fatalf("test setup: cell (%d,%d) must lie outside every layer box", ex, ey)
	}
	if top := editorTop(t, m); top != 0 {
		t.Fatalf("test setup: the editor starts scrolled to %d", top)
	}
	order := append([]*floatTerm(nil), m.floatTerms...)

	m = step(m, tea.MouseWheelMsg{X: ex, Y: ey, Button: tea.MouseWheelDown})

	if top := editorTop(t, m); top == 0 {
		t.Fatal("the wheel outside the float panel did not scroll the editor below it")
	}
	if m.floatFocused() != f || !m.popupLayerFocused() {
		t.Fatal("the wheel must leave the panel focused")
	}
	if len(m.floatTerms) != len(order) || m.floatTerms[len(m.floatTerms)-1] != order[len(order)-1] {
		t.Fatal("the wheel must not change the z-order")
	}
}

// TestPopupClickOutsideStillBlurs guards that #2343's wheel exception is
// wheel-only: a press outside the boxes keeps blurring the layer (#2309).
func TestPopupClickOutsideStillBlurs(t *testing.T) {
	m := openTestPopup(t)
	m, ex, ey := longFileEditor(t, m)
	m = step(m, press(ex, ey))
	if !m.popup.open || !m.popup.blurred {
		t.Fatal("a press outside the boxes must still blur the layer")
	}
}
