package buffer

import "strings"

// Edit is the single primitive mutation: replace the text in Range with Text.
// Insert is a replace of an empty range; Delete is a replace with "". Text may
// contain newlines. Expressing every mutation as one shape makes undo trivial —
// the inverse of an Edit is another Edit — and keeps all line-splicing in one
// place.
type Edit struct {
	Range Range
	Text  string
}

// Insert builds an Edit that inserts text at pos.
func Insert(pos Position, text string) Edit {
	return Edit{Range: Range{Start: pos, End: pos}, Text: text}
}

// Delete builds an Edit that removes the text in r.
func Delete(r Range) Edit { return Edit{Range: r, Text: ""} }

// Apply performs e against the buffer and returns the inverse edit (which undoes
// e exactly) together with the end position of the inserted text. Positions in e
// are clamped to the buffer first, so callers need not pre-validate.
func (b *Buffer) Apply(e Edit) (inverse Edit, end Position) {
	r := Range{Start: b.Clamp(e.Range.Start), End: b.Clamp(e.Range.End)}
	if r.End.Before(r.Start) {
		r.Start, r.End = r.End, r.Start
	}
	old := b.Slice(r)

	startLine := b.lines[r.Start.Line]
	endLine := b.lines[r.End.Line]
	prefix := startLine[:byteOffset(startLine, r.Start.Col)]
	suffix := endLine[byteOffset(endLine, r.End.Col):]

	merged := prefix + e.Text + suffix
	newLines := strings.Split(merged, "\n")

	// Splice newLines in place of lines [r.Start.Line .. r.End.Line].
	rebuilt := make([]string, 0, len(b.lines)-(r.End.Line-r.Start.Line)-1+len(newLines))
	rebuilt = append(rebuilt, b.lines[:r.Start.Line]...)
	rebuilt = append(rebuilt, newLines...)
	rebuilt = append(rebuilt, b.lines[r.End.Line+1:]...)
	b.setLines(rebuilt)

	// End position of the inserted text.
	nl := strings.Count(e.Text, "\n")
	if nl == 0 {
		end = Position{Line: r.Start.Line, Col: r.Start.Col + runeLen(e.Text)}
	} else {
		lastSeg := e.Text[strings.LastIndexByte(e.Text, '\n')+1:]
		end = Position{Line: r.Start.Line + nl, Col: runeLen(lastSeg)}
	}

	inverse = Edit{Range: Range{Start: r.Start, End: end}, Text: old}
	return inverse, end
}
