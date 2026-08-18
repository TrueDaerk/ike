package editor

import "ike/internal/editor/buffer"

// dommatches.go is the DOM inspector's editor overlay (#1929): the root model
// routes the pane's selector-match ranges to every editor showing the file,
// and the view paints them like search matches (the current match underlined).
// The overlay clears with an empty delivery; stale ranges after an edit
// survive only until the inspector's docVersion sync reparses the buffer.

// DOMMatchesMsg delivers the DOM inspector's selector-match ranges for a
// document: 0-based positions with rune columns, end exclusive. Current is
// the index of the pane's current match (-1 none), which renders underlined.
type DOMMatchesMsg struct {
	Path    string
	Ranges  []buffer.Range
	Current int
}

// applyDOMMatches installs the overlay after the path gate in Update.
func (m *Model) applyDOMMatches(msg DOMMatchesMsg) {
	m.domMatches = msg.Ranges
	m.domCurrent = msg.Current
	m.bumpRender()
}

// domSpan is one overlay stretch on a single line, in rune columns.
type domSpan struct {
	start, end int
	cur        bool
}

// domLineSpans projects the match ranges onto one buffer line.
func (m Model) domLineSpans(line int) []domSpan {
	var out []domSpan
	for i, r := range m.domMatches {
		if line < r.Start.Line || line > r.End.Line {
			continue
		}
		s := domSpan{end: m.buf.RuneLen(line), cur: i == m.domCurrent}
		if line == r.Start.Line {
			s.start = r.Start.Col
		}
		if line == r.End.Line {
			s.end = r.End.Col
		}
		if s.end > s.start {
			out = append(out, s)
		}
	}
	return out
}
