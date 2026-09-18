package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/diag"
	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
)

// renderreuse_test.go covers the "render only on hover change" rule (#2626):
// a coalesced motion burst over nothing that reacts to hover reuses the
// previous frame (a `view/reuse` pass, no `view/render`), while a hover
// transition, a drag step, a wheel notch or a terminal repaint still
// composes a fresh frame.

// renderCounts snapshots the two view pass counters.
func renderCounts() (render, reuse uint64) {
	c := diag.MessageCounts()
	return c["view/render"], c["view/reuse"]
}

// pass runs one Update+View round the way bubbletea's loop does and returns
// the model plus whether View composed a frame (true) or reused one.
func pass(t *testing.T, m Model, msg tea.Msg) (Model, bool) {
	t.Helper()
	r0, u0 := renderCounts()
	out, _ := m.Update(msg)
	mm := out.(Model)
	mm.View()
	r1, u1 := renderCounts()
	switch {
	case r1 == r0+1 && u1 == u0:
		return mm, true
	case r1 == r0 && u1 == u0+1:
		return mm, false
	}
	t.Fatalf("one View must count exactly one pass: render %d→%d reuse %d→%d", r0, r1, u0, u1)
	return mm, false
}

// motionBurst is a coalesced burst carrying motion alone, as the coalescer
// re-injects it for a pointer moving over the window.
func motionBurst(x, y int) coalescedInputMsg {
	m := tea.MouseMotionMsg{X: x, Y: y}
	return coalescedInputMsg{motion: &m}
}

// editorBodyCell returns a cell inside the (empty) editor pane's body.
func editorBodyCell(t *testing.T, m Model, dx, dy int) (int, int) {
	t.Helper()
	r, ok := m.lay.Panes[ctxEditor]
	if !ok {
		t.Fatal("setup: no editor pane rect")
	}
	return r.X + paneContentX + 2 + dx, r.Y + paneContentY + 2 + dy
}

func TestMotionOverNothingReusesFrame(t *testing.T) {
	m := dismissOnboarding(sized(t, 100, 40))
	m.View() // the first frame, as the loop has composed one before any input
	renders := 0
	const n = 12
	for i := 0; i < n; i++ {
		x, y := editorBodyCell(t, m, i, i%3)
		var composed bool
		m, composed = pass(t, m, motionBurst(x, y))
		if composed {
			renders++
		}
	}
	if renders > 1 {
		t.Fatalf("%d motion bursts over an empty editor composed %d frames, want ≤ 1", n, renders)
	}
	// The unfolded message (no coalescer installed) follows the same rule.
	x, y := editorBodyCell(t, m, 20, 0)
	if _, composed := pass(t, m, motion(x, y)); composed {
		t.Fatal("a plain MouseMotionMsg over nothing must reuse the frame too")
	}
}

func TestMotionRendersOnExplorerHoverTransition(t *testing.T) {
	m := dismissOnboarding(sized(t, 100, 40))
	m.View()
	r := m.lay.Panes[pane.ExplorerKey]
	x, y := r.X+paneContentX+2, r.Y+paneContentY
	// Onto the first row: the highlight appears.
	m, composed := pass(t, m, motionBurst(x, y))
	if !composed || m.explorer().HoverRow() != 0 {
		t.Fatalf("entering row 0 must render (composed=%v hover=%d)", composed, m.explorer().HoverRow())
	}
	// Along the same row: nothing changes.
	if _, composed = pass(t, m, motionBurst(x+3, y)); composed {
		t.Fatal("motion along the hovered row must reuse the frame")
	}
	// Down one row: the highlight moves.
	m, composed = pass(t, m, motionBurst(x, y+1))
	if !composed || m.explorer().HoverRow() != 1 {
		t.Fatalf("entering row 1 must render (composed=%v hover=%d)", composed, m.explorer().HoverRow())
	}
	// Leaving the explorer clears the highlight — one more render...
	ex, ey := editorBodyCell(t, m, 0, 0)
	m, composed = pass(t, m, motionBurst(ex, ey))
	if !composed || m.explorer().HoverRow() != -1 {
		t.Fatalf("leaving the explorer must render (composed=%v hover=%d)", composed, m.explorer().HoverRow())
	}
	// ...and the pointer wandering on over the editor is free again.
	if _, composed = pass(t, m, motionBurst(ex+1, ey+1)); composed {
		t.Fatal("motion over the empty editor after the transition must reuse the frame")
	}
}

func TestMotionRendersOnEditorHoverPopupTransition(t *testing.T) {
	m, r, gw := hoverIdleModel(t)
	m = dismissOnboarding(m)
	ed := m.activeEditor()
	m = step(m, ilsp.DiagnosticsMsg{Path: ed.Path(), Diagnostics: []ilsp.Diagnostic{{
		Range:    buffer.Range{Start: buffer.Position{Line: 0, Col: 0}, End: buffer.Position{Line: 0, Col: 7}},
		Severity: 1, Message: "bad package",
	}}})
	m.View()
	x, y := contentCell(r, gw, 0, 2)
	// Resting on a token arms the idle wait: no visual change yet.
	m, composed := pass(t, m, motionBurst(x, y))
	if composed {
		t.Fatal("arming the idle hover wait must not compose a frame")
	}
	if !m.hoverIdle.pending {
		t.Fatal("setup: the idle wait must be armed")
	}
	// An early tick re-arms and draws nothing either.
	m, composed = pass(t, m, mouseHoverTickMsg{gen: m.modelGen})
	if composed {
		t.Fatal("a re-arming hover tick must not compose a frame")
	}
	// The fire opens the popup: that renders.
	m.hoverIdle.deadline = time.Now().Add(-time.Millisecond)
	m, composed = pass(t, m, mouseHoverTickMsg{gen: m.modelGen})
	if !composed || !ed.HoverOpen() {
		t.Fatalf("the hover fire must render (composed=%v open=%v)", composed, ed.HoverOpen())
	}
	// Same-cell jitter keeps it open without a frame.
	if _, composed = pass(t, m, motionBurst(x, y)); composed {
		t.Fatal("same-cell jitter over an open popup must reuse the frame")
	}
	// Leaving the token dismisses the popup: the transition renders.
	m, composed = pass(t, m, motionBurst(x+3, y+1))
	if !composed || ed.HoverOpen() {
		t.Fatalf("dismissing the popup must render (composed=%v open=%v)", composed, ed.HoverOpen())
	}
}

func TestMotionDuringDragsStillRenders(t *testing.T) {
	// A divider drag: every step re-lays out.
	m := dismissOnboarding(sized(t, 100, 40))
	m.View()
	divX := m.lay.Dividers[0].Rect.X
	m, _ = pass(t, m, press(divX, 5))
	m, composed := pass(t, m, motionBurst(divX+5, 5))
	if !composed {
		t.Fatal("a divider drag step must render")
	}
	m, _ = pass(t, m, release(divX+5, 5))

	// An editor drag selection: every step extends the selection.
	m2, r, gw := hoverIdleModel(t)
	m2 = dismissOnboarding(m2)
	m2.View()
	x, y := contentCell(r, gw, 0, 0)
	m2, _ = pass(t, m2, press(x, y))
	if _, composed = pass(t, m2, motionBurst(x+4, y)); !composed {
		t.Fatal("a drag-selection step must render")
	}
}

func TestBurstWithWheelOrTerminalRepaintRenders(t *testing.T) {
	m := dismissOnboarding(sized(t, 100, 40))
	m.View()
	x, y := editorBodyCell(t, m, 0, 0)
	mo := tea.MouseMotionMsg{X: x, Y: y}
	// Motion plus a wheel notch: the notch may have scrolled something.
	burst := coalescedInputMsg{wheels: []tea.MouseWheelMsg{{X: x, Y: y, Button: tea.MouseWheelDown}}, motion: &mo}
	if _, composed := pass(t, m, burst); !composed {
		t.Fatal("a burst carrying a wheel notch must render")
	}
	// Motion plus a terminal repaint key: the grid changed under the frame.
	burst = coalescedInputMsg{motion: &mo, termKeys: []string{"term-1"}}
	if _, composed := pass(t, m, burst); !composed {
		t.Fatal("a burst carrying a terminal repaint must render")
	}
}

func TestReuseVerdictNeverOutlivesItsPass(t *testing.T) {
	m := dismissOnboarding(sized(t, 100, 40))
	m.View()
	x, y := editorBodyCell(t, m, 0, 0)
	// A no-op motion pass followed by a key press: the key's pass must
	// compose, the earlier verdict is gone.
	m, _ = pass(t, m, motionBurst(x, y))
	if _, composed := pass(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"}); !composed {
		t.Fatal("a key press after a no-op motion must compose a fresh frame")
	}
	// A no-op motion whose previous pass never reached View (the loop was
	// interrupted, a test sequenced Updates back-to-back) composes too: the
	// cached frame is not the previous pass's.
	out, _ := m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m = out.(Model)
	if _, composed := pass(t, m, motionBurst(x+1, y)); !composed {
		t.Fatal("a no-op motion must not reuse a frame older than the previous pass")
	}
}
