package editor

import ilsp "ike/internal/lsp"

// linenotes.go is the editor side of app-fed line notes (#3120): a trailing
// virtual-text annotation a non-LSP producer hangs at the end of a line — the
// first one is the captured value of an `.http` file's `# @capture` directive
// (`= node-1:42`). The app owns the content and sets it on every view of the
// document (SetLineNotes) stamped with the document version the notes were
// computed against; the editor only stores and renders. Rendering rides the
// inlay-hint machinery the code lens already uses (lineHintsWithLens): the
// same dimmed-italic style, the same end-of-line flush, no new render path. A
// note is plain text — any masking or truncation is the producer's business,
// the editor never sees the raw value.

// lineNoteGap is the air between the last buffer cell (or code lens) and the
// note.
const lineNoteGap = "  "

// SetLineNotes replaces the note set — notes maps a 0-based buffer line to
// its text — when version is the current document version, and reports
// whether it was adopted; a set computed against another version is dropped
// (the producer recomputes for the newer one). An empty set clears regardless
// of the version — a clear is never stale.
func (m *Model) SetLineNotes(notes map[int]string, version int) bool {
	if len(notes) == 0 {
		if m.lineNotes != nil {
			m.lineNotes = nil
			m.bumpRender()
		}
		return true
	}
	if version != m.docVersion {
		return false
	}
	byLine := make(map[int]string, len(notes))
	for line, text := range notes {
		if text != "" {
			byLine[line] = text
		}
	}
	m.lineNotes = byLine
	m.bumpRender()
	return true
}

// LineNoteAt returns the note on a 0-based line, if any.
func (m Model) LineNoteAt(line int) (string, bool) {
	text, ok := m.lineNotes[line]
	return text, ok
}

// HasLineNotes reports whether the view holds any line note (app symmetry:
// a clear is only worth sending while there is something to clear).
func (m Model) HasLineNotes() bool { return len(m.lineNotes) > 0 }

// lineNoteHint is a line's note as a synthetic trailing inlay hint, anchored
// past the last buffer cell like the code lens.
func (m Model) lineNoteHint(line int) (ilsp.InlayHint, bool) {
	text, ok := m.lineNotes[line]
	if !ok {
		return ilsp.InlayHint{}, false
	}
	return ilsp.InlayHint{Line: line, Col: lensCol, Label: lineNoteGap + text}, true
}
