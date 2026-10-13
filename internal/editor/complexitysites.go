package editor

// complexitysites.go is the editor side of the per-site complexity marks
// (Roadmap 0551, #2947): Sonar style, every statement that contributes to a
// function's cognitive complexity carries a trailing `+1`, `+3 (nesting 2)`
// or `+1 ||` — the second annotation store beside the function-header hints
// (complexityhints.go), mirrored one to one. The app derives both from the
// same measurement and pushes ComplexitySiteMarksMsg stamped with the
// document version; a push whose version moved is dropped. The marks render
// in the header hint's slot of the end-of-line flush with its gap, style and
// clipping (whole or not at all, never over buffer text), and a header hint
// on the same line wins. The editor does not gate them on a setting: the
// app decides what to push (editor.marks.complexity_sites, the panel's
// one-shot `m`), and editor.marks.complexity does not hide them.

// ComplexitySiteMark is one line's site annotation: Line is the 0-based
// buffer line, Text the rendered increments, Warn whether an increment on
// the line costs 3 or more (the nesting penalty worth seeing).
type ComplexitySiteMark struct {
	Line int
	Text string
	Warn bool
}

// ComplexitySiteMarksMsg replaces a document's site marks. Version is the
// DocVersion the marks were measured at; an editor at another version drops
// the message. Nil Marks clear.
type ComplexitySiteMarksMsg struct {
	Path    string
	Marks   []ComplexitySiteMark
	Version int
}

// SetComplexitySiteMarks replaces the mark set when docVersion is the
// current document version and reports whether it was adopted. Nil clears
// regardless of the version — a clear is never stale.
func (m *Model) SetComplexitySiteMarks(marks []ComplexitySiteMark, docVersion int) bool {
	if len(marks) == 0 {
		if m.cxSites != nil {
			m.cxSites = nil
			m.bumpRender()
		}
		return true
	}
	if docVersion != m.docVersion {
		return false
	}
	byLine := make(map[int]ComplexitySiteMark, len(marks))
	for _, s := range marks {
		if s.Text != "" {
			byLine[s.Line] = s
		}
	}
	m.cxSites = byLine
	m.bumpRender()
	return true
}

// HasComplexitySiteMarks reports whether the buffer holds any site marks.
func (m Model) HasComplexitySiteMarks() bool { return len(m.cxSites) > 0 }

// ComplexitySiteMarkAt returns the site mark on a 0-based line, if any.
func (m Model) ComplexitySiteMarkAt(line int) (ComplexitySiteMark, bool) {
	s, ok := m.cxSites[line]
	return s, ok
}

// complexityTrailer renders a line's complexity annotation for the
// end-of-line flush: the header hint when the line shows one, else the
// site mark, under the same whole-or-nothing fit rule.
func (m Model) complexityTrailer(line, room int) (string, int) {
	if h, ok := m.ComplexityHintAt(line); ok {
		return m.complexityHintText(h, room)
	}
	s, ok := m.cxSites[line]
	if !ok {
		return "", 0
	}
	return m.complexityTrailerText(s.Text, s.Warn, room)
}
