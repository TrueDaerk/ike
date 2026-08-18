package editor

import (
	"testing"

	"ike/internal/editor/buffer"
)

// TestDOMMatchesOverlay guards the DOM inspector's editor overlay (#1929):
// the path gate, the per-line projection of multi-line ranges (rune columns,
// end exclusive), the current-match flag and the clearing delivery.
func TestDOMMatchesOverlay(t *testing.T) {
	m, path := loaded(t, "<div>\n  <p>x</p>\n</div>\n")
	rng := func(sl, sc, el, ec int) buffer.Range {
		return buffer.Range{Start: buffer.Position{Line: sl, Col: sc}, End: buffer.Position{Line: el, Col: ec}}
	}
	m, _ = m.Update(DOMMatchesMsg{Path: path, Ranges: []buffer.Range{
		rng(0, 0, 0, 5), // <div>
		rng(1, 2, 1, 5), // <p>
	}, Current: 1})

	spans := m.domLineSpans(0)
	if len(spans) != 1 || spans[0].start != 0 || spans[0].end != 5 || spans[0].cur {
		t.Fatalf("line 0 spans = %+v", spans)
	}
	spans = m.domLineSpans(1)
	if len(spans) != 1 || spans[0].start != 2 || spans[0].end != 5 || !spans[0].cur {
		t.Fatalf("line 1 spans = %+v", spans)
	}
	if got := m.domLineSpans(2); len(got) != 0 {
		t.Fatalf("line 2 spans = %+v", got)
	}

	// A range spanning lines covers the middle lines fully.
	m, _ = m.Update(DOMMatchesMsg{Path: path, Ranges: []buffer.Range{rng(0, 3, 2, 2)}, Current: -1})
	spans = m.domLineSpans(1)
	if len(spans) != 1 || spans[0].start != 0 || spans[0].end != m.buf.RuneLen(1) {
		t.Fatalf("middle line spans = %+v", spans)
	}

	// Another document's delivery is ignored; an empty one clears.
	m, _ = m.Update(DOMMatchesMsg{Path: "/other.html", Ranges: nil, Current: -1})
	if len(m.domMatches) != 1 {
		t.Fatal("other-path msg must not replace the overlay")
	}
	m, _ = m.Update(DOMMatchesMsg{Path: path})
	if len(m.domMatches) != 0 {
		t.Fatal("empty delivery should clear the overlay")
	}
}
