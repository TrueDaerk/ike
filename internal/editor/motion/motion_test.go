package motion

import (
	"testing"

	"ike/internal/editor/buffer"
)

func pos(l, c int) buffer.Position { return buffer.Position{Line: l, Col: c} }

func TestCharwiseMotions(t *testing.T) {
	b := buffer.FromString("hello world\nsecond")
	cases := []struct {
		name string
		fn   func(*buffer.Buffer, buffer.Position, int) Result
		from buffer.Position
		cnt  int
		want buffer.Position
	}{
		{"l", Right, pos(0, 0), 2, pos(0, 2)},
		{"l-clamp", Right, pos(0, 10), 5, pos(0, 11)},
		{"h", Left, pos(0, 3), 2, pos(0, 1)},
		{"h-clamp", Left, pos(0, 1), 9, pos(0, 0)},
		{"0", LineStart, pos(0, 5), 1, pos(0, 0)},
		{"$", LineEnd, pos(0, 0), 1, pos(0, 10)},
		{"j", Down, pos(0, 2), 1, pos(1, 2)},
		{"k", Up, pos(1, 2), 1, pos(0, 2)},
	}
	for _, c := range cases {
		got := c.fn(b, c.from, c.cnt)
		if got.Pos != c.want {
			t.Errorf("%s: got %v want %v", c.name, got.Pos, c.want)
		}
	}
}

func TestFirstNonBlank(t *testing.T) {
	b := buffer.FromString("    indented")
	if got := FirstNonBlank(b, pos(0, 8), 1); got.Pos.Col != 4 {
		t.Fatalf("^ col=%d want 4", got.Pos.Col)
	}
}

func TestWordForward(t *testing.T) {
	b := buffer.FromString("foo bar baz")
	p := pos(0, 0)
	for _, want := range []int{4, 8, 10} {
		p = WordForward(b, p, 1).Pos
		if p.Col != want {
			t.Fatalf("w -> col %d want %d", p.Col, want)
		}
	}
}

func TestWordForwardPunctClass(t *testing.T) {
	b := buffer.FromString("foo.bar")
	// w stops at the '.' (punct class) then at "bar".
	p := WordForward(b, pos(0, 0), 1).Pos
	if p.Col != 3 {
		t.Fatalf("w to punct col=%d want 3", p.Col)
	}
	p = WordForward(b, p, 1).Pos
	if p.Col != 4 {
		t.Fatalf("w to bar col=%d want 4", p.Col)
	}
}

func TestWordForwardBigCrossesPunct(t *testing.T) {
	b := buffer.FromString("foo.bar baz")
	p := WordForwardBig(b, pos(0, 0), 1).Pos
	if p.Col != 8 {
		t.Fatalf("W col=%d want 8", p.Col)
	}
}

func TestWordForwardAcrossLines(t *testing.T) {
	b := buffer.FromString("foo\nbar")
	p := WordForward(b, pos(0, 1), 1).Pos
	if p != (pos(1, 0)) {
		t.Fatalf("w across line=%v want {1 0}", p)
	}
}

func TestWordForwardStopsOnEmptyLine(t *testing.T) {
	// vim: an empty line is itself a word, so w stops on it (issue #375).
	b := buffer.FromString("package main\n\nimport (")
	p := WordForward(b, pos(0, 8), 1).Pos // on "main"
	if p != pos(1, 0) {
		t.Fatalf("w onto empty line=%v want {1 0}", p)
	}
	p = WordForward(b, p, 1).Pos // leaving the empty line
	if p != pos(2, 0) {
		t.Fatalf("w off empty line=%v want {2 0}", p)
	}
}

func TestWordForwardConsecutiveEmptyLines(t *testing.T) {
	b := buffer.FromString("foo\n\n\nbar")
	p := WordForward(b, pos(0, 0), 1).Pos
	if p != pos(1, 0) {
		t.Fatalf("w first empty=%v want {1 0}", p)
	}
	p = WordForward(b, p, 1).Pos
	if p != pos(2, 0) {
		t.Fatalf("w second empty=%v want {2 0}", p)
	}
	p = WordForward(b, p, 1).Pos
	if p != pos(3, 0) {
		t.Fatalf("w to bar=%v want {3 0}", p)
	}
}

func TestWordForwardBigStopsOnEmptyLine(t *testing.T) {
	b := buffer.FromString("foo.bar\n\nbaz")
	p := WordForwardBig(b, pos(0, 0), 1).Pos
	if p != pos(1, 0) {
		t.Fatalf("W onto empty line=%v want {1 0}", p)
	}
}

func TestWordForwardCountAcrossEmptyLine(t *testing.T) {
	// 2w from "main" must land on "import", counting the empty line as one word.
	b := buffer.FromString("package main\n\nimport (")
	if p := WordForward(b, pos(0, 8), 2).Pos; p != pos(2, 0) {
		t.Fatalf("2w=%v want {2 0}", p)
	}
}

func TestWordBackwardStopsOnEmptyLine(t *testing.T) {
	b := buffer.FromString("package main\n\nimport (")
	p := WordBackward(b, pos(2, 0), 1).Pos // from "import"
	if p != pos(1, 0) {
		t.Fatalf("b onto empty line=%v want {1 0}", p)
	}
	p = WordBackward(b, p, 1).Pos // leaving the empty line
	if p != pos(0, 8) {
		t.Fatalf("b off empty line=%v want {0 8}", p)
	}
}

func TestWordBackwardConsecutiveEmptyLines(t *testing.T) {
	b := buffer.FromString("foo\n\n\nbar")
	p := WordBackward(b, pos(3, 0), 1).Pos
	if p != pos(2, 0) {
		t.Fatalf("b first empty=%v want {2 0}", p)
	}
	p = WordBackward(b, p, 1).Pos
	if p != pos(1, 0) {
		t.Fatalf("b second empty=%v want {1 0}", p)
	}
}

func TestWordEndSkipsEmptyLine(t *testing.T) {
	// vim's e does NOT stop on empty lines.
	b := buffer.FromString("foo\n\nbar")
	if p := WordEnd(b, pos(0, 2), 1).Pos; p != pos(2, 2) {
		t.Fatalf("e=%v want {2 2}", p)
	}
}

func TestWordEnd(t *testing.T) {
	b := buffer.FromString("foo bar")
	p := WordEnd(b, pos(0, 0), 1).Pos
	if p.Col != 2 {
		t.Fatalf("e col=%d want 2", p.Col)
	}
	p = WordEnd(b, p, 1).Pos
	if p.Col != 6 {
		t.Fatalf("e col=%d want 6", p.Col)
	}
}

func TestWordBackward(t *testing.T) {
	b := buffer.FromString("foo bar baz")
	p := WordBackward(b, pos(0, 10), 1).Pos
	if p.Col != 8 {
		t.Fatalf("b col=%d want 8", p.Col)
	}
	p = WordBackward(b, p, 2).Pos
	if p.Col != 0 {
		t.Fatalf("2b col=%d want 0", p.Col)
	}
}

func TestWordForwardCount(t *testing.T) {
	b := buffer.FromString("a b c d")
	if got := WordForward(b, pos(0, 0), 3).Pos.Col; got != 6 {
		t.Fatalf("3w col=%d want 6", got)
	}
}

func TestLinewiseMotions(t *testing.T) {
	b := buffer.FromString("a\nb\nc\nd")
	if got := Last(b, pos(0, 0), 0).Pos.Line; got != 3 {
		t.Fatalf("G line=%d want 3", got)
	}
	if got := First(b, pos(3, 0), 0).Pos.Line; got != 0 {
		t.Fatalf("gg line=%d want 0", got)
	}
	if got := Last(b, pos(0, 0), 2).Pos.Line; got != 1 {
		t.Fatalf("2G line=%d want 1", got)
	}
}

func TestParagraphMotions(t *testing.T) {
	b := buffer.FromString("a\nb\n\nc\nd")
	if got := ParagraphForward(b, pos(0, 0), 1).Pos.Line; got != 2 {
		t.Fatalf("} line=%d want 2", got)
	}
	if got := ParagraphBackward(b, pos(4, 0), 1).Pos.Line; got != 2 {
		t.Fatalf("{ line=%d want 2", got)
	}
}

func TestFindChar(t *testing.T) {
	b := buffer.FromString("a.b.c.d")
	r, ok := Find{FindForward, '.'}.Apply(b, pos(0, 0), 1)
	if !ok || r.Pos.Col != 1 {
		t.Fatalf("f. ok=%v col=%d want 1", ok, r.Pos.Col)
	}
	r, _ = Find{FindForward, '.'}.Apply(b, pos(0, 0), 2)
	if r.Pos.Col != 3 {
		t.Fatalf("2f. col=%d want 3", r.Pos.Col)
	}
	r, _ = Find{TillForward, '.'}.Apply(b, pos(0, 0), 1)
	if r.Pos.Col != 0 {
		t.Fatalf("t. col=%d want 0", r.Pos.Col)
	}
	r, ok = Find{FindBackward, '.'}.Apply(b, pos(0, 5), 1)
	if !ok || r.Pos.Col != 3 {
		t.Fatalf("F. col=%d want 3", r.Pos.Col)
	}
	if _, ok := (Find{FindForward, 'z'}).Apply(b, pos(0, 0), 1); ok {
		t.Fatal("missing char should report ok=false")
	}
}

func TestFindReverse(t *testing.T) {
	if got := (Find{FindForward, 'x'}).Reverse().Kind; got != FindBackward {
		t.Fatalf("reverse f = %v want F", got)
	}
}

func TestMatchPair(t *testing.T) {
	b := buffer.FromString("foo(bar(baz))")
	r, ok := MatchPair(b, pos(0, 3), 1)
	if !ok || r.Pos.Col != 12 {
		t.Fatalf("%% from open col=%d ok=%v want 12", r.Pos.Col, ok)
	}
	r, ok = MatchPair(b, pos(0, 12), 1)
	if !ok || r.Pos.Col != 3 {
		t.Fatalf("%% from close col=%d want 3", r.Pos.Col)
	}
}

func TestMatchPairAcrossLines(t *testing.T) {
	b := buffer.FromString("func() {\n  body\n}")
	r, ok := MatchPair(b, pos(0, 7), 1)
	if !ok || r.Pos != (pos(2, 0)) {
		t.Fatalf("%% multiline=%v ok=%v want {2 0}", r.Pos, ok)
	}
}

func TestWordForwardInLineClampsToLine(t *testing.T) {
	b := buffer.FromString("foo bar\nnext line")
	// Within the line it behaves like w.
	if p := WordForwardInLine(b, pos(0, 0), 1).Pos; p != pos(0, 4) {
		t.Fatalf("in-line w -> %v want %v", p, pos(0, 4))
	}
	// Past the last word it stops at the line-end slot instead of crossing.
	if p := WordForwardInLine(b, pos(0, 4), 1).Pos; p != pos(0, 7) {
		t.Fatalf("in-line w at last word -> %v want %v", p, pos(0, 7))
	}
	if p := WordForwardInLine(b, pos(0, 4), 5).Pos; p != pos(0, 7) {
		t.Fatalf("in-line w with count past end -> %v want %v", p, pos(0, 7))
	}
}

func TestWordBackwardInLineClampsToLine(t *testing.T) {
	b := buffer.FromString("first line\nfoo bar")
	if p := WordBackwardInLine(b, pos(1, 4), 1).Pos; p != pos(1, 0) {
		t.Fatalf("in-line b -> %v want %v", p, pos(1, 0))
	}
	// Before the first word it stops at column 0 instead of crossing.
	if p := WordBackwardInLine(b, pos(1, 0), 1).Pos; p != pos(1, 0) {
		t.Fatalf("in-line b at line start -> %v want %v", p, pos(1, 0))
	}
	if p := WordBackwardInLine(b, pos(1, 6), 9).Pos; p != pos(1, 0) {
		t.Fatalf("in-line b with count past start -> %v want %v", p, pos(1, 0))
	}
}

func TestWordInLineDotStops(t *testing.T) {
	b := buffer.FromString("config.editor.tabWidth x")
	// Forward: each '.' and each segment is a stop point.
	p := pos(0, 0)
	for _, want := range []int{6, 7, 13, 14, 23} {
		p = WordForwardInLine(b, p, 1).Pos
		if p != pos(0, want) {
			t.Fatalf("in-line w -> %v want col %d", p, want)
		}
	}
	// Backward retraces the same stops.
	for _, want := range []int{14, 13, 7, 6, 0} {
		p = WordBackwardInLine(b, p, 1).Pos
		if p != pos(0, want) {
			t.Fatalf("in-line b -> %v want col %d", p, want)
		}
	}
}
