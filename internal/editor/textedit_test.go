package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestApplyTextEditsMultiLineAndOrder(t *testing.T) {
	m, _ := loaded(t, "func main(){\nx:=1\ny:=2\n}")
	// Two edits in top-down order; ApplyTextEdits must sort bottom-up so the
	// first edit's line numbers stay valid.
	n := m.ApplyTextEdits([]TextEdit{
		{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 0, Text: "\t"},
		{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 0, Text: "\t"},
	})
	if n != 2 {
		t.Fatalf("applied = %d", n)
	}
	want := "func main(){\n\tx:=1\n\ty:=2\n}"
	if got := m.Text(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestApplyTextEditsSpanningLines(t *testing.T) {
	m, _ := loaded(t, "aaa\nbbb\nccc")
	// Replace from middle of line 0 to middle of line 2 with new multi-line text.
	m.ApplyTextEdits([]TextEdit{{StartLine: 0, StartCol: 1, EndLine: 2, EndCol: 2, Text: "X\nY"}})
	if got := m.Text(); got != "aX\nYc" {
		t.Fatalf("text = %q", got)
	}
}

func TestApplyTextEditsSingleUndoUnit(t *testing.T) {
	orig := "one\ntwo\nthree"
	m, _ := loaded(t, orig)
	m.ApplyTextEdits([]TextEdit{
		{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 5, Text: "THREE"},
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, Text: "ONE"},
	})
	if m.Text() != "ONE\ntwo\nTHREE" {
		t.Fatalf("text = %q", m.Text())
	}
	if !m.Dirty() {
		t.Fatal("edits should mark the buffer dirty")
	}
	m = send(m, key('u'))
	if m.Text() != orig {
		t.Fatalf("one undo should revert the whole batch, got %q", m.Text())
	}
}

func TestApplyTextEditsUnicode(t *testing.T) {
	m, _ := loaded(t, "héllo 🌍 wörld")
	// Rune coordinates: replace the emoji (rune 6..7).
	m.ApplyTextEdits([]TextEdit{{StartLine: 0, StartCol: 6, EndLine: 0, EndCol: 7, Text: "🌕"}})
	if got := m.Text(); got != "héllo 🌕 wörld" {
		t.Fatalf("text = %q", got)
	}
}

func TestApplyTextEditsEmptyNoop(t *testing.T) {
	m, _ := loaded(t, "abc")
	if n := m.ApplyTextEdits(nil); n != 0 {
		t.Fatalf("empty edits should be a no-op, got %d", n)
	}
	if m.Dirty() {
		t.Fatal("no-op must not dirty the buffer")
	}
}

// TestVisualEventCarriesAnchor guards the selection seam range formatting
// reads: cursor events in visual mode carry the anchor, leaving visual mode
// clears it.
func TestVisualEventCarriesAnchor(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma")
	var got []Event
	m.SetEmitter(EmitterFunc(func(e Event) { got = append(got, e) }))

	m = send(m, key('v'), key('j')) // char-wise, extend one line down
	last := got[len(got)-1]
	if last.Sel != SelChar {
		t.Fatalf("visual cursor event should carry SelChar, got %+v", last)
	}
	if last.AnchorLine != 0 || last.AnchorCol != 0 || last.Line != 1 {
		t.Errorf("anchor/cursor wrong: %+v", last)
	}

	got = nil
	m = send(m, special(tea.KeyEscape))
	m = send(m, key('V'), key('j'))
	if last := got[len(got)-1]; last.Sel != SelLine {
		t.Fatalf("visual-line event should carry SelLine, got %+v", last)
	}

	got = nil
	m = send(m, special(tea.KeyEscape), key('j'))
	if last := got[len(got)-1]; last.Sel != SelNone {
		t.Fatalf("normal-mode event should carry SelNone, got %+v", last)
	}
}
