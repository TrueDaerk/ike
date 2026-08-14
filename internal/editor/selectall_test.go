package editor

import (
	"testing"

	"ike/internal/editor/buffer"
)

// TestSelectAllSelectsWholeBuffer is the #1861 happy path: cmd+a (select_all)
// enters a linewise visual selection spanning the whole buffer, anchor at the
// first line and cursor on the last — the "ggVG" equivalent.
func TestSelectAllSelectsWholeBuffer(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")

	m, _ = m.runAction("select_all")

	if m.ModeName() != VisualLine {
		t.Fatalf("mode=%v want VisualLine", m.ModeName())
	}
	if m.anchor.Line != 0 {
		t.Fatalf("anchor=%v want line 0", m.anchor)
	}
	if m.cursor.Line != m.buf.LineCount()-1 {
		t.Fatalf("cursor=%v want last line (%d)", m.cursor, m.buf.LineCount()-1)
	}

	// The follow-up ("y" after ggVG) copies the entire buffer.
	m, _ = m.Update(key('y'))
	if m.ModeName() != Normal {
		t.Fatalf("mode after yank=%v want Normal", m.ModeName())
	}
	if got := m.regs.Get(0).Text; got != "alpha\nbeta\ngamma\n" {
		t.Fatalf("yanked = %q, want the whole buffer", got)
	}
}

// TestSelectAllEmptyBufferNoOp: an empty buffer has nothing to select, so the
// command leaves the editor in its current mode instead of opening an empty
// visual selection.
func TestSelectAllEmptyBufferNoOp(t *testing.T) {
	m, _ := loaded(t, "")

	m, _ = m.runAction("select_all")

	if m.ModeName() != Normal {
		t.Fatalf("mode=%v want Normal (no-op on empty buffer)", m.ModeName())
	}
	if m.cursor != (buffer.Position{}) {
		t.Fatalf("cursor=%v want zero position, unmoved", m.cursor)
	}
}

// TestSelectAllWorksInReadOnlyBuffer covers the read-only acceptance
// criterion: selecting (and copying) is allowed, mutations still refused.
func TestSelectAllWorksInReadOnlyBuffer(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n")
	m.SetReadOnly(true)

	m, _ = m.runAction("select_all")
	if m.ModeName() != VisualLine {
		t.Fatalf("mode=%v want VisualLine even read-only", m.ModeName())
	}

	m, _ = m.Update(key('y'))
	if got := m.regs.Get(0).Text; got != "one\ntwo\n" {
		t.Fatalf("yanked = %q, want the whole buffer", got)
	}
}
