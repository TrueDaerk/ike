package editor

// selrange_test.go covers extend/shrink selection (#1912, selrange.go): the
// ladder application, the growth/shrink walk, staleness guards, and the
// word/line/buffer last-resort ladder. Ladders are injected as
// ilsp.SelectionRangesMsg directly — no server and no cgo involved.

import (
	"testing"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
)

// rng is a shorthand charwise range.
func rng(sl, sc, el, ec int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: sl, Col: sc}, End: buffer.Position{Line: el, Col: ec}}
}

// extendWith starts an extend at the current cursor and feeds the given
// ladder back as the (matching) reply.
func extendWith(t *testing.T, m Model, ranges ...buffer.Range) Model {
	t.Helper()
	m, _ = m.runAction("selection_extend")
	s := m.selRange
	if s == nil || !s.pending {
		t.Fatal("extend must leave a pending request")
	}
	m, _ = m.Update(ilsp.SelectionRangesMsg{Path: m.path, Line: s.req.Line, Col: s.req.Col, Ranges: ranges})
	return m
}

// selText fails the test unless the active selection is exactly want.
func selText(t *testing.T, m Model, want string) {
	t.Helper()
	got, ok := m.SelectionText()
	if !ok {
		t.Fatalf("no active selection, want %q", want)
	}
	if got != want {
		t.Fatalf("SelectionText() = %q, want %q", got, want)
	}
}

// selModel is the shared fixture: cursor on the 'b' of "beta".
func selModel(t *testing.T) Model {
	t.Helper()
	m, _ := loaded(t, "alpha(beta, gamma)\nsecond line\n")
	m.SetCursor(0, 6)
	return m
}

// selLadder is the fixture's three-step ladder: beta → the argument list →
// the whole first line.
func selLadder() []buffer.Range {
	return []buffer.Range{rng(0, 6, 0, 10), rng(0, 6, 0, 17), rng(0, 0, 0, 18)}
}

func TestExtendAppliesInnermostRange(t *testing.T) {
	m := selModel(t)
	m = extendWith(t, m, selLadder()...)
	if m.mode != Visual {
		t.Fatalf("mode = %v, want Visual", m.mode)
	}
	selText(t, m, "beta")
}

func TestExtendGrowsAndShrinkReverses(t *testing.T) {
	m := selModel(t)
	m = extendWith(t, m, selLadder()...)
	m, _ = m.runAction("selection_extend")
	selText(t, m, "beta, gamma")
	m, _ = m.runAction("selection_extend")
	selText(t, m, "alpha(beta, gamma)")
	// The top step is the ladder's end: another extend keeps the selection.
	m, _ = m.runAction("selection_extend")
	selText(t, m, "alpha(beta, gamma)")
	m, _ = m.runAction("selection_shrink")
	selText(t, m, "beta, gamma")
	m, _ = m.runAction("selection_shrink")
	selText(t, m, "beta")
}

func TestShrinkBelowFirstStepRestoresCursor(t *testing.T) {
	m := selModel(t)
	m = extendWith(t, m, selLadder()...)
	m, _ = m.runAction("selection_shrink")
	if m.mode != Normal {
		t.Fatalf("bottom shrink must return to Normal, mode = %v", m.mode)
	}
	if m.cursor != (buffer.Position{Line: 0, Col: 6}) {
		t.Fatalf("bottom shrink must restore the origin cursor, got %v", m.cursor)
	}
	if m.selRange != nil {
		t.Fatal("bottom shrink must drop the ladder state")
	}
}

func TestShrinkWithoutLadderIsNoop(t *testing.T) {
	m := selModel(t)
	m, _ = m.runAction("selection_shrink")
	if m.mode != Normal || m.cursor != (buffer.Position{Line: 0, Col: 6}) {
		t.Fatalf("shrink with no ladder must not move, mode=%v cursor=%v", m.mode, m.cursor)
	}
}

func TestStaleSelectionRangesIgnored(t *testing.T) {
	m := selModel(t)
	m, _ = m.runAction("selection_extend")
	// Wrong path, then wrong anchor: neither may apply a selection.
	m, _ = m.Update(ilsp.SelectionRangesMsg{Path: "/other.go", Line: 0, Col: 6, Ranges: selLadder()})
	if m.mode.IsVisual() {
		t.Fatal("another document's ladder must be ignored")
	}
	m, _ = m.Update(ilsp.SelectionRangesMsg{Path: m.path, Line: 1, Col: 0, Ranges: selLadder()})
	if m.mode.IsVisual() {
		t.Fatal("a ladder for another anchor position must be ignored")
	}
	// A reply without a pending request (the request was consumed) too.
	m, _ = m.Update(ilsp.SelectionRangesMsg{Path: m.path, Line: 0, Col: 6, Ranges: selLadder()})
	selText(t, m, "beta")
	m, _ = m.Update(ilsp.SelectionRangesMsg{Path: m.path, Line: 0, Col: 6, Ranges: []buffer.Range{rng(0, 0, 0, 18)}})
	selText(t, m, "beta")
}

func TestExtendFromManualSelectionGrowsBeyondIt(t *testing.T) {
	// A manual visual selection over "beta, " (cols 6-11): the first applied
	// step is the smallest ladder range strictly containing it — "beta" is
	// skipped even though it contains the cursor's request position.
	m := selModel(t)
	m = send(m, key('v'))
	m.cursor = buffer.Position{Line: 0, Col: 11}
	m, _ = m.runAction("selection_extend")
	s := m.selRange
	m, _ = m.Update(ilsp.SelectionRangesMsg{Path: m.path, Line: s.req.Line, Col: s.req.Col, Ranges: []buffer.Range{
		rng(0, 6, 0, 10), rng(0, 6, 0, 17), rng(0, 0, 0, 18),
	}})
	selText(t, m, "beta, gamma")
}

func TestExtendRestartsAfterEdit(t *testing.T) {
	m := selModel(t)
	m = extendWith(t, m, selLadder()...)
	m = send(m, key('y')) // yank leaves visual mode without an edit
	m = send(m, key('x')) // edit bumps the doc version
	m.SetCursor(0, 6)
	m, cmd := m.runAction("selection_extend")
	if s := m.selRange; s == nil || !s.pending {
		t.Fatal("extend after an edit must start a fresh request")
	}
	if cmd == nil {
		t.Fatal("a fresh extend must return the request/fallback command")
	}
}

func TestEmptyLadderFallsBackToWordLineBuffer(t *testing.T) {
	// No provider and no grammar for .txt: the fallback command answers with
	// an empty ladder, which lands on the word → line → buffer last resort.
	m := selModel(t)
	m, cmd := m.runAction("selection_extend")
	if cmd == nil {
		t.Fatal("extend without a provider must return the fallback command")
	}
	msg, ok := cmd().(ilsp.SelectionRangesMsg)
	if !ok || len(msg.Ranges) != 0 {
		t.Fatalf("Tree-sitter fallback on a grammarless file must yield an empty ladder, got %+v", msg)
	}
	m, _ = m.Update(msg)
	selText(t, m, "beta")
	m, _ = m.runAction("selection_extend")
	selText(t, m, "alpha(beta, gamma)")
	m, _ = m.runAction("selection_extend")
	selText(t, m, "alpha(beta, gamma)\nsecond line")
}

func TestLadderSanitizeNormalizesLineEndZero(t *testing.T) {
	// A range ending at column 0 of the next line covers the same text as one
	// ending at the previous line's end; the applied selection must match the
	// range's text exactly — without swallowing the newline cell.
	m, _ := loaded(t, "ab\ncd\n")
	m.SetCursor(0, 0)
	m = extendWith(t, m, rng(0, 0, 1, 0))
	selText(t, m, "ab")
}

func TestExtendFromInsertModeCommitsFirst(t *testing.T) {
	m := selModel(t)
	// Insert at the 'e': commitInsert steps the cursor back onto the 'b', so
	// the request anchors inside "beta".
	m.SetCursor(0, 7)
	m = send(m, key('i'))
	m = extendWith(t, m, selLadder()...)
	if m.mode != Visual {
		t.Fatalf("extend from insert mode must land in Visual, mode = %v", m.mode)
	}
	selText(t, m, "beta")
}
