package search

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// Tests for the bounded landing scan (#2734): Next stops at the match it
// lands on, Step resumes under a byte budget, and the long-line cut keeps a
// landing on a minified line from costing the whole line.

// nextByAllMatches is the pre-#2734 landing — the modulo pick over every
// match — kept as the oracle the bounded walk must agree with.
func nextByAllMatches(q Query, b *buffer.Buffer, from buffer.Position, dir Direction, count int) (buffer.Position, bool) {
	all := q.AllMatches(b)
	if len(all) == 0 {
		return from, false
	}
	idx := -1
	if dir == Forward {
		for i, s := range all {
			if s.Line > from.Line || (s.Line == from.Line && s.Start > from.Col) {
				idx = i
				break
			}
		}
		if idx < 0 {
			idx = 0
		}
		idx = (idx + count - 1) % len(all)
	} else {
		for i := len(all) - 1; i >= 0; i-- {
			s := all[i]
			if s.Line < from.Line || (s.Line == from.Line && s.Start < from.Col) {
				idx = i
				break
			}
		}
		if idx < 0 {
			idx = len(all) - 1
		}
		idx = ((idx-(count-1))%len(all) + len(all)) % len(all)
	}
	return buffer.Position{Line: all[idx].Line, Col: all[idx].Start}, true
}

func TestNextAgreesWithFullScanOracle(t *testing.T) {
	b := buffer.New([]string{"ab ab", "", "xx ab", "ab", "zz", "ab ab ab"})
	for _, pat := range []string{"ab", "zz", "none", "a."} {
		q := Compile(pat, pat == "a.", CaseExact)
		for line := 0; line < b.LineCount(); line++ {
			for col := 0; col <= 8; col++ {
				from := buffer.Position{Line: line, Col: col}
				for _, dir := range []Direction{Forward, Backward} {
					for count := 1; count <= 9; count++ {
						want, wantOK := nextByAllMatches(q, b, from, dir, count)
						got, gotOK := q.Next(b, from, dir, count)
						if got != want || gotOK != wantOK {
							t.Fatalf("Next(%q from %v dir %d count %d) = %v,%v want %v,%v", pat, from, dir, count, got, gotOK, want, wantOK)
						}
					}
				}
			}
		}
	}
}

func TestStepStopsAtFirstMatchWithoutScanningPast(t *testing.T) {
	// The match sits on the line after the cursor and a 700-byte filler line
	// follows it; a 30-byte budget covers the first two lines only, so the
	// step can only find the needle by stopping there instead of collecting
	// every match first.
	lines := []string{"start", "the needle here", strings.Repeat("filler ", 100)}
	b := buffer.New(lines)
	q := Compile("needle", false, CaseExact)
	l := q.Step(b, q.Begin(buffer.Position{}, Forward, 1), 30)
	if !l.Found || !l.Done || l.Pos != (buffer.Position{Line: 1, Col: 4}) {
		t.Fatalf("Step = %+v, want the needle on line 1 col 4 after two lines", l)
	}
}

func TestStepResumesUnderBudgetAndWraps(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, strings.Repeat("x", 100))
	}
	lines[3] = "here is the needle"
	b := buffer.New(lines)
	q := Compile("needle", false, CaseExact)
	// Forward from line 10: the walk runs to the end, wraps, and finds line
	// 3 — in several budgeted steps that each hand back a resumable scan.
	s := q.Begin(buffer.Position{Line: 10}, Forward, 1)
	steps := 0
	for {
		l := q.Step(b, s, 500)
		steps++
		if l.Done {
			if !l.Found || l.Pos != (buffer.Position{Line: 3, Col: 12}) {
				t.Fatalf("landing = %+v, want line 3 col 12", l)
			}
			break
		}
		s = l.Scan
		if steps > 100 {
			t.Fatal("the scan never finished")
		}
	}
	if steps < 5 {
		t.Fatalf("a 500-byte budget over ~4 KB of lines must take several steps, took %d", steps)
	}
	// A pattern matching nowhere ends Done without Found, after covering
	// the whole buffer once.
	q = Compile("absent", false, CaseExact)
	s = q.Begin(buffer.Position{Line: 10, Col: 3}, Backward, 1)
	for {
		l := q.Step(b, s, 700)
		if l.Done {
			if l.Found {
				t.Fatalf("absent pattern found at %v", l.Pos)
			}
			break
		}
		s = l.Scan
	}
}

func TestStepCountWalksMatchesAcrossBudgets(t *testing.T) {
	b := buffer.New([]string{"a", strings.Repeat("-", 300), "a", strings.Repeat("-", 300), "a"})
	q := Compile("a", false, CaseExact)
	s := q.Begin(buffer.Position{}, Forward, 2)
	var l Landing
	for {
		l = q.Step(b, s, 100)
		if l.Done {
			break
		}
		s = l.Scan
	}
	if !l.Found || l.Pos != (buffer.Position{Line: 4}) {
		t.Fatalf("2n from line 0 = %+v, want line 4", l)
	}
}

func TestMultilineStepMatchesOracle(t *testing.T) {
	b := buffer.New([]string{"foo", "bar", "x", "foo", "bar", "foo", "baz", "foo", "bar"})
	q := Compile("foo\nbar", false, CaseExact)
	for line := 0; line < b.LineCount(); line++ {
		from := buffer.Position{Line: line}
		for _, dir := range []Direction{Forward, Backward} {
			for count := 1; count <= 4; count++ {
				want, wantOK := nextByAllMatches(q, b, from, dir, count)
				got, gotOK := q.Next(b, from, dir, count)
				if got != want || gotOK != wantOK {
					t.Fatalf("multi Next(from %v dir %d count %d) = %v,%v want %v,%v", from, dir, count, got, gotOK, want, wantOK)
				}
				// The budgeted walk lands on the same match.
				s := q.Begin(from, dir, count)
				var l Landing
				for {
					l = q.Step(b, s, 5)
					if l.Done {
						break
					}
					s = l.Scan
				}
				if l.Found != wantOK || (wantOK && l.Pos != want) {
					t.Fatalf("multi Step(from %v dir %d count %d) = %+v want %v,%v", from, dir, count, l, want, wantOK)
				}
			}
		}
	}
}

func TestLongLineLandingCutsAtTheDepartureColumn(t *testing.T) {
	// A single line well past LongLineBytes: the forward landing must scan
	// on from the cursor, the backward one the prefix, and both agree with
	// the whole-line answer.
	unit := "<div>é</div>" // a multi-byte rune keeps byte and rune columns apart
	line := strings.Repeat(unit, 1000)
	b := buffer.New([]string{"top", line, "bottom"})
	q := Compile("div", false, CaseExact)
	if len(line) <= LongLineBytes {
		t.Fatal("setup: the line must be long")
	}
	for _, col := range []int{0, 1, 5, 6, 12, 5000, 11990} {
		from := buffer.Position{Line: 1, Col: col}
		for _, dir := range []Direction{Forward, Backward} {
			want, _ := nextByAllMatches(q, b, from, dir, 1)
			got, ok := q.Next(b, from, dir, 1)
			if !ok || got != want {
				t.Fatalf("long-line Next(col %d dir %d) = %v,%v want %v", col, dir, got, ok, want)
			}
		}
	}
	// The regex path, case-folded, on the same line.
	q = Compile("d.v", true, CaseSmart)
	for _, col := range []int{0, 7, 6000} {
		from := buffer.Position{Line: 1, Col: col}
		want, _ := nextByAllMatches(q, b, from, Forward, 1)
		if got, _ := q.Next(b, from, Forward, 1); got != want {
			t.Fatalf("long-line regex Next(col %d) = %v want %v", col, got, want)
		}
	}
}

func TestLineMatchesInWindowsLongLines(t *testing.T) {
	line := strings.Repeat("ab ", 3000) // 9000 bytes, 9000 runes
	b := buffer.New([]string{line})
	q := Compile("ab", false, CaseExact)
	spans := q.LineMatchesIn(b, 0, 300, 330)
	if len(spans) != 10 {
		t.Fatalf("window [300,330) holds 10 matches, got %d", len(spans))
	}
	if spans[0].Start != 300 || spans[0].End != 302 || spans[9].Start != 327 {
		t.Fatalf("windowed columns must be line columns: %v", spans[:1])
	}
	// A short line answers exactly like LineMatches, window or not.
	b = buffer.New([]string{"ab ab ab"})
	if got := q.LineMatchesIn(b, 0, 4, 5); len(got) != 3 {
		t.Fatalf("short line must return every match, got %d", len(got))
	}
}

func TestScanMatchesCapsByBytes(t *testing.T) {
	// Few lines, each far larger than the byte budget allows for all of them.
	big := strings.Repeat("needle ", MaxScanBytes/14) // ~half the budget per line
	b := buffer.New([]string{big, big, big, big})
	q := Compile("needle", false, CaseExact)
	_, capped := q.ScanMatches(b, 1<<30, 0)
	if !capped {
		t.Fatal("four lines of half the byte budget each must cap the tally")
	}
	_, capped = q.ScanMatches(buffer.New([]string{big}), 1<<30, 0)
	if capped {
		t.Fatal("a single line within the byte budget must not cap")
	}
}

func BenchmarkLandingLongLine(b *testing.B) {
	line := strings.Repeat(`<div class="row"><span>alpha</span></div>`, 16000) // ~700 KB
	buf := buffer.New([]string{line, line, line})
	q := Compile("div", true, CaseSmart)
	from := buffer.Position{Line: 1, Col: 300000}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Next(buf, from, Forward, 1)
	}
}
