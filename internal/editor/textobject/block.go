package textobject

import (
	"strings"

	"ike/internal/editor/buffer"
)

// block.go implements the paragraph and sentence text objects (#1193).

// Paragraph resolves "ip" / "ap": the contiguous run of non-blank lines around
// the cursor (or, on a blank line, the run of blank lines). Around extends the
// paragraph by its following blank lines — or the preceding ones when it is the
// buffer's last paragraph, matching vim. The result is linewise.
func Paragraph(b *buffer.Buffer, p buffer.Position, around bool) Result {
	blank := func(i int) bool { return strings.TrimSpace(b.Line(i)) == "" }
	last := b.LineCount() - 1
	onBlank := blank(p.Line)
	a, z := p.Line, p.Line
	for a > 0 && blank(a-1) == onBlank {
		a--
	}
	for z < last && blank(z+1) == onBlank {
		z++
	}
	if around && !onBlank {
		if z < last && blank(z+1) {
			for z < last && blank(z+1) {
				z++
			}
		} else {
			for a > 0 && blank(a-1) {
				a--
			}
		}
	}
	return Result{
		Range:    buffer.Range{Start: buffer.Position{Line: a}, End: buffer.Position{Line: z}},
		OK:       true,
		Linewise: true,
	}
}

// Sentence resolves "is" / "as" within the cursor's paragraph. A sentence ends
// at '.', '!' or '?' (optionally followed by closing quotes or brackets) before
// whitespace or the paragraph's end. Inner is the sentence text; around adds
// the following whitespace — or the preceding when the sentence is the
// paragraph's last.
func Sentence(b *buffer.Buffer, p buffer.Position, around bool) Result {
	par := Paragraph(b, p, false)
	first, lastLine := par.Range.Start.Line, par.Range.End.Line
	if strings.TrimSpace(b.Line(p.Line)) == "" {
		return Result{}
	}

	// Flatten the paragraph into one rune sequence, remembering each rune's
	// buffer position; line breaks count as single spaces.
	var runes []rune
	var pos []buffer.Position
	cursorIdx := 0
	for l := first; l <= lastLine; l++ {
		line := []rune(b.Line(l))
		for c, r := range line {
			if l == p.Line && c == p.Col {
				cursorIdx = len(runes)
			}
			runes = append(runes, r)
			pos = append(pos, buffer.Position{Line: l, Col: c})
		}
		if l == p.Line && p.Col >= len(line) {
			cursorIdx = len(runes)
		}
		if l < lastLine {
			runes = append(runes, ' ')
			pos = append(pos, buffer.Position{Line: l, Col: len(line)})
		}
	}
	if len(runes) == 0 {
		return Result{}
	}
	if cursorIdx > len(runes)-1 {
		cursorIdx = len(runes) - 1
	}

	// Sentence start indices within the flattened paragraph.
	starts := []int{0}
	for i := 0; i < len(runes); i++ {
		if runes[i] != '.' && runes[i] != '!' && runes[i] != '?' {
			continue
		}
		j := i + 1
		for j < len(runes) && strings.ContainsRune("\"')]", runes[j]) {
			j++
		}
		if j >= len(runes) || runes[j] != ' ' && runes[j] != '\t' {
			continue
		}
		for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
			j++
		}
		if j < len(runes) {
			starts = append(starts, j)
		}
	}

	// The cursor's sentence: the last start at or before the cursor.
	si := 0
	for i, s := range starts {
		if s <= cursorIdx {
			si = i
		}
	}
	begin := starts[si]
	end := len(runes) // exclusive flattened end
	if si+1 < len(starts) {
		end = starts[si+1]
	}
	trimmed := end // end without trailing whitespace
	for trimmed > begin && (runes[trimmed-1] == ' ' || runes[trimmed-1] == '\t') {
		trimmed--
	}
	if around {
		if end > trimmed || si+1 < len(starts) {
			// Include the trailing whitespace up to the next sentence.
			trimmed = end
		} else {
			// Last sentence with no trailing blanks: include the preceding run.
			for begin > 0 && (runes[begin-1] == ' ' || runes[begin-1] == '\t') {
				begin--
			}
		}
	}
	if trimmed <= begin {
		return Result{}
	}
	endPos := pos[trimmed-1]
	endPos.Col++ // half-open
	return Result{Range: buffer.Range{Start: pos[begin], End: endPos}, OK: true}
}
