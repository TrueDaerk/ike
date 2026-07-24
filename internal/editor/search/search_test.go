package search

import (
	"testing"

	"ike/internal/editor/buffer"
)

func TestLiteralLineMatches(t *testing.T) {
	b := buffer.FromString("the cat sat on the mat")
	q := Compile("at", false, CaseSmart)
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
	q := Compile("foo", false, CaseSmart)
	// from line 2 (last "foo"), forward wraps to line 0.
	p, ok := q.Next(b, buffer.Position{Line: 2, Col: 0}, Forward, 1)
	if !ok || p != (buffer.Position{Line: 0, Col: 0}) {
		t.Fatalf("forward wrap=%v ok=%v want {0 0}", p, ok)
	}
}

func TestNextForwardAndCount(t *testing.T) {
	b := buffer.FromString("x x x x")
	q := Compile("x", false, CaseSmart)
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
	q := Compile("foo", false, CaseSmart)
	p, ok := q.Next(b, buffer.Position{Line: 0, Col: 8}, Backward, 1)
	if !ok || p.Col != 2 {
		t.Fatalf("backward col=%d want 2", p.Col)
	}
}

func TestRegexMatch(t *testing.T) {
	b := buffer.FromString("a1 b22 c333")
	q := Compile(`[0-9]+`, true, CaseSmart)
	spans := q.AllMatches(b)
	if len(spans) != 3 || spans[2].Start != 8 || spans[2].End != 11 {
		t.Fatalf("regex spans=%v", spans)
	}
}

func TestInvalidRegexFallsBackToLiteral(t *testing.T) {
	q := Compile("a(b", true, CaseSmart) // invalid regex
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
	q := Compile("zzz", false, CaseSmart)
	if _, ok := q.Next(b, buffer.Position{Line: 0, Col: 0}, Forward, 1); ok {
		t.Fatal("no match should report ok=false")
	}
}

func TestSmartcaseLowercaseFoldsCase(t *testing.T) {
	b := buffer.FromString("Foo foo FOO\n")
	q := Compile("foo", false, CaseSmart)
	spans := q.LineMatches(b, 0)
	if len(spans) != 3 {
		t.Fatalf("lowercase pattern should match all cases, got %d spans", len(spans))
	}
}

func TestSmartcaseUppercaseStaysExact(t *testing.T) {
	b := buffer.FromString("Foo foo FOO\n")
	q := Compile("Foo", false, CaseSmart)
	spans := q.LineMatches(b, 0)
	if len(spans) != 1 || spans[0].Start != 0 {
		t.Fatalf("uppercase pattern must match exactly, got %+v", spans)
	}
}

func TestSmartcaseAppliesToRegex(t *testing.T) {
	b := buffer.FromString("Alpha ALPHA alpha\n")
	q := Compile("al.ha", true, CaseSmart)
	if got := len(q.LineMatches(b, 0)); got != 3 {
		t.Fatalf("lowercase regex should fold case, got %d", got)
	}
	q = Compile("Al.ha", true, CaseSmart)
	if got := len(q.LineMatches(b, 0)); got != 1 {
		t.Fatalf("uppercase regex must stay exact, got %d", got)
	}
}

func TestCompileExactSkipsSmartcase(t *testing.T) {
	b := buffer.FromString("word Word WORD\n")
	q := CompileExact("word")
	spans := q.LineMatches(b, 0)
	if len(spans) != 1 || spans[0].Start != 0 {
		t.Fatalf("CompileExact must be case-sensitive, got %+v", spans)
	}
}

// TestCompileCaseModes pins the Case parameter (#1111): CaseFold matches
// insensitively regardless of the pattern's spelling, CaseExact never folds,
// and CaseSmart keeps the vim smartcase behavior (#257).
func TestCompileCaseModes(t *testing.T) {
	b := buffer.FromString("FOO foo Foo\n")
	count := func(q Query) int { return len(q.AllMatches(b)) }
	if got := count(Compile("Foo", false, CaseFold)); got != 3 {
		t.Errorf("CaseFold literal matches=%d want 3", got)
	}
	if got := count(Compile("foo", false, CaseExact)); got != 1 {
		t.Errorf("CaseExact literal matches=%d want 1", got)
	}
	if got := count(Compile("foo", false, CaseSmart)); got != 3 {
		t.Errorf("CaseSmart lowercase matches=%d want 3", got)
	}
	if got := count(Compile("Foo", false, CaseSmart)); got != 1 {
		t.Errorf("CaseSmart uppercase matches=%d want 1", got)
	}
	if got := count(Compile("F[Oo]O", true, CaseFold)); got != 3 {
		t.Errorf("CaseFold regex matches=%d want 3", got)
	}
	if got := count(Compile("f.o", true, CaseExact)); got != 1 {
		t.Errorf("CaseExact regex matches=%d want 1", got)
	}
}
