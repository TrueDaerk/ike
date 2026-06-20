package motion

import "ike/internal/editor/buffer"

// First moves to the first line ("gg"); with a count it targets that line
// number (1-based). Linewise.
func First(b *buffer.Buffer, from buffer.Position, count int) Result {
	line := 0
	if count > 0 {
		line = count - 1
	}
	if line > b.LineCount()-1 {
		line = b.LineCount() - 1
	}
	return Result{Pos: buffer.Position{Line: line, Col: 0}, Kind: Linewise}
}

// Last moves to the last line ("G"); with a count it targets that line number
// (1-based). Linewise.
func Last(b *buffer.Buffer, from buffer.Position, count int) Result {
	line := b.LineCount() - 1
	if count > 0 {
		line = count - 1
		if line > b.LineCount()-1 {
			line = b.LineCount() - 1
		}
	}
	return Result{Pos: buffer.Position{Line: line, Col: 0}, Kind: Linewise}
}

// ParagraphForward moves to the next blank line ("}"); exclusive.
func ParagraphForward(b *buffer.Buffer, from buffer.Position, count int) Result {
	line := from.Line
	for i := 0; i < max1(count); i++ {
		line++
		for line < b.LineCount()-1 && b.Line(line) != "" {
			line++
		}
		if line > b.LineCount()-1 {
			line = b.LineCount() - 1
		}
	}
	return Result{Pos: buffer.Position{Line: line, Col: 0}, Kind: Exclusive}
}

// ParagraphBackward moves to the previous blank line ("{"); exclusive.
func ParagraphBackward(b *buffer.Buffer, from buffer.Position, count int) Result {
	line := from.Line
	for i := 0; i < max1(count); i++ {
		line--
		for line > 0 && b.Line(line) != "" {
			line--
		}
		if line < 0 {
			line = 0
		}
	}
	return Result{Pos: buffer.Position{Line: line, Col: 0}, Kind: Exclusive}
}
