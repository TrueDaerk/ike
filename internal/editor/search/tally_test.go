package search

import (
	"strconv"
	"testing"

	"ike/internal/editor/buffer"
)

// Tests for the capped match tally behind the search counter (#2145).

func tallyBuf(lines ...string) *buffer.Buffer { return buffer.New(lines) }

func TestCountMatchesIndexAndTotal(t *testing.T) {
	b := tallyBuf("foo bar foo", "baz", "foo")
	q := Compile("foo", false, CaseExact)

	cases := []struct {
		pos   buffer.Position
		index int
	}{
		{buffer.Position{Line: 0, Col: 0}, 1},
		{buffer.Position{Line: 0, Col: 8}, 2},
		{buffer.Position{Line: 2, Col: 0}, 3},
		{buffer.Position{Line: 1, Col: 0}, 0}, // cursor sits on no match
	}
	for _, c := range cases {
		got := q.CountMatches(b, c.pos, 0, 0)
		if got.Index != c.index || got.Total != 3 || got.Capped {
			t.Fatalf("CountMatches at %v = %+v, want index %d total 3 uncapped", c.pos, got, c.index)
		}
	}
}

func TestCountMatchesEmptyQueryAndNoHits(t *testing.T) {
	b := tallyBuf("alpha", "beta")
	if got := (Query{}).CountMatches(b, buffer.Position{}, 0, 0); got != (Tally{}) {
		t.Fatalf("empty query tally = %+v, want zero", got)
	}
	q := Compile("gamma", false, CaseExact)
	if got := q.CountMatches(b, buffer.Position{}, 0, 0); got.Total != 0 || got.Capped {
		t.Fatalf("no-hit tally = %+v, want total 0 uncapped", got)
	}
}

func TestCountMatchesCapsOnMatchBudget(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "x x x"
	}
	b := tallyBuf(lines...) // 150 matches
	q := Compile("x", false, CaseExact)

	got := q.CountMatches(b, buffer.Position{Line: 0, Col: 0}, 10, 0)
	if got.Total != 10 || !got.Capped {
		t.Fatalf("capped tally = %+v, want total 10 capped", got)
	}
	if got.Index != 1 {
		t.Fatalf("capped tally index = %d, want 1", got.Index)
	}
	// A cursor past the cut cannot be located, so the index degrades to 0
	// while the capped total still reports a lower bound.
	past := q.CountMatches(b, buffer.Position{Line: 40, Col: 0}, 10, 0)
	if past.Index != 0 || past.Total != 10 || !past.Capped {
		t.Fatalf("past-cut tally = %+v, want index 0 total 10 capped", past)
	}
}

func TestCountMatchesCapsOnLineBudget(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "hit " + strconv.Itoa(i)
	}
	b := tallyBuf(lines...)
	q := Compile("hit", false, CaseExact)

	got := q.CountMatches(b, buffer.Position{Line: 0, Col: 0}, 0, 20)
	if got.Total != 20 || !got.Capped {
		t.Fatalf("line-capped tally = %+v, want total 20 capped", got)
	}
	// The whole buffer inside the line budget counts exactly.
	full := q.CountMatches(b, buffer.Position{Line: 0, Col: 0}, 0, 1000)
	if full.Total != 100 || full.Capped {
		t.Fatalf("full tally = %+v, want total 100 uncapped", full)
	}
}

func TestScanMatchesOrderAndIndexOf(t *testing.T) {
	b := tallyBuf("ab ab", "", "ab")
	q := Compile("ab", false, CaseExact)
	spans, capped := q.ScanMatches(b, 0, 0)
	if capped {
		t.Fatal("ScanMatches capped on a tiny buffer")
	}
	want := []Span{{Line: 0, Start: 0, End: 2}, {Line: 0, Start: 3, End: 5}, {Line: 2, Start: 0, End: 2}}
	if len(spans) != len(want) {
		t.Fatalf("spans = %v, want %v", spans, want)
	}
	for i, s := range spans {
		if s != want[i] {
			t.Fatalf("span %d = %v, want %v", i, s, want[i])
		}
	}
	if got := IndexOf(spans, buffer.Position{Line: 0, Col: 3}); got != 2 {
		t.Fatalf("IndexOf = %d, want 2", got)
	}
	if got := IndexOf(spans, buffer.Position{Line: 0, Col: 1}); got != 0 {
		t.Fatalf("IndexOf mid-span = %d, want 0", got)
	}
}

func TestQueryIDDistinguishesModes(t *testing.T) {
	lit := Compile("foo", false, CaseExact)
	fold := Compile("foo", false, CaseFold)
	re := Compile("foo", true, CaseExact)
	ids := map[string]bool{lit.ID(): true, fold.ID(): true, re.ID(): true}
	if len(ids) != 3 {
		t.Fatalf("IDs collide: %v", ids)
	}
	if Compile("foo", false, CaseExact).ID() != lit.ID() {
		t.Fatal("equal queries must share an ID")
	}
}
