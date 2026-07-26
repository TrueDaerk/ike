package operator

import (
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
)

// Transform replaces t's text with f applied to every rune (the case operators
// gu / gU / g~, #1193). It returns the cursor position vim leaves after a case
// operator: the start of the span. A transform that changes nothing applies no
// edit, so undo history stays clean.
func Transform(b *buffer.Buffer, rec *history.Recorder, t Target, f func(rune) rune) buffer.Position {
	r := t.Range
	if t.Linewise {
		r = buffer.Range{
			Start: buffer.Position{Line: r.Start.Line, Col: 0},
			End:   buffer.Position{Line: r.End.Line, Col: b.RuneLen(r.End.Line)},
		}
	}
	text := b.Slice(r)
	mapped := strings.Map(f, text)
	if mapped != text {
		rec.Apply(buffer.Edit{Range: r, Text: mapped})
	}
	if t.Linewise {
		return b.ClampCursor(buffer.Position{Line: t.Range.Start.Line, Col: firstNonBlankCol(b, t.Range.Start.Line)})
	}
	return b.ClampCursor(r.Start)
}
