package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
)

// lineops_test.go covers the #2400 line/document commands: the JetBrains
// chords that had no command behind them at all.

// selectLines puts m into a linewise-ish charwise selection spanning the
// given lines, the shape a shift+arrow or mouse drag leaves behind.
func selectLines(m *Model, first, last int) {
	m.cursor = buffer.Position{Line: first}
	m.enterVisual(Visual)
	m.cursor = buffer.Position{Line: last}
}

func TestMoveLineUpWithoutSelection(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.cursor = buffer.Position{Line: 1, Col: 2}

	m, _ = m.runAction("move_line_up")

	if got := m.Text(); got != "beta\nalpha\ngamma" {
		t.Fatalf("text = %q", got)
	}
	if m.cursor.Line != 0 || m.cursor.Col != 2 {
		t.Fatalf("cursor = %v, want line 0 col 2 (the line it rode along with)", m.cursor)
	}
}

func TestMoveLineDownWithoutSelection(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.cursor = buffer.Position{Line: 0, Col: 3}

	m, _ = m.runAction("move_line_down")

	if got := m.Text(); got != "beta\nalpha\ngamma" {
		t.Fatalf("text = %q", got)
	}
	if m.cursor.Line != 1 || m.cursor.Col != 3 {
		t.Fatalf("cursor = %v, want line 1 col 3", m.cursor)
	}
}

// TestMoveLinesMovesWholeSelection: with a selection every touched line moves
// together, and the selection keeps covering the same text so the chord
// repeats.
func TestMoveLinesMovesWholeSelection(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\n")
	selectLines(&m, 1, 2)

	m, _ = m.runAction("move_line_down")

	if got := m.Text(); got != "one\nfour\ntwo\nthree" {
		t.Fatalf("text = %q", got)
	}
	if m.anchor.Line != 2 || m.cursor.Line != 3 {
		t.Fatalf("selection = %d..%d, want 2..3", m.anchor.Line, m.cursor.Line)
	}
}

// TestMoveLinesAtBufferEdgesNoOp: there is nothing to swap with above the
// first line or below the last, and the buffer must stay untouched.
func TestMoveLinesAtBufferEdgesNoOp(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	m.cursor = buffer.Position{Line: 0}
	m, _ = m.runAction("move_line_up")
	if got := m.Text(); got != "alpha\nbeta" {
		t.Fatalf("move up on the first line changed the buffer: %q", got)
	}
	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}
	m, _ = m.runAction("move_line_down")
	if got := m.Text(); got != "alpha\nbeta" {
		t.Fatalf("move down on the last line changed the buffer: %q", got)
	}
}

// TestMoveLineIsOneUndoStep: the swap is a single edit, so one undo restores
// the original order.
func TestMoveLineIsOneUndoStep(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.cursor = buffer.Position{Line: 1}
	m, _ = m.runAction("move_line_up")
	m, _ = m.runAction("undo")
	if got := m.Text(); got != "alpha\nbeta\ngamma" {
		t.Fatalf("after undo = %q, want the original order", got)
	}
}

func TestDeleteLineWithoutSelection(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.cursor = buffer.Position{Line: 1, Col: 3}

	m, _ = m.runAction("delete_line")

	if got := m.Text(); got != "alpha\ngamma" {
		t.Fatalf("text = %q", got)
	}
	if m.cursor.Line != 1 || m.cursor.Col != 3 {
		t.Fatalf("cursor = %v, want line 1 col 3 (same column, following line)", m.cursor)
	}
}

// TestDeleteLineWithMultiLineSelection: every line the selection touches goes,
// partial lines included, and the selection ends with it.
func TestDeleteLineWithMultiLineSelection(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\n")
	selectLines(&m, 1, 2)

	m, _ = m.runAction("delete_line")

	if got := m.Text(); got != "one\nfour" {
		t.Fatalf("text = %q", got)
	}
	if m.ModeName() != Normal {
		t.Fatalf("mode = %v, want Normal after the deletion", m.ModeName())
	}
}

// TestDeleteLineLastLine: deleting the last line takes the preceding break
// with it, so no empty line is left behind.
func TestDeleteLineLastLine(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta")
	m.cursor = buffer.Position{Line: 1}

	m, _ = m.runAction("delete_line")

	if got := m.Text(); got != "alpha" {
		t.Fatalf("text = %q, want just the first line", got)
	}
}

// TestDeleteLineInInsertModeKeepsSessionBehaviour: mid-insert the command
// defers to the open session's own line kill (#955) rather than committing
// the insert first — the editor stays in insert mode.
func TestDeleteLineInInsertModeKeepsSessionBehaviour(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	m.cursor = buffer.Position{Line: 1}
	m, _ = m.Update(key('i'))

	m, _ = m.runAction("delete_line")

	if m.ModeName() != Insert {
		t.Fatalf("mode = %v, want Insert", m.ModeName())
	}
	if got := m.Text(); got != "alpha\ngamma" {
		t.Fatalf("text = %q", got)
	}
}

func TestDeleteWordBackward(t *testing.T) {
	m, _ := loaded(t, "alpha beta gamma\n")
	m.cursor = buffer.Position{Line: 0, Col: 11} // on the "g" of gamma

	m, _ = m.runAction("delete_word_backward")

	if got := line(m, 0); got != "alpha gamma" {
		t.Fatalf("line = %q", got)
	}
}

// TestDeleteWordBackwardAtLineStart joins with the previous line, exactly
// like the insert-mode kill does — the word before the cursor is the tail of
// the line above.
func TestDeleteWordBackwardAtLineStart(t *testing.T) {
	m, _ := loaded(t, "alpha beta\ngamma\n")
	m.cursor = buffer.Position{Line: 1, Col: 0}

	m, _ = m.runAction("delete_word_backward")

	if got := m.Text(); got != "alpha gamma" {
		t.Fatalf("text = %q", got)
	}
}

// TestDeleteWordBackwardInInsertMode keeps the session's kill (#246): the
// editor stays in insert mode.
func TestDeleteWordBackwardInInsertMode(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m.cursor = buffer.Position{Line: 0, Col: 10}
	m, _ = m.Update(modKey('a', 0)) // append: enter insert at the line end

	m, _ = m.runAction("delete_word_backward")

	if m.ModeName() != Insert {
		t.Fatalf("mode = %v, want Insert", m.ModeName())
	}
	if got := line(m, 0); got != "alpha " {
		t.Fatalf("line = %q", got)
	}
}

func TestDocStartAndDocEnd(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\n")
	m.cursor = buffer.Position{Line: 1}

	m, _ = m.runAction("doc_end")
	if m.cursor.Line != m.buf.LineCount()-1 {
		t.Fatalf("doc_end cursor = %v, want the last line (%d)", m.cursor, m.buf.LineCount()-1)
	}

	m, _ = m.runAction("doc_start")
	if m.cursor.Line != 0 {
		t.Fatalf("doc_start cursor = %v, want line 0", m.cursor)
	}
}

// TestDocEndOpensFold: landing inside a collapsed fold opens it, vim's
// foldopen behaviour every other jump already has (scroll → unfoldAtCursor).
func TestDocEndOpensFold(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\nfive\n")
	m.folded = map[int]int{2: 4}
	m.cursor = buffer.Position{Line: 0}

	m, _ = m.runAction("doc_end")

	if m.lineHidden(m.cursor.Line) {
		t.Fatalf("cursor line %d is still hidden by a fold", m.cursor.Line)
	}
	if len(m.folded) != 0 {
		t.Fatalf("fold survived the jump: %v", m.folded)
	}
}

// TestDocEndExtendsSelection: in a visual mode the motion extends the
// selection instead of dropping it, like G does.
func TestDocEndExtendsSelection(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\n")
	selectLines(&m, 0, 0)

	m, _ = m.runAction("doc_end")

	if !m.ModeName().IsVisual() {
		t.Fatalf("mode = %v, want a visual mode", m.ModeName())
	}
	if m.anchor.Line != 0 || m.cursor.Line != m.buf.LineCount()-1 {
		t.Fatalf("selection = %d..%d, want 0..%d", m.anchor.Line, m.cursor.Line, m.buf.LineCount()-1)
	}
}

func TestSelectLineStartAndEnd(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m.cursor = buffer.Position{Line: 0, Col: 6}

	m, _ = m.runAction("select_line_end")
	if !m.ModeName().IsVisual() {
		t.Fatalf("mode = %v, want a visual mode", m.ModeName())
	}
	if m.anchor.Col != 6 || m.cursor.Col != len([]rune(line(m, 0)))-1 {
		t.Fatalf("selection cols = %d..%d, want 6..line end", m.anchor.Col, m.cursor.Col)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.cursor = buffer.Position{Line: 0, Col: 6}
	m, _ = m.runAction("select_line_start")
	if m.anchor.Col != 6 || m.cursor.Col != 0 {
		t.Fatalf("selection cols = %d..%d, want 6..0", m.anchor.Col, m.cursor.Col)
	}
}

// TestSelectLineEndThenCopy: the selection the command leaves is a real one —
// yanking it copies the selected run.
func TestSelectLineEndThenCopy(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m.cursor = buffer.Position{Line: 0, Col: 6}

	m, _ = m.runAction("select_line_end")
	m, _ = m.Update(key('y'))

	if got := m.regs.Get(0).Text; !strings.Contains(got, "beta") {
		t.Fatalf("yanked = %q, want the selected tail", got)
	}
}
