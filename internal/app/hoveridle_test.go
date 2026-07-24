package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	"ike/internal/layout"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/registry"
)

// hoveridle_test.go covers the app layer of the mouse-idle hover (#1129):
// idle-tick arming, cancellation, and the fire path (diagnostic popup at the
// pointer + position-carrying hover request through the editor-event seam).

// hoverIdleModel is a sized model with one open Go file, returning the editor
// pane's rect and its gutter width.
func hoverIdleModel(t *testing.T) (Model, layout.Rect, int) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{"editor.line_numbers": "true"})
	out0, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out0.(Model)
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	var r layout.Rect
	found := false
	for key, rect := range m.lay.Panes {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			r, found = rect, true
		}
	}
	if !found {
		t.Fatal("setup: no editor pane rect")
	}
	return m, r, m.activeEditor().GutterWidth()
}

// contentCell returns the screen cell over buffer position (line, col).
func contentCell(r layout.Rect, gw, line, col int) (x, y int) {
	return r.X + paneContentX + gw + col, r.Y + paneContentY + line
}

func TestMouseHoverIdleArmsOncePerCell(t *testing.T) {
	m, r, gw := hoverIdleModel(t)
	x, y := contentCell(r, gw, 0, 2)
	m = step(m, motion(x, y))
	if !m.hoverIdle.pending || !m.hoverIdleTickArmed {
		t.Fatal("motion over editor content must arm the idle wait")
	}
	if m.hoverIdle.pos != (buffer.Position{Line: 0, Col: 2}) {
		t.Fatalf("tracked position = %v, want (0,2)", m.hoverIdle.pos)
	}
	deadline := m.hoverIdle.deadline
	// A second motion event on the same cell must not restart the wait.
	m = step(m, motion(x, y))
	if !m.hoverIdle.deadline.Equal(deadline) {
		t.Fatal("same-cell motion must not restart the idle wait")
	}
	// Motion to a new cell re-tracks with a fresh deadline; the single armed
	// tick stays in flight (#1001) and re-arms itself for the remainder.
	m = step(m, motion(x+1, y))
	if !m.hoverIdle.pending || m.hoverIdle.pos != (buffer.Position{Line: 0, Col: 3}) {
		t.Fatalf("motion to a new cell must re-track, got %+v", m.hoverIdle)
	}
	if m.hoverIdle.deadline.Equal(deadline) {
		t.Fatal("a new cell must get a fresh deadline")
	}
}

func TestMouseHoverIdleCancelsOnKeyAndPress(t *testing.T) {
	m, r, gw := hoverIdleModel(t)
	x, y := contentCell(r, gw, 0, 2)

	m = step(m, motion(x, y))
	m = step(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.hoverIdle.pending {
		t.Fatal("a key must cancel the pending idle wait")
	}

	m = step(m, motion(x, y))
	m = step(m, press(x, y))
	if m.hoverIdle.pending {
		t.Fatal("a click must cancel the pending idle wait")
	}
}

func TestMouseHoverIdleIgnoresGutterAndOtherPanes(t *testing.T) {
	m, r, _ := hoverIdleModel(t)
	// The gutter is not a hover target.
	m = step(m, motion(r.X+paneContentX, r.Y+paneContentY))
	if m.hoverIdle.pending {
		t.Fatal("motion over the gutter must not arm the idle wait")
	}
	// The (unfocused) explorer pane is not a hover target (MVP scope).
	if er, ok := m.lay.Panes[pane.ExplorerKey]; ok {
		m = step(m, motion(er.X+paneContentX+2, er.Y+paneContentY+1))
		if m.hoverIdle.pending {
			t.Fatal("motion over a non-focused, non-editor pane must not arm the idle wait")
		}
	}
}

// recorder captures editor events fanned out by the host.
type recorder struct{ evs []host.EditorEvent }

func (r *recorder) Emit(ev host.EditorEvent) { r.evs = append(r.evs, ev) }

func TestMouseHoverFireOpensDiagnosticPopupAndRequestsPosition(t *testing.T) {
	m, r, gw := hoverIdleModel(t)
	ed := m.activeEditor()
	// A diagnostic covering (0,2), published like the LSP bridge would.
	m = step(m, ilsp.DiagnosticsMsg{Path: ed.Path(), Diagnostics: []ilsp.Diagnostic{{
		Range: buffer.Range{
			Start: buffer.Position{Line: 0, Col: 0},
			End:   buffer.Position{Line: 0, Col: 7},
		},
		Severity: 1, Message: "bad package",
	}}})
	rec := &recorder{}
	m.host.SetEditorEmitter("rec", rec)

	x, y := contentCell(r, gw, 0, 2)
	m = step(m, motion(x, y))
	if !m.hoverIdle.pending {
		t.Fatal("setup: idle wait must be armed")
	}
	// Let the deadline elapse and deliver the tick.
	m.hoverIdle.deadline = time.Now().Add(-time.Millisecond)
	m = step(m, mouseHoverTickMsg{})

	if !ed.HoverOpen() {
		t.Fatal("the diagnostic under the pointer must open the popup without any LSP hover")
	}
	if col, line := ed.HoverAnchor(); col != 2 || line != 0 {
		t.Fatalf("popup anchors at (%d,%d), want the hovered cell (2,0)", col, line)
	}
	// The hover request left through the editor-event seam with the hovered
	// position — not the cursor.
	var req *host.EditorEvent
	for i := range rec.evs {
		if rec.evs[i].Kind == host.EditorHoverRequest {
			req = &rec.evs[i]
		}
	}
	if req == nil {
		t.Fatal("firing must emit an EditorHoverRequest")
	}
	if req.Path != ed.Path() || req.Line != 0 || req.Col != 2 {
		t.Fatalf("hover request = %q (%d,%d), want the hovered cell (0,2)", req.Path, req.Line, req.Col)
	}
}

func TestMouseHoverTickBeforeDeadlineRearms(t *testing.T) {
	m, r, gw := hoverIdleModel(t)
	x, y := contentCell(r, gw, 0, 2)
	m = step(m, motion(x, y))
	m = step(m, mouseHoverTickMsg{}) // fires early (deadline not reached)
	if !m.hoverIdle.pending || !m.hoverIdleTickArmed {
		t.Fatal("an early tick must re-arm for the remaining wait")
	}
	if ed := m.activeEditor(); ed.HoverOpen() {
		t.Fatal("an early tick must not open the popup")
	}
}

func TestMouseHoverMotionOffCellDismissesPopup(t *testing.T) {
	m, r, gw := hoverIdleModel(t)
	ed := m.activeEditor()
	m = step(m, ilsp.DiagnosticsMsg{Path: ed.Path(), Diagnostics: []ilsp.Diagnostic{{
		Range: buffer.Range{
			Start: buffer.Position{Line: 0, Col: 0},
			End:   buffer.Position{Line: 0, Col: 7},
		},
		Severity: 1, Message: "bad package",
	}}})
	x, y := contentCell(r, gw, 0, 2)
	m = step(m, motion(x, y))
	m.hoverIdle.deadline = time.Now().Add(-time.Millisecond)
	m = step(m, mouseHoverTickMsg{})
	if !ed.HoverOpen() {
		t.Fatal("setup: popup must be open")
	}
	// Same-cell motion jitter keeps it open; leaving the cell closes it.
	m = step(m, motion(x, y))
	if !ed.HoverOpen() {
		t.Fatal("same-cell motion must not dismiss the popup")
	}
	m = step(m, motion(x+3, y))
	if ed.HoverOpen() {
		t.Fatal("motion off the hovered cell must dismiss the popup")
	}
}
