package search

import (
	"testing"

	"ike/internal/editor/buffer"
)

func TestLiteralLineMatches(t *testing.T) {
	b := buffer.FromString("the cat sat on the mat")
	q := Compile("at", false)
	spans := q.AllMatches(b)
	if len(spans) != 3 {
		t.Fatalf("matches=%d want 3 (%v)", len(spans), spans)
	}
	if spans[0].Start != 5 {
		t.Fatalf("first match start=%d want 5", spans[0].Start)
	}
}

func TestNextForwardWraps(t *testing.T) {
	b := buffer.FromString("foo\nbar foo\nfoo")
	q := Compile("foo", false)
	// from line 2 (last "foo"), forward wraps to line 0.
	p, ok := q.Next(b, buffer.Position{Line: 2, Col: 0}, Forward, 1)
	if !ok || p != (buffer.Position{Line: 0, Col: 0}) {
		t.Fatalf("forward wrap=%v ok=%v want {0 0}", p, ok)
	}
}

func TestNextForwardAndCount(t *testing.T) {
	b := buffer.FromString("x x x x")
	q := Compile("x", false)
	p, _ := q.Next(b, buffer.Position{Line: 0, Col: 0}, Forward, 1)
	if p.Col != 2 {
		t.Fatalf("n col=%d want 2", p.Col)
	}
	p, _ = q.Next(b, buffer.Position{Line: 0, Col: 0}, Forward, 2)
	if p.Col != 4 {
		t.Fatalf("2n col=%d want 4", p.Col)
	}
}

func TestNextBackward(t *testing.T) {
	b := buffer.FromString("a foo b foo c")
	q := Compile("foo", false)
	p, ok := q.Next(b, buffer.Position{Line: 0, Col: 8}, Backward, 1)
	if !ok || p.Col != 2 {
		t.Fatalf("backward col=%d want 2", p.Col)
	}
}

func TestRegexMatch(t *testing.T) {
	b := buffer.FromString("a1 b22 c333")
	q := Compile(`[0-9]+`, true)
	spans := q.AllMatches(b)
	if len(spans) != 3 || spans[2].Start != 8 || spans[2].End != 11 {
		t.Fatalf("regex spans=%v", spans)
	}
}

func TestInvalidRegexFallsBackToLiteral(t *testing.T) {
	q := Compile("a(b", true) // invalid regex
	if q.Regex {
		t.Fatal("invalid regex should fall back to literal")
	}
	b := buffer.FromString("xa(byz")
	if got := q.AllMatches(b); len(got) != 1 {
		t.Fatalf("literal fallback matches=%v", got)
	}
}

func TestNoMatch(t *testing.T) {
	b := buffer.FromString("abc")
	q := Compile("zzz", false)
	if _, ok := q.Next(b, buffer.Position{Line: 0, Col: 0}, Forward, 1); ok {
		t.Fatal("no match should report ok=false")
	}
}
