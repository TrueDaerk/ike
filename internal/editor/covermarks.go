package editor

import (
	"ike/internal/lang"
)

// covermarks.go holds the gutter test-coverage state (#2081): the app parses a
// coverage run's profile through the language's ParseCover seam and pushes
// per-file marks (coverage.MarksMsg); the editor only stores and renders them.
// Visibility is gated by the editor.marks.coverage toggle like every other
// mark class (#1259). Staleness is two-layered: the app flags store-level
// staleness (file edited in another view, or reopened after edits), and the
// view compares its own document version against the one the marks landed on —
// so typing immediately neutralizes the marks without any push bookkeeping.

// setCoverageMarks replaces the coverage-mark set; nil clears it.
func (m *Model) setCoverageMarks(marks map[int]lang.CoverKind, stale bool) {
	if len(marks) == 0 {
		m.covMarks = nil
		m.covStale = false
		return
	}
	m.covMarks = marks
	m.covStale = stale
	m.covDocVersion = m.docVersion
}

// coverMarkAt reports the coverage mark on a 0-based line and whether the data
// is stale; nothing while the visibility toggle is off (cached marks survive a
// toggle round-trip, like inheritance arrows).
func (m Model) coverMarkAt(line int) (kind lang.CoverKind, stale, ok bool) {
	if len(m.covMarks) == 0 || !m.coverVisible() {
		return 0, false, false
	}
	kind, ok = m.covMarks[line]
	return kind, m.covStale || m.docVersion != m.covDocVersion, ok
}

// coverVisible reports whether coverage marks render; unset means on,
// matching the config default.
func (m Model) coverVisible() bool {
	if m.cfg == nil {
		return true
	}
	return boolOr(m.cfg, "editor.marks.coverage", true)
}
