package textobject

import (
	"testing"

	"ike/internal/editor/buffer"
)

func pos(l, c int) buffer.Position { return buffer.Position{Line: l, Col: c} }
func rg(l, s, e int) buffer.Range {
	return buffer.Range{Start: pos(l, s), End: pos(l, e)}
}

func TestWordInner(t *testing.T) {
	b := buffer.FromString("foo bar baz")
	got := Word(b, pos(0, 5), false, false) // cursor on 'a' of bar
	if !got.OK || got.Range != rg(0, 4, 7) {
		t.Fatalf("iw=%v want {4,7}", got.Range)
	}
}

func TestWordAroundTrailingSpace(t *testing.T) {
	b := buffer.FromString("foo bar baz")
	got := Word(b, pos(0, 0), true, false)
	if got.Range != rg(0, 0, 4) {
		t.Fatalf("aw=%v want {0,4}", got.Range)
	}
}

func TestWordAroundLeadingWhenNoTrailing(t *testing.T) {
	b := buffer.FromString("foo bar")
	got := Word(b, pos(0, 5), true, false) // last word, no trailing space
	if got.Range != rg(0, 3, 7) {
		t.Fatalf("aw last=%v want {3,7}", got.Range)
	}
}

func TestPairInner(t *testing.T) {
	b := buffer.FromString("foo(bar)baz")
	got := Pair(b, pos(0, 5), '(', ')', false)
	if !got.OK || got.Range != rg(0, 4, 7) {
		t.Fatalf("i(=%v want {4,7}", got.Range)
	}
}

func TestPairAround(t *testing.T) {
	b := buffer.FromString("foo(bar)baz")
	got := Pair(b, pos(0, 5), '(', ')', true)
	if got.Range != rg(0, 3, 8) {
		t.Fatalf("a(=%v want {3,8}", got.Range)
	}
}

func TestPairNested(t *testing.T) {
	b := buffer.FromString("(a(b)c)")
	got := Pair(b, pos(0, 3), '(', ')', false) // cursor on 'b'
	if got.Range != rg(0, 3, 4) {
		t.Fatalf("nested i(=%v want {3,4}", got.Range)
	}
}

func TestPairMultiline(t *testing.T) {
	b := buffer.FromString("{\n  x\n}")
	got := Pair(b, pos(1, 2), '{', '}', false)
	if !got.OK || got.Range.Start != (pos(0, 1)) || got.Range.End != (pos(2, 0)) {
		t.Fatalf("multiline i{=%v", got.Range)
	}
}

func TestPairCursorOnBracket(t *testing.T) {
	b := buffer.FromString("(abc)")
	got := Pair(b, pos(0, 0), '(', ')', false)
	if got.Range != rg(0, 1, 4) {
		t.Fatalf("i( on open=%v want {1,4}", got.Range)
	}
}

func TestQuoteInnerAndAround(t *testing.T) {
	b := buffer.FromString(`say "hi" now`)
	in := Quote(b, pos(0, 6), '"', false)
	if in.Range != rg(0, 5, 7) {
		t.Fatalf("i\"=%v want {5,7}", in.Range)
	}
	ar := Quote(b, pos(0, 6), '"', true)
	if ar.Range != rg(0, 4, 8) {
		t.Fatalf("a\"=%v want {4,8}", ar.Range)
	}
}

func TestCloseFor(t *testing.T) {
	if o, c, ok := CloseFor(')'); !ok || o != '(' || c != ')' {
		t.Fatalf("CloseFor ) = %c %c %v", o, c, ok)
	}
	if _, _, ok := CloseFor('x'); ok {
		t.Fatal("x is not a bracket")
	}
}

func TestPairNotFound(t *testing.T) {
	b := buffer.FromString("no brackets here")
	if got := Pair(b, pos(0, 3), '(', ')', false); got.OK {
		t.Fatal("should not find a pair")
	}
}
