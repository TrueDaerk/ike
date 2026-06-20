package textobject

import "ike/internal/editor/buffer"

// Pairs maps an opening delimiter to its closing one for bracket text objects.
var Pairs = map[rune]rune{'(': ')', '[': ']', '{': '}', '<': '>'}

// CloseFor returns the closing delimiter for an opening one (and vice versa),
// plus whether ch is a known bracket. It lets the editor accept either member of
// a pair (e.g. both "i(" and "i)").
func CloseFor(ch rune) (open, close rune, ok bool) {
	if c, isOpen := Pairs[ch]; isOpen {
		return ch, c, true
	}
	for o, c := range Pairs {
		if c == ch {
			return o, c, true
		}
	}
	return 0, 0, false
}

// Pair resolves a bracket text object ("i(" / "a(") enclosing the cursor,
// honouring nesting and spanning lines. Inner is the span between the brackets;
// around includes them. ok is false when the cursor is not inside a matching
// pair.
func Pair(b *buffer.Buffer, p buffer.Position, open, close rune, around bool) Result {
	openPos, ok := findOpen(b, p, open, close)
	if !ok {
		return Result{}
	}
	closePos, ok := findClose(b, openPos, open, close)
	if !ok {
		return Result{}
	}
	if around {
		end, _ := next(b, closePos) // include the closing bracket
		return Result{Range: buffer.Range{Start: openPos, End: end}, OK: true}
	}
	inner, _ := next(b, openPos)
	return Result{Range: buffer.Range{Start: inner, End: closePos}, OK: true}
}

// findOpen scans backward from p for the unmatched opening bracket. If the
// cursor sits on the opening bracket, that is the match.
func findOpen(b *buffer.Buffer, p buffer.Position, open, close rune) (buffer.Position, bool) {
	if runeAt(b, p) == open {
		return p, true
	}
	depth := 0
	cur := p
	for {
		q, ok := prev(b, cur)
		if !ok {
			return buffer.Position{}, false
		}
		cur = q
		switch runeAt(b, cur) {
		case close:
			depth++
		case open:
			if depth == 0 {
				return cur, true
			}
			depth--
		}
	}
}

// findClose scans forward from the opening bracket for its match.
func findClose(b *buffer.Buffer, openPos buffer.Position, open, close rune) (buffer.Position, bool) {
	depth := 0
	cur := openPos
	for {
		switch runeAt(b, cur) {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return cur, true
			}
		}
		q, ok := next(b, cur)
		if !ok {
			return buffer.Position{}, false
		}
		cur = q
	}
}

// Quote resolves a quote text object ("i\"" / "a\"") on the cursor's line. It
// pairs quotes left to right and picks the pair enclosing the cursor, else the
// next pair after it. Around includes the quotes; inner is the text between.
func Quote(b *buffer.Buffer, p buffer.Position, q rune, around bool) Result {
	line := []rune(b.Line(p.Line))
	var quotes []int
	for i, r := range line {
		if r == q {
			quotes = append(quotes, i)
		}
	}
	for i := 0; i+1 < len(quotes); i += 2 {
		o, c := quotes[i], quotes[i+1]
		if p.Col <= c {
			if around {
				return Result{Range: rng(p.Line, o, c+1), OK: true}
			}
			return Result{Range: rng(p.Line, o+1, c), OK: true}
		}
	}
	return Result{}
}

func rng(line, start, end int) buffer.Range {
	return buffer.Range{
		Start: buffer.Position{Line: line, Col: start},
		End:   buffer.Position{Line: line, Col: end},
	}
}
