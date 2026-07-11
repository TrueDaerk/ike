package editor

import (
	"testing"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// occMsg builds a DocumentHighlightsMsg anchored at (line, col) marking the
// [start, end) column span of that line.
func occMsg(path string, line, col, start, end, kind int) ilsp.DocumentHighlightsMsg {
	return ilsp.DocumentHighlightsMsg{
		Path: path, Line: line, Col: col,
		Highlights: []ilsp.DocumentHighlight{{
			Range: buffer.Range{Start: buffer.Position{Line: line, Col: start}, End: buffer.Position{Line: line, Col: end}},
			Kind:  kind,
		}},
	}
}

// TestDocumentHighlightsApplyAndCover guards the install path: a reply
// anchored at the current cursor installs its marks, occurrenceAt covers
// exactly the end-exclusive range, and the kind round-trips.
func TestDocumentHighlightsApplyAndCover(t *testing.T) {
	m, path := loaded(t, "alpha beta alpha\n")
	m, _ = m.Update(occMsg(path, 0, 0, 0, 5, protocol.HighlightWrite))

	kind, ok := m.occurrenceAt(0, 0)
	if !ok || kind != protocol.HighlightWrite {
		t.Fatalf("occurrenceAt(0,0) = %d, %v; want write kind", kind, ok)
	}
	if _, ok := m.occurrenceAt(0, 4); !ok {
		t.Error("col 4 is inside the range and must be covered")
	}
	if _, ok := m.occurrenceAt(0, 5); ok {
		t.Error("ranges are end-exclusive; col 5 must not be covered")
	}
	if _, ok := m.occurrenceAt(1, 0); ok {
		t.Error("other lines must not be covered")
	}

	// An empty reply for the same position clears the marks.
	m, _ = m.Update(ilsp.DocumentHighlightsMsg{Path: path, Line: 0, Col: 0})
	if _, ok := m.occurrenceAt(0, 0); ok {
		t.Error("empty highlight set must clear the marks")
	}
}

// TestDocumentHighlightsStaleAndOtherPath guards the two drop paths: a reply
// for another document is ignored entirely, and a reply anchored at a
// position the cursor has left clears the marks instead of installing them.
func TestDocumentHighlightsStaleAndOtherPath(t *testing.T) {
	m, path := loaded(t, "alpha beta\nalpha\n")
	m, _ = m.Update(occMsg(path, 0, 0, 0, 5, protocol.HighlightRead))
	if _, ok := m.occurrenceAt(0, 0); !ok {
		t.Fatal("marks should be installed")
	}

	// Another document's reply must not touch this editor's marks.
	m, _ = m.Update(occMsg("/other.go", 0, 0, 6, 10, protocol.HighlightRead))
	if _, ok := m.occurrenceAt(0, 6); ok {
		t.Error("other-path msg must be ignored")
	}
	if _, ok := m.occurrenceAt(0, 0); !ok {
		t.Error("other-path msg must not clear existing marks")
	}

	// A reply raced by a cursor move is stale: it clears rather than installs.
	m.SetCursor(1, 0)
	m, _ = m.Update(occMsg(path, 0, 0, 0, 5, protocol.HighlightRead))
	if _, ok := m.occurrenceAt(0, 0); ok {
		t.Error("stale reply must clear the marks, not install them")
	}
}

// TestOccurrenceColorKinds maps write to the warm slot and everything else
// (read, plain text, absent kind) to the cool one.
func TestOccurrenceColorKinds(t *testing.T) {
	m, _ := loaded(t, "x\n")
	write := m.occurrenceColor(protocol.HighlightWrite)
	read := m.occurrenceColor(protocol.HighlightRead)
	text := m.occurrenceColor(protocol.HighlightText)
	none := m.occurrenceColor(0)
	if write == read {
		t.Error("write and read occurrences must use distinct theme slots")
	}
	if read != text || read != none {
		t.Error("text and unspecified kinds must fall back to the read slot")
	}
}
