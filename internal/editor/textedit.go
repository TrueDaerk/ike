package editor

import (
	"sort"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
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
	m.hist.Push(history.Change{
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
