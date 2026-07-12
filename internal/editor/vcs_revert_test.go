package editor

import (
	"strings"
	"testing"
)

// vcs.revertHunk (#555): the contiguous change under the caret reverts to its
// HEAD content as one undo step; the caret outside any change is a no-op.

// revertAt loads buf, puts the cursor on line, and runs the hunk revert
// against head. It returns the editor and whether a hunk was reverted.
func revertAt(t *testing.T, head, buf string, line int) (Model, bool) {
	t.Helper()
	m, _ := loaded(t, buf)
	m.SetCursor(line, 0)
	ok := m.RevertHunkUnderCursor(head)
	return m, ok
}

// wantText compares the buffer against want, ignoring the trailing newline
// the buffer models as a line terminator rather than content.
func wantText(t *testing.T, m Model, want string) {
	t.Helper()
	if got := m.Text(); got != strings.TrimSuffix(want, "\n") {
		t.Fatalf("buffer = %q, want %q", got, want)
	}
}

func TestRevertHunkChangedLines(t *testing.T) {
	head := "a\nb\nc\n"
	m, ok := revertAt(t, head, "a\nX\nc\n", 1)
	if !ok {
		t.Fatal("hunk under caret not found")
	}
	wantText(t, m, head)
	if !m.Dirty() {
		t.Fatal("revert must dirty the buffer")
	}
}

func TestRevertHunkAddedLines(t *testing.T) {
	head := "a\nb\n"
	for _, line := range []int{1, 2} {
		m, ok := revertAt(t, head, "a\nx\ny\nb\n", line)
		if !ok {
			t.Fatalf("line %d: hunk not found", line)
		}
		wantText(t, m, head)
	}
}

func TestRevertHunkAddedLinesAtEOF(t *testing.T) {
	m, ok := revertAt(t, "a\n", "a\nx", 1)
	if !ok {
		t.Fatal("hunk not found")
	}
	wantText(t, m, "a")
}

func TestRevertHunkDeletedLines(t *testing.T) {
	// b removed: the deletion mark sits on the line now in its place ("c").
	m, ok := revertAt(t, "a\nb\nc\n", "a\nc\n", 1)
	if !ok {
		t.Fatal("hunk not found")
	}
	wantText(t, m, "a\nb\nc\n")
}

func TestRevertHunkDeletedHeadOfFile(t *testing.T) {
	m, ok := revertAt(t, "a\nb\nc\n", "c\n", 0)
	if !ok {
		t.Fatal("hunk not found")
	}
	wantText(t, m, "a\nb\nc\n")
}

func TestRevertHunkDeletionAtEOFFoldsOntoLastLine(t *testing.T) {
	// b and c removed at EOF: the mark folds onto the last real line ("a").
	m, ok := revertAt(t, "a\nb\nc\n", "a\n", 0)
	if !ok {
		t.Fatal("hunk not found")
	}
	wantText(t, m, "a\nb\nc\n")
}

func TestRevertHunkMixedTrailingDeletionMark(t *testing.T) {
	// b→X plus c removed: the deletion mark lands on the unchanged line
	// after the hunk ("d"), which must still resolve to this hunk.
	head := "a\nb\nc\nd\n"
	for _, line := range []int{1, 2} {
		m, ok := revertAt(t, head, "a\nX\nd\n", line)
		if !ok {
			t.Fatalf("line %d: hunk not found", line)
		}
		wantText(t, m, head)
	}
}

func TestRevertHunkPicksHunkUnderCaret(t *testing.T) {
	m, ok := revertAt(t, "a\nb\nc\nd\ne\n", "a\nX\nc\nd\nY\n", 4)
	if !ok {
		t.Fatal("hunk not found")
	}
	wantText(t, m, "a\nX\nc\nd\ne\n") // other hunk untouched
}

func TestRevertHunkCaretOutsideChange(t *testing.T) {
	m, ok := revertAt(t, "a\nb\nc\n", "a\nX\nc\n", 0)
	if ok {
		t.Fatal("caret on an unchanged line must not revert")
	}
	wantText(t, m, "a\nX\nc\n")
	if m.Dirty() {
		t.Fatal("no-op must not dirty the buffer")
	}
}

func TestRevertHunkCleanBuffer(t *testing.T) {
	if _, ok := revertAt(t, "a\nb\n", "a\nb\n", 0); ok {
		t.Fatal("clean buffer must not revert")
	}
}

func TestRestoreContentUndoRoundTrip(t *testing.T) {
	m, _ := loaded(t, "head\n")
	if !m.RestoreContent("pre\nrevert\r\ncontent\n") {
		t.Fatal("restore reported no-op")
	}
	wantText(t, m, "pre\nrevert\ncontent") // CRLF folded, terminator dropped
	if !m.Dirty() {
		t.Fatal("restore must dirty the buffer")
	}
	m.undo(1)
	wantText(t, m, "head")
	m.redo(1)
	wantText(t, m, "pre\nrevert\ncontent")
}

func TestRestoreContentSameContentIsNoOp(t *testing.T) {
	m, _ := loaded(t, "a\nb\n")
	if m.RestoreContent("a\nb\n") {
		t.Fatal("identical content must report false")
	}
	if m.Dirty() {
		t.Fatal("no-op must not dirty the buffer")
	}
}

func TestRevertHunkUndoRoundTrip(t *testing.T) {
	buf := "a\nX\ny\nc\n"
	m, ok := revertAt(t, "a\nb\nc\n", buf, 1)
	if !ok {
		t.Fatal("hunk not found")
	}
	wantText(t, m, "a\nb\nc\n")
	m.undo(1)
	wantText(t, m, buf)
	m.redo(1)
	wantText(t, m, "a\nb\nc\n")
}
