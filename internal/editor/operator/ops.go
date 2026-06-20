package operator

import (
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/register"
)

// extract returns the register payload for t: the joined line text (with a
// trailing newline) for a linewise target, or the sliced span for a charwise one.
func extract(b *buffer.Buffer, t Target) register.Entry {
	if t.Linewise {
		var sb strings.Builder
		for i := t.Range.Start.Line; i <= t.Range.End.Line; i++ {
			sb.WriteString(b.Line(i))
			sb.WriteByte('\n')
		}
		return register.Entry{Text: sb.String(), Linewise: true}
	}
	return register.Entry{Text: b.Slice(t.Range)}
}

// deleteEdit builds the primitive edit that removes t from b. For a linewise
// target it removes whole lines along with the joining newline so the line count
// shrinks correctly even at the buffer's last line.
func deleteEdit(b *buffer.Buffer, t Target) buffer.Edit {
	if !t.Linewise {
		return buffer.Delete(t.Range)
	}
	a, z := t.Range.Start.Line, t.Range.End.Line
	last := b.LineCount() - 1
	switch {
	case z < last:
		return buffer.Delete(buffer.Range{Start: buffer.Position{Line: a, Col: 0}, End: buffer.Position{Line: z + 1, Col: 0}})
	case a > 0:
		// Last lines: also consume the newline before line a.
		return buffer.Delete(buffer.Range{Start: buffer.Position{Line: a - 1, Col: b.RuneLen(a - 1)}, End: b.EndOfBuffer()})
	default:
		// Whole buffer: collapse to one empty line.
		return buffer.Edit{Range: buffer.Range{Start: buffer.Position{Line: 0, Col: 0}, End: b.EndOfBuffer()}, Text: ""}
	}
}

// Yank records t into the register without mutating the buffer. It returns the
// cursor position vim leaves after a yank: the start of the span.
func Yank(b *buffer.Buffer, store *register.Store, reg rune, t Target) buffer.Position {
	store.Yank(reg, extract(b, t))
	if t.Linewise {
		return b.ClampCursor(buffer.Position{Line: t.Range.Start.Line, Col: firstNonBlankCol(b, t.Range.Start.Line)})
	}
	return b.ClampCursor(t.Range.Start)
}

// Delete removes t, records it into the register store, and returns the new
// cursor position.
func Delete(b *buffer.Buffer, rec *history.Recorder, store *register.Store, reg rune, t Target) buffer.Position {
	store.Delete(reg, extract(b, t))
	rec.Apply(deleteEdit(b, t))
	if t.Linewise {
		line := t.Range.Start.Line
		if line > b.LineCount()-1 {
			line = b.LineCount() - 1
		}
		return b.ClampCursor(buffer.Position{Line: line, Col: firstNonBlankCol(b, line)})
	}
	return b.ClampCursor(t.Range.Start)
}

// Change deletes t (recording it) and returns the position where insert mode
// should begin. A linewise change keeps an empty line in place rather than
// removing it, matching "cc".
func Change(b *buffer.Buffer, rec *history.Recorder, store *register.Store, reg rune, t Target) buffer.Position {
	store.Delete(reg, extract(b, t))
	if t.Linewise {
		a, z := t.Range.Start.Line, t.Range.End.Line
		indent := leadingBlank(b.Line(a))
		// Replace the spanned lines with a single (optionally indented) empty line.
		end := buffer.Position{Line: z, Col: b.RuneLen(z)}
		rec.Apply(buffer.Edit{Range: buffer.Range{Start: buffer.Position{Line: a, Col: 0}, End: end}, Text: indent})
		return buffer.Position{Line: a, Col: len([]rune(indent))}
	}
	rec.Apply(buffer.Delete(t.Range))
	return t.Range.Start
}

// leadingBlank returns the leading run of spaces/tabs of line (its indent).
func leadingBlank(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

// Paste inserts the register entry relative to cursor. after selects p (after)
// vs P (before); count repeats the payload. It returns the resulting cursor
// position. When moveAfter is true (gp) the cursor lands just past the pasted
// text instead of on its last rune.
func Paste(b *buffer.Buffer, rec *history.Recorder, e register.Entry, cursor buffer.Position, after bool, count int, moveAfter bool) buffer.Position {
	if count < 1 {
		count = 1
	}
	if e.Text == "" {
		return cursor
	}
	if e.Linewise {
		return pasteLines(b, rec, e, cursor, after, count, moveAfter)
	}
	return pasteChars(b, rec, e, cursor, after, count, moveAfter)
}

// pasteChars splices charwise text at or after the cursor.
func pasteChars(b *buffer.Buffer, rec *history.Recorder, e register.Entry, cursor buffer.Position, after bool, count int, moveAfter bool) buffer.Position {
	text := strings.Repeat(e.Text, count)
	at := cursor
	if after && b.RuneLen(cursor.Line) > 0 {
		at.Col = cursor.Col + 1
	}
	at = b.Clamp(at)
	end := rec.Apply(buffer.Insert(at, text))
	if moveAfter {
		return b.Clamp(end)
	}
	// Land on the last rune of the inserted text (vim leaves the cursor there).
	if end.Col > at.Col {
		end.Col--
	}
	return b.ClampCursor(end)
}

// pasteLines opens the register's whole lines above or below the cursor line.
func pasteLines(b *buffer.Buffer, rec *history.Recorder, e register.Entry, cursor buffer.Position, after bool, count int, moveAfter bool) buffer.Position {
	body := strings.TrimSuffix(e.Text, "\n")
	block := body
	for i := 1; i < count; i++ {
		block += "\n" + body
	}
	var landLine int
	if after {
		at := buffer.Position{Line: cursor.Line, Col: b.RuneLen(cursor.Line)}
		rec.Apply(buffer.Insert(at, "\n"+block))
		landLine = cursor.Line + 1
	} else {
		at := buffer.Position{Line: cursor.Line, Col: 0}
		rec.Apply(buffer.Insert(at, block+"\n"))
		landLine = cursor.Line
	}
	if moveAfter {
		after := landLine + strings.Count(block, "\n") + 1
		if after > b.LineCount()-1 {
			after = b.LineCount() - 1
		}
		return buffer.Position{Line: after, Col: 0}
	}
	return b.ClampCursor(buffer.Position{Line: landLine, Col: firstNonBlankCol(b, landLine)})
}
