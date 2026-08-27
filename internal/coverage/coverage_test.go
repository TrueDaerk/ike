package coverage

import (
	"reflect"
	"testing"

	"ike/internal/lang"
)

func run() []lang.FileCoverage {
	return []lang.FileCoverage{
		{Path: "/a.go", Lines: map[int]lang.CoverKind{1: lang.CoverCovered, 2: lang.CoverUncovered, 3: lang.CoverPartial}},
		{Path: "/b.go", Lines: map[int]lang.CoverKind{5: lang.CoverCovered}},
	}
}

// TestSetRunConvertsToZeroBased: the parser's 1-based lines become the
// editor's 0-based gutter lines.
func TestSetRunConvertsToZeroBased(t *testing.T) {
	s := NewStore()
	s.SetRun(run())
	marks, stale, ok := s.Marks("/a.go")
	if !ok || stale {
		t.Fatal("fresh marks must be present and not stale")
	}
	want := map[int]lang.CoverKind{0: lang.CoverCovered, 1: lang.CoverUncovered, 2: lang.CoverPartial}
	if !reflect.DeepEqual(marks, want) {
		t.Fatalf("marks = %v, want %v", marks, want)
	}
	if _, _, ok := s.Marks("/missing.go"); ok {
		t.Fatal("an uncovered file must report ok=false")
	}
}

// TestMarkStale: staleness is per file and survives until the next run
// replaces the store.
func TestMarkStale(t *testing.T) {
	s := NewStore()
	s.SetRun(run())
	if s.MarkStale("/missing.go") {
		t.Fatal("marking an unknown file must report false")
	}
	if !s.MarkStale("/a.go") {
		t.Fatal("marking a covered file must report true")
	}
	if _, stale, _ := s.Marks("/a.go"); !stale {
		t.Fatal("/a.go must be stale")
	}
	if _, stale, _ := s.Marks("/b.go"); stale {
		t.Fatal("/b.go must stay fresh")
	}
	s.SetRun(run())
	if _, stale, _ := s.Marks("/a.go"); stale {
		t.Fatal("a new run must reset staleness")
	}
}

// TestPercent: executed lines (covered or partial) over all tracked lines,
// across files; empty store reports none.
func TestPercent(t *testing.T) {
	s := NewStore()
	if _, ok := s.Percent(); ok {
		t.Fatal("an empty store has no percentage")
	}
	s.SetRun(run())
	// 3 executed (covered a:1, partial a:3, covered b:5) of 4 tracked.
	if pct, ok := s.Percent(); !ok || pct != 75 {
		t.Fatalf("percent = %v %v, want 75 true", pct, ok)
	}
}

// TestPathsAndClear: deterministic listing; Clear empties.
func TestPathsAndClear(t *testing.T) {
	s := NewStore()
	s.SetRun(run())
	if got := s.Paths(); !reflect.DeepEqual(got, []string{"/a.go", "/b.go"}) {
		t.Fatalf("paths = %v", got)
	}
	if s.Empty() {
		t.Fatal("store with a run must not be empty")
	}
	s.Clear()
	if !s.Empty() || len(s.Paths()) != 0 {
		t.Fatal("Clear must empty the store")
	}
}

// TestFilePercent: per-file ratios, the store's staleness flag alongside, and
// ok=false for a file the run never covered (#2246).
func TestFilePercent(t *testing.T) {
	s := NewStore()
	s.SetRun(run())
	// /a.go: covered + partial executed of three tracked lines.
	if pct, stale, ok := s.FilePercent("/a.go"); !ok || stale || pct < 66.6 || pct > 66.7 {
		t.Fatalf("FilePercent(/a.go) = %v %v %v", pct, stale, ok)
	}
	if pct, _, ok := s.FilePercent("/b.go"); !ok || pct != 100 {
		t.Fatalf("FilePercent(/b.go) = %v %v, want 100 true", pct, ok)
	}
	if _, _, ok := s.FilePercent("/missing.go"); ok {
		t.Fatal("an uncovered file must report ok=false")
	}
	s.MarkStale("/a.go")
	if _, stale, _ := s.FilePercent("/a.go"); !stale {
		t.Fatal("FilePercent must report the store's staleness flag")
	}
}

// TestFileStats: worst coverage first, counts and staleness per file, and a
// file with no tracked line left out (#2246).
func TestFileStats(t *testing.T) {
	s := NewStore()
	s.SetRun(append(run(), lang.FileCoverage{Path: "/empty.go"}))
	s.MarkStale("/b.go")
	got := s.FileStats()
	want := []FileStat{
		{Path: "/a.go", Covered: 2, Total: 3, Percent: 200.0 / 3, Stale: false},
		{Path: "/b.go", Covered: 1, Total: 1, Percent: 100, Stale: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FileStats = %+v, want %+v", got, want)
	}
	if len(NewStore().FileStats()) != 0 {
		t.Fatal("an empty store has no file stats")
	}
}
