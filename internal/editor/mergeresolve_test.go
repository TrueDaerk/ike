package editor

// mergeresolve_test.go covers the conflict-resolution ergonomics of #2258:
// the keep-manual-edit resolution, the go / gt / gb / gm and ]n / [n
// normal-mode chords, and the caret's place in the remaining-conflict count.
// The block detection, the accepts and the wrap-around jump itself live in
// mergeconflict_test.go (#1149).

import (
	"strings"
	"testing"
)

// TestKeepManualKeepsHandMergedBody: the block's body survives verbatim —
// including a hand-typed line that the accepts would drop with the side they
// discard.
func TestKeepManualKeepsHandMergedBody(t *testing.T) {
	text := "top\n" +
		"<<<<<<< HEAD\n" +
		"merged by hand\n" +
		"=======\n" +
		">>>>>>> feature\n" +
		"bottom\n"
	m := acceptOn(t, text, 2, "merge_keep_manual")
	if got := m.Text(); got != "top\nmerged by hand\nbottom" {
		t.Fatalf("text = %q", got)
	}
	if m.ConflictCount() != 0 {
		t.Fatalf("the block must be resolved, %d left", m.ConflictCount())
	}
}

// TestKeepManualKeepsDiff3Base: unlike the accepts, keep-manual keeps the
// diff3 base section — after a hand merge those lines are the user's.
func TestKeepManualKeepsDiff3Base(t *testing.T) {
	m := acceptOn(t, diff3Text, 4, "merge_keep_manual")
	if got := m.Text(); got != "a\nours\nbase\ntheirs\nz" {
		t.Fatalf("text = %q", got)
	}
}

// TestKeepManualSingleUndo guards the one-undo-unit requirement, like the
// accepts: a single `u` restores the whole block.
func TestKeepManualSingleUndo(t *testing.T) {
	m := acceptOn(t, conflictText, 2, "merge_keep_manual")
	m = send(m, key('u'))
	if got := m.Text(); got != strings.TrimSuffix(conflictText, "\n") {
		t.Fatalf("one undo must restore the block, got %q", got)
	}
}

// TestKeepManualOutsideConflictNotices leaves the buffer untouched and
// answers with the ex-line notice.
func TestKeepManualOutsideConflictNotices(t *testing.T) {
	m, _ := loaded(t, conflictText)
	m.SetCursor(0, 0)
	m, cmd := m.Update(ActionMsg{Action: "merge_keep_manual"})
	if m.Dirty() || m.Text() != strings.TrimSuffix(conflictText, "\n") {
		t.Fatal("buffer must stay untouched outside a conflict")
	}
	if cmd == nil {
		t.Fatal("expected the no-conflict notice command")
	}
}

// TestKeepManualPreviewMatchesWhatItWrites: the intention popup's preview and
// the applied resolution read the same lines (#2252's rule, #2258's op).
func TestKeepManualPreviewMatchesWhatItWrites(t *testing.T) {
	m, _ := loaded(t, diff3Text)
	m.SetCursor(2, 0)
	_, after, _, ok := m.ConflictManualPreviewAtCaret()
	if !ok {
		t.Fatal("line 2 is inside the conflict block")
	}
	if m.Dirty() {
		t.Fatal("previewing must not dirty the buffer")
	}
	got := acceptOn(t, diff3Text, 2, "merge_keep_manual").Text()
	if want := "a\n" + after + "\nz"; got != want {
		t.Fatalf("keep-manual wrote %q, the preview promised %q", got, want)
	}
}

// TestKeepManualPreviewDeclinesOutsideABlock mirrors the accept previews.
func TestKeepManualPreviewDeclinesOutsideABlock(t *testing.T) {
	m, _ := loaded(t, conflictText)
	m.SetCursor(0, 0)
	if _, _, _, ok := m.ConflictManualPreviewAtCaret(); ok {
		t.Fatal("line 0 is outside the conflict block")
	}
}

// TestConflictChordsResolve drives the four normal-mode resolution chords.
func TestConflictChordsResolve(t *testing.T) {
	for _, tc := range []struct {
		chord rune
		want  string
	}{
		{'o', "top\nours1\nours2\nbottom"},
		{'t', "top\ntheirs1\nbottom"},
		{'b', "top\nours1\nours2\ntheirs1\nbottom"},
		{'m', "top\nours1\nours2\ntheirs1\nbottom"},
	} {
		m, _ := loaded(t, conflictText)
		m.SetCursor(2, 0)
		m = send(m, key('g'), key(tc.chord))
		if got := m.Text(); got != tc.want {
			t.Fatalf("g%c produced %q, want %q", tc.chord, got, tc.want)
		}
	}
}

// TestConflictChordsIdleOutsideBlock: off a conflict block the g-family
// resolutions only notice, so the plain letters stay harmless in ordinary
// buffers.
func TestConflictChordsIdleOutsideBlock(t *testing.T) {
	m, _ := loaded(t, "just\ntext\n")
	m = send(m, key('g'), key('o'))
	if m.Dirty() || m.Text() != "just\ntext" {
		t.Fatalf("go outside a conflict changed the buffer: %q", m.Text())
	}
}

// TestConflictChordsCancelPendingOperator: the resolutions are not motions,
// so `dgo` deletes nothing and leaves the block alone.
func TestConflictChordsCancelPendingOperator(t *testing.T) {
	m, _ := loaded(t, conflictText)
	m.SetCursor(2, 0)
	m = send(m, key('d'), key('g'), key('o'))
	if m.Dirty() || m.Text() != strings.TrimSuffix(conflictText, "\n") {
		t.Fatalf("a pending operator must cancel, buffer = %q", m.Text())
	}
}

// TestConflictBracketChords: ]n / [n cycle the blocks like the actions do.
func TestConflictBracketChords(t *testing.T) {
	m, _ := loaded(t, twoConflicts)
	m = send(m, key(']'), key('n'))
	if m.cursor.Line != 1 {
		t.Fatalf("]n lands on %d, want 1", m.cursor.Line)
	}
	m = send(m, key(']'), key('n'))
	if m.cursor.Line != 8 {
		t.Fatalf("second ]n lands on %d, want 8", m.cursor.Line)
	}
	m = send(m, key('['), key('n'))
	if m.cursor.Line != 1 {
		t.Fatalf("[n lands on %d, want 1", m.cursor.Line)
	}
}

// TestConflictMarkerLinesSeesHalfEditedBlocks: a block whose closer was
// deleted parses as no conflict at all, yet its markers still poison the file
// — which is exactly what the finish guard checks for.
func TestConflictMarkerLinesSeesHalfEditedBlocks(t *testing.T) {
	m, _ := loaded(t, conflictText)
	if got := m.ConflictMarkerLines(); got != 3 {
		t.Fatalf("marker lines = %d, want 3", got)
	}
	half := "top\n<<<<<<< HEAD\nours1\n=======\ntheirs1\nbottom\n"
	m, _ = loaded(t, half)
	if m.ConflictCount() != 0 {
		t.Fatalf("a block without a closer is not a conflict, got %d", m.ConflictCount())
	}
	if got := m.ConflictMarkerLines(); got != 2 {
		t.Fatalf("marker lines = %d, want 2", got)
	}
	m = acceptOn(t, conflictText, 2, "merge_accept_ours")
	if got := m.ConflictMarkerLines(); got != 0 {
		t.Fatalf("a resolved block leaves no markers, got %d", got)
	}
}

// TestConflictIndexAtCursor tracks the caret's place in the cycle and the
// remaining count as blocks are resolved.
func TestConflictIndexAtCursor(t *testing.T) {
	m, _ := loaded(t, twoConflicts)
	if idx, total := m.ConflictIndexAtCursor(); idx != 0 || total != 2 {
		t.Fatalf("outside a block: idx=%d total=%d, want 0/2", idx, total)
	}
	m.SetCursor(9, 0)
	if idx, total := m.ConflictIndexAtCursor(); idx != 2 || total != 2 {
		t.Fatalf("in the second block: idx=%d total=%d, want 2/2", idx, total)
	}
	m, _ = m.Update(ActionMsg{Action: "merge_accept_ours"})
	if idx, total := m.ConflictIndexAtCursor(); idx != 0 || total != 1 {
		t.Fatalf("after resolving: idx=%d total=%d, want 0/1", idx, total)
	}
	m.SetCursor(1, 0)
	if idx, total := m.ConflictIndexAtCursor(); idx != 1 || total != 1 {
		t.Fatalf("in the remaining block: idx=%d total=%d, want 1/1", idx, total)
	}
}
