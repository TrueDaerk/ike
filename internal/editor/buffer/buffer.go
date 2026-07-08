package buffer

import "strings"

// Buffer is the text storage: one string per logical line, never empty (an
// empty document is a single "" line). Line addressing is O(1); within a line
// columns are rune indices. The backing store is intentionally a plain slice —
// the API is written so a gap buffer / piece table can replace it later without
// touching callers.
type Buffer struct {
	lines []string
}

// New returns a buffer over the given lines. A nil or empty slice yields a
// single empty line. The slice is copied so the caller cannot alias the store.
func New(lines []string) *Buffer {
	if len(lines) == 0 {
		return &Buffer{lines: []string{""}}
	}
	cp := make([]string, len(lines))
	copy(cp, lines)
	return &Buffer{lines: cp}
}

// FromString splits s on newlines into a buffer, normalizing CRLF to LF. A
// single trailing newline is treated as a line terminator, not a spurious empty
// final line.
func FromString(s string) *Buffer {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return New(lines)
}

// LineCount returns the number of lines (always >= 1).
func (b *Buffer) LineCount() int { return len(b.lines) }

// Line returns line i. Out-of-range indices return "".
func (b *Buffer) Line(i int) string {
	if i < 0 || i >= len(b.lines) {
		return ""
	}
	return b.lines[i]
}

// Lines returns a copy of every line.
func (b *Buffer) Lines() []string {
	cp := make([]string, len(b.lines))
	copy(cp, b.lines)
	return cp
}

// RuneLen returns the rune length of line i (0 for out-of-range lines).
func (b *Buffer) RuneLen(i int) int { return runeLen(b.Line(i)) }

// String renders the buffer as newline-joined text without a trailing newline;
// the writer (excmd/save) adds the final newline policy.
func (b *Buffer) String() string { return strings.Join(b.lines, "\n") }

// Clamp returns p moved onto a valid position: line into [0, LineCount-1] and
// col into [0, RuneLen(line)]. Col may equal the line length (one past end).
func (b *Buffer) Clamp(p Position) Position {
	if p.Line < 0 {
		p.Line = 0
	}
	if p.Line >= len(b.lines) {
		p.Line = len(b.lines) - 1
	}
	max := b.RuneLen(p.Line)
	if p.Col < 0 {
		p.Col = 0
	}
	if p.Col > max {
		p.Col = max
	}
	return p
}

// ClampCursor is like Clamp but keeps the column on the last rune of a non-empty
// line (normal-mode semantics, where the cursor sits on a character rather than
// one past the end).
func (b *Buffer) ClampCursor(p Position) Position {
	p = b.Clamp(p)
	if max := b.RuneLen(p.Line) - 1; max >= 0 && p.Col > max {
		p.Col = max
	}
	return p
}

// EndOfBuffer returns the position one past the last rune of the last line.
func (b *Buffer) EndOfBuffer() Position {
	last := len(b.lines) - 1
	return Position{Line: last, Col: b.RuneLen(last)}
}

// Slice returns the text covered by r, including embedded newlines for a
// multi-line range. An empty range yields "".
func (b *Buffer) Slice(r Range) string {
	r = Range{Start: b.Clamp(r.Start), End: b.Clamp(r.End)}
	if r.Empty() || r.End.Before(r.Start) {
		return ""
	}
	if r.Start.Line == r.End.Line {
		line := b.lines[r.Start.Line]
		return line[byteOffset(line, r.Start.Col):byteOffset(line, r.End.Col)]
	}
	var sb strings.Builder
	first := b.lines[r.Start.Line]
	sb.WriteString(first[byteOffset(first, r.Start.Col):])
	sb.WriteByte('\n')
	for i := r.Start.Line + 1; i < r.End.Line; i++ {
		sb.WriteString(b.lines[i])
		sb.WriteByte('\n')
	}
	last := b.lines[r.End.Line]
	sb.WriteString(last[:byteOffset(last, r.End.Col)])
	return sb.String()
}

// ReplaceAll swaps the whole content for s (same normalization as FromString),
// mutating the buffer in place. Callers that share the buffer across views
// (Roadmap 0140 reload, shared documents) use this instead of allocating a new
// Buffer, so every alias sees the new text.
func (b *Buffer) ReplaceAll(s string) {
	b.lines = FromString(s).lines
}

// setLines replaces the entire backing store; helper for edit application.
func (b *Buffer) setLines(lines []string) {
	if len(lines) == 0 {
		lines = []string{""}
	}
	b.lines = lines
}
