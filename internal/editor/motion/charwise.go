package motion

import "ike/internal/editor/buffer"

// Left moves count columns left within the line, stopping at column 0.
func Left(b *buffer.Buffer, from buffer.Position, count int) Result {
	col := from.Col - max1(count)
	if col < 0 {
		col = 0
	}
	return Result{Pos: buffer.Position{Line: from.Line, Col: col}, Kind: Exclusive}
}

// Right moves count columns right within the line, stopping one past the last
// rune (the cursor's normal-mode clamp pulls it back onto a character).
func Right(b *buffer.Buffer, from buffer.Position, count int) Result {
	col := from.Col + max1(count)
	if m := b.RuneLen(from.Line); col > m {
		col = m
	}
	return Result{Pos: buffer.Position{Line: from.Line, Col: col}, Kind: Exclusive}
}

// Down moves count lines down (linewise), keeping the column where possible.
func Down(b *buffer.Buffer, from buffer.Position, count int) Result {
	line := from.Line + max1(count)
	if line > b.LineCount()-1 {
		line = b.LineCount() - 1
	}
	return Result{Pos: buffer.Position{Line: line, Col: from.Col}, Kind: Linewise}
}

// Up moves count lines up (linewise).
func Up(b *buffer.Buffer, from buffer.Position, count int) Result {
	line := from.Line - max1(count)
	if line < 0 {
		line = 0
	}
	return Result{Pos: buffer.Position{Line: line, Col: from.Col}, Kind: Linewise}
}

// LineStart moves to column 0 ("0").
func LineStart(b *buffer.Buffer, from buffer.Position, count int) Result {
	return Result{Pos: buffer.Position{Line: from.Line, Col: 0}, Kind: Exclusive}
}

// FirstNonBlank moves to the first non-blank rune of the line ("^").
func FirstNonBlank(b *buffer.Buffer, from buffer.Position, count int) Result {
	r := []rune(b.Line(from.Line))
	col := 0
	for col < len(r) && (r[col] == ' ' || r[col] == '\t') {
		col++
	}
	if col >= len(r) && len(r) > 0 {
		col = len(r) - 1
	}
	return Result{Pos: buffer.Position{Line: from.Line, Col: col}, Kind: Exclusive}
}

// LineEnd moves to the last rune of the line ("$"); inclusive so an operator
// reaches the end of the line. With a count it targets the end of a lower line.
func LineEnd(b *buffer.Buffer, from buffer.Position, count int) Result {
	line := from.Line + max1(count) - 1
	if line > b.LineCount()-1 {
		line = b.LineCount() - 1
	}
	col := b.RuneLen(line) - 1
	if col < 0 {
		col = 0
	}
	return Result{Pos: buffer.Position{Line: line, Col: col}, Kind: Inclusive}
}

// SmartHome implements "smart Home" (JetBrains/VS Code): it targets the
// first non-blank column, or column 0 if the cursor is already there, so
// repeated presses toggle between the two. A line with no non-blank runes
// has no separate content column, so it always targets 0. Distinct from the
// vim "0"/"^" motions, which keep their own conventions.
func SmartHome(b *buffer.Buffer, from buffer.Position, count int) Result {
	col := firstNonBlankCol(b, from.Line)
	if from.Col == col {
		col = 0
	}
	return Result{Pos: buffer.Position{Line: from.Line, Col: col}, Kind: Exclusive}
}

// SmartEnd implements "smart End": it targets the column just past the last
// non-blank rune, or the true end of line if the cursor is already there, so
// repeated presses toggle between the two. A line with no non-blank runes has
// no separate content column, so it always targets the true end. Distinct
// from the vim "$" motion, which keeps its own convention.
func SmartEnd(b *buffer.Buffer, from buffer.Position, count int) Result {
	col := lastNonBlankCol(b, from.Line)
	if from.Col == col {
		col = b.RuneLen(from.Line)
	}
	return Result{Pos: buffer.Position{Line: from.Line, Col: col}, Kind: Exclusive}
}

func firstNonBlankCol(b *buffer.Buffer, line int) int {
	r := []rune(b.Line(line))
	col := 0
	for col < len(r) && (r[col] == ' ' || r[col] == '\t') {
		col++
	}
	if col >= len(r) {
		return 0
	}
	return col
}

func lastNonBlankCol(b *buffer.Buffer, line int) int {
	r := []rune(b.Line(line))
	col := len(r)
	for col > 0 && (r[col-1] == ' ' || r[col-1] == '\t') {
		col--
	}
	if col == 0 {
		return b.RuneLen(line)
	}
	return col
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
