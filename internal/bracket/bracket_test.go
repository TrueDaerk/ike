package bracket

import (
	"fmt"
	"strings"
	"testing"
)

// marks renders a scan as "line:col:depth[!]" tokens, so a test reads as the
// sequence it expects instead of a struct dump.
func marks(t *testing.T, lines []string, syn Syntax) []string {
	t.Helper()
	var out []string
	for _, m := range Scan(lines, syn) {
		s := fmt.Sprintf("%d:%d:%d", m.Line, m.Col, m.Depth)
		if m.Unmatched {
			s += "!"
		}
		out = append(out, s)
	}
	return out
}

func want(t *testing.T, got, exp []string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(exp, " ") {
		t.Fatalf("marks = %v want %v", got, exp)
	}
}

// TestPairSharesDepth (#1628): the two halves of a pair carry the same depth,
// and nesting cycles upward one level per open bracket.
func TestPairSharesDepth(t *testing.T) {
	got := marks(t, []string{"a(b[c{d}e]f)g"}, Syntax{})
	want(t, got, []string{"0:1:0", "0:3:1", "0:5:2", "0:7:2", "0:9:1", "0:11:0"})
}

// TestSiblingsShareDepth (#1628): sequential pairs at the same level all get
// depth 0 — depth is nesting, not occurrence order.
func TestSiblingsShareDepth(t *testing.T) {
	got := marks(t, []string{"()[]{}"}, Syntax{})
	want(t, got, []string{"0:0:0", "0:1:0", "0:2:0", "0:3:0", "0:4:0", "0:5:0"})
}

// TestDepthSpansLines (#1628): the stack is buffer-wide, so a brace opened on
// one line still deepens the lines below it.
func TestDepthSpansLines(t *testing.T) {
	got := marks(t, []string{"f() {", "  g([1])", "}"}, Syntax{})
	want(t, got, []string{"0:1:0", "0:2:0", "0:4:0", "1:3:1", "1:4:2", "1:6:2", "1:7:1", "2:0:0"})
}

// TestUnmatchedCloser (#1628): a closer with no opener waiting is an error and
// does not disturb the depth of the brackets around it.
func TestUnmatchedCloser(t *testing.T) {
	got := marks(t, []string{"a) (b)"}, Syntax{})
	want(t, got, []string{"0:1:0!", "0:3:0", "0:5:0"})
}

// TestUnmatchedOpener (#1628): an opener never closed by the end of the buffer
// is an error.
func TestUnmatchedOpener(t *testing.T) {
	got := marks(t, []string{"f(a", "b"}, Syntax{})
	want(t, got, []string{"0:1:0!"})
}

// TestCrossedPairMarksInnerOpener (#1628): in `{ ( }` the `}` still matches
// its `{` — only the `(` opened inside it is wrong. Without inward-out
// matching a single typo would cascade over the whole rest of the file.
func TestCrossedPairMarksInnerOpener(t *testing.T) {
	got := marks(t, []string{"{ ( }"}, Syntax{})
	want(t, got, []string{"0:0:0", "0:2:1!", "0:4:0"})
}

// TestStrayCloserKeepsOpenersWaiting (#1628): `[ ) ]` reports only the `)`.
func TestStrayCloserKeepsOpenersWaiting(t *testing.T) {
	got := marks(t, []string{"[ ) ]"}, Syntax{})
	want(t, got, []string{"0:0:0", "0:2:0!", "0:4:0"})
}

// TestBracketsInStrings (#1628): brackets inside a quoted run neither pair nor
// appear at all — `"("` closes nothing and is no error either.
func TestBracketsInStrings(t *testing.T) {
	got := marks(t, []string{`f("(", '}', ` + "`[`" + `)`}, Syntax{})
	want(t, got, []string{"0:1:0", "0:15:0"})
}

// TestEscapedQuoteInString (#1628): a `\"` does not end the string, so the
// bracket behind it stays skipped.
func TestEscapedQuoteInString(t *testing.T) {
	got := marks(t, []string{`x = "a\"(" + y`}, Syntax{})
	if len(got) != 0 {
		t.Fatalf("marks = %v want none", got)
	}
}

// TestUnterminatedQuoteIsOrdinary (#1628): an apostrophe with no partner on
// the line (prose, a Rust lifetime) must not swallow the brackets after it.
func TestUnterminatedQuoteIsOrdinary(t *testing.T) {
	got := marks(t, []string{"// don't (yet)"}, Syntax{Quotes: DefaultQuotes})
	want(t, got, []string{"0:9:0", "0:13:0"})
}

// TestLineComment (#1628): brackets behind the line-comment marker are skipped
// — an unbalanced `(` in a comment is not an error.
func TestLineComment(t *testing.T) {
	got := marks(t, []string{"f() // note (", "g()"}, Syntax{LineComment: "//"})
	want(t, got, []string{"0:1:0", "0:2:0", "1:1:0", "1:2:0"})
}

// TestBlockCommentSpansLines (#1628): a `/* … */` run keeps its state across
// lines, and code after the closer is scanned again.
func TestBlockCommentSpansLines(t *testing.T) {
	syn := Syntax{LineComment: "//", BlockComment: [2]string{"/*", "*/"}}
	got := marks(t, []string{"/* ( [", "still { comment", "*/ f()"}, syn)
	want(t, got, []string{"2:4:0", "2:5:0"})
}

// TestInlineBlockComment (#1628): a block comment closing on the same line
// skips only its own span.
func TestInlineBlockComment(t *testing.T) {
	syn := Syntax{BlockComment: [2]string{"/*", "*/"}}
	got := marks(t, []string{"f(/* ) */ x)"}, syn)
	want(t, got, []string{"0:1:0", "0:11:0"})
}

// TestHashComment (#1628): the heuristics are language-driven, so a YAML/shell
// buffer skips `#` comments and knows nothing about `//`.
func TestHashComment(t *testing.T) {
	got := marks(t, []string{"key: [a] # ( ]"}, Syntax{LineComment: "#"})
	want(t, got, []string{"0:5:0", "0:7:0"})
}

// TestSkipMaskWins (#1628): a Skip mask replaces the heuristics wholesale — a
// grammar that says "this is a string" is right even where the quote rules
// would disagree, and the heuristics must not fire underneath it.
func TestSkipMaskWins(t *testing.T) {
	lines := []string{`"""`, "text ( with ' quote", `"""`, "f()"}
	syn := Syntax{
		LineComment: "#",
		Skip:        func(line, col int) bool { return line <= 2 },
	}
	got := marks(t, lines, syn)
	want(t, got, []string{"3:1:0", "3:2:0"})
}

// TestWideRunesUseRuneColumns (#1628): columns are rune columns, not bytes, so
// they line up with the editor's own cells.
func TestWideRunesUseRuneColumns(t *testing.T) {
	got := marks(t, []string{"äöü(x)"}, Syntax{})
	want(t, got, []string{"0:3:0", "0:5:0"})
}

// TestEmptyBuffer (#1628): no lines, no marks, no panic.
func TestEmptyBuffer(t *testing.T) {
	if got := Scan(nil, Syntax{}); got != nil {
		t.Fatalf("Scan(nil) = %v want nil", got)
	}
	if got := Scan([]string{"", "   "}, Syntax{}); got != nil {
		t.Fatalf("Scan(blank) = %v want nil", got)
	}
}

// TestOpenFlag (#1628): Open distinguishes the halves of a pair, which the
// renderer needs to decide what a click or a jump lands on.
func TestOpenFlag(t *testing.T) {
	got := Scan([]string{"(x)"}, Syntax{})
	if len(got) != 2 || !got[0].Open || got[1].Open {
		t.Fatalf("Open flags = %+v", got)
	}
}
