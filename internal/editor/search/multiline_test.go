package search

// multiline_test.go covers the across-line-boundaries matching path (#2600).

import (
	"testing"

	"ike/internal/editor/buffer"
)

func TestMultilineLiteralMatchesAcrossLines(t *testing.T) {
	b := buffer.FromString("x foo\nbar y\nfoo\nbaz")
	q := Compile("foo\nbar", false, CaseSmart)
	all := q.AllMatches(b)
	if len(all) != 1 {
		t.Fatalf("matches=%d want 1 (%v)", len(all), all)
	}
	// One span per match, anchored where the match starts.
	if all[0] != (Span{Line: 0, Start: 2, End: 5}) {
		t.Fatalf("head span=%v want {0 2 5}", all[0])
	}
	// The highlighter gets the pieces the match contributes to each line.
	if got := q.LineMatches(b, 0); len(got) != 1 || got[0] != (Span{Line: 0, Start: 2, End: 5}) {
		t.Fatalf("line 0 pieces=%v", got)
	}
	if got := q.LineMatches(b, 1); len(got) != 1 || got[0] != (Span{Line: 1, Start: 0, End: 3}) {
		t.Fatalf("line 1 pieces=%v", got)
	}
	if got := q.LineMatches(b, 2); len(got) != 0 {
		t.Fatalf("line 2 should hold no piece: %v", got)
	}
}

func TestMultilineNextAndTally(t *testing.T) {
	b := buffer.FromString("foo\nbar\nnope\nfoo\nbar")
	q := Compile("foo\nbar", false, CaseSmart)
	tal := q.CountMatches(b, buffer.Position{Line: 0, Col: 0}, 0, 0)
	if tal.Total != 2 || tal.Index != 1 {
		t.Fatalf("tally=%+v want total 2 index 1", tal)
	}
	p, ok := q.Next(b, buffer.Position{Line: 0, Col: 0}, Forward, 1)
	if !ok || p != (buffer.Position{Line: 3, Col: 0}) {
		t.Fatalf("n landed at %v ok=%v, want {3 0}", p, ok)
	}
	// N from the second match wraps back to the first.
	p, ok = q.Next(b, buffer.Position{Line: 3, Col: 0}, Backward, 1)
	if !ok || p != (buffer.Position{Line: 0, Col: 0}) {
		t.Fatalf("N landed at %v ok=%v, want {0 0}", p, ok)
	}
}

func TestMultilineSpansThreeLines(t *testing.T) {
	b := buffer.FromString("aa\nbb\ncc\ndd")
	q := Compile("a\nbb\nc", false, CaseSmart)
	all := q.AllMatches(b)
	if len(all) != 1 {
		t.Fatalf("matches=%d want 1", len(all))
	}
	// The whole middle line is painted, the outer lines only their part.
	if got := q.LineMatches(b, 1); len(got) != 1 || got[0] != (Span{Line: 1, Start: 0, End: 2}) {
		t.Fatalf("middle pieces=%v", got)
	}
	if got := q.LineMatches(b, 2); len(got) != 1 || got[0] != (Span{Line: 2, Start: 0, End: 1}) {
		t.Fatalf("last pieces=%v", got)
	}
	if got := q.LineMatches(b, 3); len(got) != 0 {
		t.Fatalf("line 3 should hold no piece: %v", got)
	}
}

func TestMultilineWindowAgreesWithWholeScan(t *testing.T) {
	// A self-overlapping pattern: the whole-buffer scan finds one match
	// (lines 0-1), so the windowed per-line scan must not invent a second one
	// on lines 1-2 — the window widens until the boundary settles.
	b := buffer.FromString("a\na\na")
	q := Compile("a\na", false, CaseSmart)
	if all := q.AllMatches(b); len(all) != 1 {
		t.Fatalf("whole-buffer matches=%d want 1 (%v)", len(all), all)
	}
	if got := q.LineMatches(b, 2); len(got) != 0 {
		t.Fatalf("line 2 should hold no piece: %v", got)
	}
}

func TestMultilineSmartcaseAndRegex(t *testing.T) {
	b := buffer.FromString("FOO\nBAR")
	// All-lowercase pattern folds case, as anywhere else.
	if all := Compile("foo\nbar", false, CaseSmart).AllMatches(b); len(all) != 1 {
		t.Fatalf("smartcase matches=%d want 1", len(all))
	}
	if all := Compile("foo\nbar", false, CaseExact).AllMatches(b); len(all) != 0 {
		t.Fatalf("exact matches=%d want 0", len(all))
	}
	// A regex pattern carrying a break matches across the boundary too, and
	// "^"/"$" keep meaning line start / line end.
	if all := Compile(`FOO$\nBAR`, true, CaseExact).AllMatches(b); len(all) != 1 {
		t.Fatalf("regex matches=%d want 1", len(all))
	}
}

func TestSingleLinePatternKeepsPerLinePath(t *testing.T) {
	b := buffer.FromString("foo\nfoo")
	q := Compile("foo", false, CaseSmart)
	if q.multi {
		t.Fatal("a break-free pattern must not take the multiline path")
	}
	if all := q.AllMatches(b); len(all) != 2 {
		t.Fatalf("matches=%d want 2", len(all))
	}
}

func TestMultilineMatchesLineIsFalse(t *testing.T) {
	// One line can never answer a pattern that spans lines — the follow
	// filter's predicate must say no rather than half-match.
	if Compile("a\nb", false, CaseSmart).MatchesLine("a") {
		t.Fatal("MatchesLine should be false for a multiline pattern")
	}
}
