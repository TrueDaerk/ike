package editor

import (
	"strings"
	"testing"
)

// lineset_test.go covers the #2417 line-set commands: the block they pick, the
// order each flavour produces, the single undo step and the selection left
// behind.

func TestSortLinesWholeBufferWithoutSelection(t *testing.T) {
	m, _ := loaded(t, "pear\napple\ncherry\n")

	m, _ = m.runAction("sort_lines")

	if got, want := bufLines(m), "apple\ncherry\npear"; got != want {
		t.Fatalf("sort_lines = %q, want %q", got, want)
	}
}

func TestSortLinesOnlyTouchesSelectedLines(t *testing.T) {
	m, _ := loaded(t, "head\nc\na\nb\ntail\n")
	selectLines(&m, 1, 3)

	m, _ = m.runAction("sort_lines")

	if got, want := bufLines(m), "head\na\nb\nc\ntail"; got != want {
		t.Fatalf("sort_lines = %q, want %q", got, want)
	}
}

// The block stays selected so a second command in the family chains onto the
// same lines.
func TestSortLinesLeavesResultSelected(t *testing.T) {
	m, _ := loaded(t, "head\nc\na\nb\ntail\n")
	selectLines(&m, 1, 3)

	m, _ = m.runAction("sort_lines")

	if m.ModeName() != VisualLine {
		t.Fatalf("mode = %v, want VisualLine", m.ModeName())
	}
	if m.anchor.Line != 1 || m.cursor.Line != 3 {
		t.Fatalf("selection = %d..%d, want 1..3", m.anchor.Line, m.cursor.Line)
	}
}

// Unique shortens the block, so the restored selection must shrink with it.
func TestUniqueLinesKeepsFirstOccurrenceAndShrinksSelection(t *testing.T) {
	m, _ := loaded(t, "b\na\nb\nc\na\n")

	m, _ = m.runAction("unique_lines")

	if got, want := bufLines(m), "b\na\nc"; got != want {
		t.Fatalf("unique_lines = %q, want %q", got, want)
	}
	if m.anchor.Line != 0 || m.cursor.Line != 2 {
		t.Fatalf("selection = %d..%d, want 0..2", m.anchor.Line, m.cursor.Line)
	}
}

func TestReverseLinesFlipsTheBlock(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\n")

	m, _ = m.runAction("reverse_lines")

	if got, want := bufLines(m), "three\ntwo\none"; got != want {
		t.Fatalf("reverse_lines = %q, want %q", got, want)
	}
}

// Shuffle is the one non-deterministic flavour: the permutation is pinned so
// the test asserts the rewrite, not the randomness.
func TestShuffleLinesPermutesTheBlock(t *testing.T) {
	orig := shuffleFunc
	t.Cleanup(func() { shuffleFunc = orig })
	shuffleFunc = func(n int, swap func(i, j int)) {
		for i := 0; i+1 < n; i += 2 {
			swap(i, i+1)
		}
	}

	m, _ := loaded(t, "a\nb\nc\nd\n")
	m, _ = m.runAction("shuffle_lines")

	if got, want := bufLines(m), "b\na\nd\nc"; got != want {
		t.Fatalf("shuffle_lines = %q, want %q", got, want)
	}
}

// One invocation is one undo step, however many lines it moved.
func TestSortLinesIsASingleUndoStep(t *testing.T) {
	m, _ := loaded(t, "d\nc\nb\na\n")

	m, _ = m.runAction("sort_lines")
	if got := bufLines(m); got != "a\nb\nc\nd" {
		t.Fatalf("sort_lines = %q", got)
	}

	m, _ = m.runAction("undo")
	if got, want := bufLines(m), "d\nc\nb\na"; got != want {
		t.Fatalf("after undo = %q, want %q", got, want)
	}
}

// An already-ordered block records no undo entry — the next "u" must reach the
// edit before it, not a no-op sort.
func TestSortLinesNoOpRecordsNoUndoStep(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\n")

	m, _ = m.runAction("sort_lines")
	if m.dirty {
		t.Fatalf("no-op sort marked the buffer dirty")
	}
	if m.ModeName() != VisualLine {
		t.Fatalf("mode = %v, want VisualLine even for a no-op", m.ModeName())
	}
}

// Sorting the whole buffer must not add or drop the file's final newline.
func TestSortLinesKeepsTrailingNewline(t *testing.T) {
	for _, text := range []string{"b\na\n", "b\na"} {
		m, _ := loaded(t, text)
		before := m.buf.LineCount()

		m, _ = m.runAction("sort_lines")

		if got := m.buf.LineCount(); got != before {
			t.Fatalf("%q: line count %d, want %d", text, got, before)
		}
		if got := bufLines(m); got != "a\nb" {
			t.Fatalf("%q: sorted = %q", text, got)
		}
	}
}

func TestSortLinesFlavours(t *testing.T) {
	tests := []struct {
		action string
		in     string
		want   string
	}{
		{"sort_lines", "b\nA\na\nB\n", "A\nB\na\nb"},
		{"sort_lines_ignore_case", "b\nA\na\nB\n", "A\na\nB\nb"},
		{"sort_lines_natural", "file10\nfile2\nfile1\n", "file1\nfile2\nfile10"},
		{"sort_lines_descending", "b\nc\na\n", "c\nb\na"},
		{"sort_lines_by_length", "ccc\na\nbb\n", "a\nbb\nccc"},
	}
	for _, tt := range tests {
		m, _ := loaded(t, tt.in)
		m, _ = m.runAction(tt.action)
		if got := bufLines(m); got != tt.want {
			t.Errorf("%s(%q) = %q, want %q", tt.action, tt.in, got, tt.want)
		}
	}
}

// Natural order is the property the flavour exists for: "a2" before "a10".
func TestNaturalLess(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"a2", "a10", true},
		{"a10", "a2", false},
		{"file2.txt", "file10.txt", true},
		{"a", "a1", true},
		{"a1", "a", false},
		{"a1", "a01", true},  // equal value, fewer leading zeros first
		{"a01", "a1", false}, //
		{"abc", "abd", true},
		{"", "a", true},
		{"a", "a", false},
	}
	for _, tt := range tests {
		if got := naturalLess(tt.a, tt.b); got != tt.want {
			t.Errorf("naturalLess(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// Ties under the case fold fall back to the raw line, so the ignore-case order
// depends on the text alone and not on where the lines started out.
func TestSortLinesIgnoreCaseBreaksTiesOnTheRawLine(t *testing.T) {
	got := strings.Join(applyLineSet([]string{"b", "B", "a"}, opSortIgnoreCase), " ")
	if want := "a B b"; got != want {
		t.Fatalf("ignore-case sort = %q, want %q", got, want)
	}
}

// Same-length lines break their tie on the line text, so by-length order is a
// function of the block's content alone.
func TestSortLinesByLengthBreaksTiesOnTheLine(t *testing.T) {
	got := strings.Join(applyLineSet([]string{"bb", "aa", "c"}, opSortByLength), " ")
	if want := "c aa bb"; got != want {
		t.Fatalf("by-length sort = %q, want %q", got, want)
	}
}

// Multi-caret takes part only through the primary selection's line range.
func TestSortLinesCollapsesSecondaryCarets(t *testing.T) {
	m, _ := loaded(t, "b\na\nb\n")
	m, _ = m.runAction("caret_add_all")

	m, _ = m.runAction("sort_lines")

	if len(m.carets) != 0 {
		t.Fatalf("carets = %d, want 0 after a line-set command", len(m.carets))
	}
	if got, want := bufLines(m), "a\nb\nb"; got != want {
		t.Fatalf("sort_lines = %q, want %q", got, want)
	}
}
