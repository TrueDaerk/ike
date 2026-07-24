package editor

// testmarks.go detects the buffer's test functions for the gutter run
// markers and run.testAtCursor (#1150). Detection goes through the language
// registry's TestSpec seam (lang.TestsInFile — a line-anchored regex scan)
// and is cached per document version: the scan runs at most once per edit
// (docVersion bumps on every EventChange, load and reload), never per frame,
// matching the render-epoch discipline of the line cache. The store is a
// pointer so the many value-copies of a Model sharing one view (View has a
// value receiver) share one cache, exactly like lineCacheStore.

import (
	"sort"

	"ike/internal/lang"
)

type testMarkStore struct {
	version int
	path    string
	marks   map[int]lang.TestMatch // 0-based buffer line -> match
}

func newTestMarkStore() *testMarkStore { return &testMarkStore{version: -1} }

// testMarks returns the current line->test map, rescanning only when the
// document version or path moved since the last scan. Nil when the file's
// language declares no tests or the file is not a test file.
func (m Model) testMarks() map[int]lang.TestMatch {
	if m.testCache == nil || !m.HasFile() {
		return nil
	}
	if m.testCache.version == m.docVersion && m.testCache.path == m.path {
		return m.testCache.marks
	}
	m.testCache.version = m.docVersion
	m.testCache.path = m.path
	m.testCache.marks = nil
	if !lang.HasTests(m.path) {
		return nil
	}
	matches := lang.TestsInFile(m.path, m.buf.Lines())
	if len(matches) == 0 {
		return nil
	}
	marks := make(map[int]lang.TestMatch, len(matches))
	for _, t := range matches {
		marks[t.Line] = t
	}
	m.testCache.marks = marks
	return marks
}

// TestMarkAt reports the test declared exactly on the 0-based buffer line
// (the gutter marker's click target).
func (m Model) TestMarkAt(line int) (lang.TestMatch, bool) {
	t, ok := m.testMarks()[line]
	return t, ok
}

// NearestTestAt resolves the test a cursor on the 0-based line means: the
// nearest declaration at or above it — the enclosing or preceding test.
// ok=false when no test is declared at or above the line.
func (m Model) NearestTestAt(line int) (lang.TestMatch, bool) {
	marks := m.testMarks()
	best, found := lang.TestMatch{}, false
	for l, t := range marks {
		if l <= line && (!found || l > best.Line) {
			best, found = t, true
		}
	}
	return best, found
}

// TestLines lists the marked lines, sorted (tests and tooling).
func (m Model) TestLines() []int {
	marks := m.testMarks()
	out := make([]int, 0, len(marks))
	for l := range marks {
		out = append(out, l)
	}
	sort.Ints(out)
	return out
}
