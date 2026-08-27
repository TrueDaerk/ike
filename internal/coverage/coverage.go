// Package coverage holds the per-run test-coverage store (#2081): the neutral
// per-file line model a language's ParseCover produced, kept separate from the
// test-result parsing in internal/testresults so a rerun-failed — which
// produces no coverage — never silently invalidates coverage of untouched
// files. The app owns one Store; editors receive their file's marks via
// MarksMsg and render them in the gutter.
package coverage

import (
	"sort"

	"ike/internal/lang"
)

// MarksMsg pushes one file's coverage marks to its editor views. Nil Marks
// clears the file's marks (the display toggle hiding them). Lines are 0-based,
// matching the editor's gutter maps.
type MarksMsg struct {
	Path  string
	Marks map[int]lang.CoverKind
	// Stale flags data from before an edit of the file: still shown, but
	// visibly neutralized so it never reads as current.
	Stale bool
}

// entry is one file's stored coverage.
type entry struct {
	marks map[int]lang.CoverKind // 0-based lines
	stale bool
}

// Store holds the last coverage run's per-file line marks. The zero value is
// unusable; construct with NewStore. Not goroutine-safe — it lives on the app
// model and is only touched inside Update.
type Store struct {
	files map[string]*entry
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{files: map[string]*entry{}}
}

// SetRun replaces the store's content with a run's parsed coverage,
// converting the parser's 1-based lines to the editor's 0-based ones.
func (s *Store) SetRun(files []lang.FileCoverage) {
	s.files = make(map[string]*entry, len(files))
	for _, f := range files {
		marks := make(map[int]lang.CoverKind, len(f.Lines))
		for line, kind := range f.Lines {
			marks[line-1] = kind
		}
		s.files[f.Path] = &entry{marks: marks}
	}
}

// Marks returns a file's stored marks and staleness; ok=false when the last
// run covered no such file.
func (s *Store) Marks(path string) (marks map[int]lang.CoverKind, stale, ok bool) {
	e := s.files[path]
	if e == nil {
		return nil, false, false
	}
	return e.marks, e.stale, true
}

// MarkStale flags a file's coverage as outdated (the file changed since the
// run); reports whether the store held the file at all.
func (s *Store) MarkStale(path string) bool {
	e := s.files[path]
	if e == nil {
		return false
	}
	e.stale = true
	return true
}

// Paths lists the covered files, sorted for deterministic iteration.
func (s *Store) Paths() []string {
	out := make([]string, 0, len(s.files))
	for p := range s.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Empty reports whether the store holds no coverage data.
func (s *Store) Empty() bool {
	return len(s.files) == 0
}

// Percent is the run-wide line coverage percentage — executed lines (covered
// or partial) over all tracked lines. ok=false while the store is empty.
func (s *Store) Percent() (float64, bool) {
	covered, total := 0, 0
	for _, e := range s.files {
		c, t := e.count()
		covered, total = covered+c, total+t
	}
	return percent(covered, total)
}

// FilePercent is one file's line-coverage percentage (#2246) — the figure the
// status-line segment and the Test Results detail listing show. stale repeats
// the store's staleness flag for the file; ok=false when the last run covered
// no such file, or tracked no line in it.
func (s *Store) FilePercent(path string) (pct float64, stale, ok bool) {
	e := s.files[path]
	if e == nil {
		return 0, false, false
	}
	pct, ok = percent(e.count())
	return pct, e.stale, ok
}

// FileStat is one file's coverage summary: executed lines (covered or
// partial) over the file's tracked lines, plus the derived percentage and the
// store's staleness flag.
type FileStat struct {
	Path    string
	Covered int
	Total   int
	Percent float64
	Stale   bool
}

// FileStats summarizes every covered file, worst coverage first and
// path-sorted within an equal percentage — the order the Test Results detail
// column reads as a to-do list. Files whose run tracked no line at all are
// left out; they carry no information.
func (s *Store) FileStats() []FileStat {
	out := make([]FileStat, 0, len(s.files))
	for path, e := range s.files {
		covered, total := e.count()
		pct, ok := percent(covered, total)
		if !ok {
			continue
		}
		out = append(out, FileStat{Path: path, Covered: covered, Total: total, Percent: pct, Stale: e.stale})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Percent != out[j].Percent {
			return out[i].Percent < out[j].Percent
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// count reports one file's executed and tracked line counts.
func (e *entry) count() (covered, total int) {
	for _, kind := range e.marks {
		total++
		if kind != lang.CoverUncovered {
			covered++
		}
	}
	return covered, total
}

// percent is the shared covered/total ratio; ok=false when nothing is tracked.
func percent(covered, total int) (float64, bool) {
	if total == 0 {
		return 0, false
	}
	return 100 * float64(covered) / float64(total), true
}

// Clear drops every file's coverage.
func (s *Store) Clear() {
	s.files = map[string]*entry{}
}
