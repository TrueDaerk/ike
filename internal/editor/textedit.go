package editor

import (
	"sort"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/highlight"
)

// textedit.go applies LSP-shaped text edits to the buffer (Roadmap 0100, #7:
// document/range formatting). Like replace.go it batches every edit into one
// history change — a single undo reverts the whole formatting pass — and
// applies bottom-up so earlier positions stay valid while later ones shift.

// TextEdit is one range rewrite in 0-based editor rune coordinates: the
// [Start, End) span becomes Text (which may contain newlines). It mirrors the
// LSP TextEdit after position conversion; nothing LSP-typed leaks in here.
type TextEdit struct {
	StartLine, StartCol int
	EndLine, EndCol     int
	Text                string
}

// ApplyTextEdits applies the edits as one history change and returns how many
// were applied. Edits are sorted bottom-up (later start positions first), the
// order LSP formatting results are safe to apply in; ranges are clamped by the
// buffer. An empty slice is a no-op returning 0.
func (m *Model) ApplyTextEdits(edits []TextEdit) int {
	if len(edits) == 0 {
		return 0
	}
	sorted := make([]TextEdit, len(edits))
	copy(sorted, edits)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].StartLine != sorted[j].StartLine {
			return sorted[i].StartLine > sorted[j].StartLine
		}
		return sorted[i].StartCol > sorted[j].StartCol
	})

	cursorBefore := m.cursor
	var fwd, inv []buffer.Edit
	for _, e := range sorted {
		be := buffer.Edit{
			Range: buffer.Range{
				Start: buffer.Position{Line: e.StartLine, Col: e.StartCol},
				End:   buffer.Position{Line: e.EndLine, Col: e.EndCol},
			},
			Text: e.Text,
		}
		inverse, _ := m.buf.Apply(be)
		fwd = append(fwd, be)
		inv = append(inv, inverse)
	}
	m.cursor = m.buf.ClampCursor(m.cursor)
	m.pushChange(history.Change{
		Forwards:     fwd,
		Inverses:     inv,
		CursorBefore: cursorBefore,
		CursorAfter:  m.cursor,
	})
	m.dirty = true
	m.scroll()
	m.emit(EventChange)
	return len(sorted)
}

// ReparseEdits invalidates the decoration caches built from the pre-edit text
// and schedules a fresh parse. App-driven edits (formatting results, a
// local-history restore) call ApplyTextEdits outside the editor's Update loop,
// so maybeReparse never observes the docVersion bump — without this the old
// highlight and conceal spans keep rendering over the new text until the next
// keystroke (#1683). Mirrors what applySync does for the other views of a
// shared document.
func (m *Model) ReparseEdits() tea.Cmd {
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.concealExt = nil
	m.decodes = nil
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	return m.parseCmd()
}
