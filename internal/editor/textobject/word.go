package textobject

import "ike/internal/editor/buffer"

type wclass int

const (
	wBlank wclass = iota
	wWord
	wPunct
)

func wordClass(r rune, big bool) wclass {
	switch {
	case r == ' ' || r == '\t':
		return wBlank
	case big:
		return wPunct
	case r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127:
		return wWord
	default:
		return wPunct
	}
}

// Word resolves "iw"/"aw" (and the "iW"/"aW" big variants) on the cursor's line.
// Inner covers the run of like-classed runes under the cursor; around also
// swallows the trailing whitespace, or the leading whitespace when there is no
// trailing run (matching vim).
func Word(b *buffer.Buffer, p buffer.Position, around, big bool) Result {
	line := []rune(b.Line(p.Line))
	if len(line) == 0 {
		return Result{}
	}
	col := p.Col
	if col >= len(line) {
		col = len(line) - 1
	}
	c := wordClass(line[col], big)
	start, end := col, col
	for start > 0 && wordClass(line[start-1], big) == c {
		start--
	}
	for end < len(line) && wordClass(line[end], big) == c {
		end++
	}
	if around {
		ws := end
		for ws < len(line) && wordClass(line[ws], big) == wBlank {
			ws++
		}
		if ws > end {
			end = ws
		} else {
			for start > 0 && wordClass(line[start-1], big) == wBlank {
				start--
			}
		}
	}
	return Result{
		Range: buffer.Range{
			Start: buffer.Position{Line: p.Line, Col: start},
			End:   buffer.Position{Line: p.Line, Col: end},
		},
		OK: true,
	}
}
