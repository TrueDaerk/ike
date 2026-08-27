package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// TestFoldSpansIndexMatchesScan (#2187): the interval index lineHidden binary-
// searches must answer exactly what the old linear map scan did, for nested,
// chained and disjoint folds alike.
func TestFoldSpansIndexMatchesScan(t *testing.T) {
	m := foldModel(t)
	m.folded = map[int]int{2: 7, 3: 5, 9: 11}
	m.bumpFolds()
	for line := 0; line < m.buf.LineCount(); line++ {
		want := false
		for h, e := range m.folded {
			if line > h && line <= e {
				want = true
			}
		}
		if got := m.lineHidden(line); got != want {
			t.Errorf("lineHidden(%d) = %v, want %v", line, got, want)
		}
	}
}

// TestFoldSpansMemoized (#2187): an unchanged collapsed set serves the built
// index; any mutation of folded rebuilds it.
func TestFoldSpansMemoized(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jzc")...) // close 2-7
	heads, _ := m.foldSpans()
	again, _ := m.foldSpans()
	if len(heads) != 1 || &again[0] != &heads[0] {
		t.Fatalf("unchanged fold set must serve the memoized index (%v/%v)", heads, again)
	}
	m = send(m, keys("zR")...) // open every fold
	if m.lineHidden(4) {
		t.Fatal("zR must reveal every line — the index outlived its mutation")
	}
	m = send(m, keys("zM")...) // close every fold again
	after, _ := m.foldSpans()
	if len(after) == 0 {
		t.Fatal("zM must rebuild the index over the collapsed set")
	}
	if !m.lineHidden(4) {
		t.Error("line 4 sits inside fold 2-7 and must be hidden after zM")
	}
}

// TestFoldSpansSurviveEdits (#2187): dissolveFoldsAtEdit replaces the map
// wholesale, so the index must follow the shifted folds and not the old ones.
func TestFoldSpansSurviveEdits(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("9jzc")...) // close 9-11, cursor on header 9
	if !m.lineHidden(10) {
		t.Fatal("setup: line 10 should be hidden inside fold 9-11")
	}
	m = send(m, keys("ggO")...) // insert a line above everything
	m = send(m, key('x'))
	m = send(m, esc())
	if m.lineHidden(10) {
		t.Error("the fold shifted down by one line; 10 is its header now, not hidden")
	}
	if !m.lineHidden(11) {
		t.Error("line 11 sits inside the shifted fold 10-12 and must stay hidden")
	}
}

// TestFoldIndexIsPerView (#2187/#144): a second view of the same document
// folds independently, so it must not inherit the source view's index.
func TestFoldIndexIsPerView(t *testing.T) {
	src := foldModel(t)
	src = send(src, keys("2jzc")...)
	if !src.lineHidden(4) {
		t.Fatal("setup: line 4 should be hidden in the source view")
	}
	dst := New()
	dst.buf = buffer.FromString(strings.Join([]string{"x"}, "\n"))
	dst.SetSize(40, 10)
	dst.ShareDocumentWith(&src)
	if dst.lineHidden(4) {
		t.Error("a fresh view starts with no collapsed folds and must hide nothing")
	}
}
