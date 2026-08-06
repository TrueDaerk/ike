package highlight

// regex.go — built-in regex mini-grammar (#1631): a pure-Go tokenizer that
// colours regular-expression syntax inside host strings known to hold regexes
// (regexp.MustCompile in Go, re.compile in Python, /…/ literals and new
// RegExp(…) in JS/TS). Context detection lives in each host's injections.scm
// under the capture name fragment.regex; overlayFragments routes those
// fragments here instead of a registered language grammar. Literal runs emit
// no span at all, so the host's own string colour shows through between
// regex tokens — the same layering every injected language uses.
//
// Captures (theme derivation in regexSources, theme.go):
//
//	regex.class       character classes: [...] bodies, shorthand \d \w \s
//	                  (and negations), \p{…} unicode classes, the . wildcard
//	regex.escape      all other \x escapes, inside and outside classes
//	regex.quantifier  * + ? {m,n} with lazy/possessive suffix
//	regex.anchor      ^ $ \b \B \A \z \Z \G
//	regex.alternation |
//	regex.group.name  the name of a (?P<name>…) / (?<name>…) / (?'name'…) group
//	regex.flags       inline flag letters in (?imsUx-…) / (?flags:…)
//	regex.comment     (?#…) comment groups
//
// Group punctuation — ( ) and opener modifiers like ?: ?= ?<! — colours by
// nesting depth with the rainbow-bracket palette (RainbowCapture), so the
// open and close of the same group always share a colour. With rainbow
// brackets disabled every group paren takes the flat "regex.group" capture.

// RegexSpans tokenizes lines as one regular expression and returns its
// highlight spans in editor rune coordinates. lines is the fragment text
// exactly as it appears in the host buffer (multi-line for raw strings);
// spans never cross lines. Malformed input degrades gracefully: unmatched
// closers and invalid {…} counts emit no span and render as literals.
func RegexSpans(lines []string) []Span {
	s := &regexScanner{}
	for _, l := range lines {
		s.lines = append(s.lines, []rune(l))
	}
	s.scan()
	return s.spans
}

// regexScanner walks the fragment rune by rune, tracking a (line, col)
// position and the open-group depth stack.
type regexScanner struct {
	lines [][]rune
	line  int
	col   int
	spans []Span
	depth int // open groups; the next opener colours with this depth
}

// eof normalizes the position past exhausted lines and reports whether the
// scanner is done. A line boundary is never itself a token.
func (s *regexScanner) eof() bool {
	for s.line < len(s.lines) && s.col >= len(s.lines[s.line]) {
		s.line++
		s.col = 0
	}
	return s.line >= len(s.lines)
}

// peek returns the rune at the current position; eof must be false.
func (s *regexScanner) peek() rune { return s.lines[s.line][s.col] }

// peekAt returns the rune n positions ahead on the current line, or 0 when
// the line ends first — lookahead never crosses a line boundary.
func (s *regexScanner) peekAt(n int) rune {
	if s.col+n >= len(s.lines[s.line]) {
		return 0
	}
	return s.lines[s.line][s.col+n]
}

// emit appends one same-line span.
func (s *regexScanner) emit(line, start, end int, capture string) {
	if end > start {
		s.spans = append(s.spans, Span{Line: line, StartCol: start, EndCol: end, Capture: capture})
	}
}

// emitRange appends a possibly multi-line range as one span per line.
func (s *regexScanner) emitRange(startLine, startCol, endLine, endCol int, capture string) {
	if startLine == endLine {
		s.emit(startLine, startCol, endCol, capture)
		return
	}
	s.emit(startLine, startCol, len(s.lines[startLine]), capture)
	for l := startLine + 1; l < endLine; l++ {
		s.emit(l, 0, len(s.lines[l]), capture)
	}
	s.emit(endLine, 0, endCol, capture)
}

// groupCapture is the capture for group punctuation at nesting depth d:
// the rainbow palette when enabled, a flat colour otherwise.
func groupCapture(d int) string {
	if RainbowEnabled() {
		return RainbowCapture(d)
	}
	return "regex.group"
}

func (s *regexScanner) scan() {
	for !s.eof() {
		switch s.peek() {
		case '\\':
			s.scanEscape()
		case '[':
			s.scanClass()
		case '(':
			s.scanGroupOpen()
		case ')':
			if s.depth > 0 {
				s.depth--
				s.emit(s.line, s.col, s.col+1, groupCapture(s.depth))
			}
			s.col++
		case '*', '+', '?':
			line, start := s.line, s.col
			s.col++
			// Lazy/possessive suffix merges into the quantifier token.
			if s.col < len(s.lines[line]) && (s.lines[line][s.col] == '?' || s.lines[line][s.col] == '+') {
				s.col++
			}
			s.emit(line, start, s.col, "regex.quantifier")
		case '{':
			s.scanCount()
		case '|':
			s.emit(s.line, s.col, s.col+1, "regex.alternation")
			s.col++
		case '^', '$':
			s.emit(s.line, s.col, s.col+1, "regex.anchor")
			s.col++
		case '.':
			s.emit(s.line, s.col, s.col+1, "regex.class")
			s.col++
		default:
			s.col++
		}
	}
}

// peekOk reports whether the current rune equals r without eof handling —
// callers already know the position is valid on this line.
func (s *regexScanner) peekOk(r rune) bool {
	return s.col < len(s.lines[s.line]) && s.lines[s.line][s.col] == r
}

// escapeCapture classifies a backslash escape: shorthand classes and unicode
// properties read as classes, positional escapes as anchors, everything else
// as a plain escape.
func escapeCapture(r rune) string {
	switch r {
	case 'd', 'D', 'w', 'W', 's', 'S', 'p', 'P':
		return "regex.class"
	case 'b', 'B', 'A', 'z', 'Z', 'G':
		return "regex.anchor"
	}
	return "regex.escape"
}

// scanEscape consumes a backslash escape: \ plus one rune, plus a {…} body
// for \p{L} / \x{10FFFF}-style forms.
func (s *regexScanner) scanEscape() {
	line, start := s.line, s.col
	s.col++ // backslash
	if s.col >= len(s.lines[line]) {
		// Trailing backslash at end of line: colour it alone.
		s.emit(line, start, s.col, "regex.escape")
		return
	}
	r := s.peek()
	s.col++
	if (r == 'p' || r == 'P' || r == 'x' || r == 'u') && s.col < len(s.lines[line]) && s.lines[line][s.col] == '{' {
		for s.col < len(s.lines[line]) {
			c := s.lines[line][s.col]
			s.col++
			if c == '}' {
				break
			}
		}
	}
	// \b inside a class is backspace, not an anchor; scanClass reclassifies
	// anchor spans it collects, so no special case is needed here.
	s.emit(line, start, s.col, escapeCapture(r))
}

// scanClass consumes a [...] character class. Escapes inside emit their own
// spans first, then the whole class emits regex.class — span order makes the
// escapes win where they overlap (CaptureAt is first-covering-wins). A ]
// directly after [ or [^ is a literal member, per every regex flavor.
func (s *regexScanner) scanClass() {
	startLine, startCol := s.line, s.col
	s.col++ // [
	if !s.eof() && s.peekOk('^') {
		s.col++
	}
	if !s.eof() && s.peekOk(']') {
		s.col++
	}
	var inner []Span
	for !s.eof() {
		switch s.peek() {
		case ']':
			s.col++
			s.spans = append(s.spans, inner...)
			s.emitRange(startLine, startCol, s.line, s.col, "regex.class")
			return
		case '\\':
			from := len(s.spans)
			s.scanEscape()
			// Reclassify anchors: \b is backspace inside a class.
			for i := from; i < len(s.spans); i++ {
				if s.spans[i].Capture == "regex.anchor" {
					s.spans[i].Capture = "regex.escape"
				}
			}
			inner = append(inner, s.spans[from:]...)
			s.spans = s.spans[:from]
		default:
			s.col++
		}
	}
	// Unterminated class: colour what we saw to the end of the fragment.
	s.spans = append(s.spans, inner...)
	last := len(s.lines) - 1
	s.emitRange(startLine, startCol, last, len(s.lines[last]), "regex.class")
}

// scanCount consumes a {m} / {m,} / {m,n} / {,n} counted quantifier with an
// optional lazy ? suffix. Anything else brace-shaped is a literal and emits
// no span.
func (s *regexScanner) scanCount() {
	line, start := s.line, s.col
	i, n := start+1, len(s.lines[line])
	digits := func() int {
		d := 0
		for i < n && s.lines[line][i] >= '0' && s.lines[line][i] <= '9' {
			i++
			d++
		}
		return d
	}
	lead := digits()
	comma := i < n && s.lines[line][i] == ','
	if comma {
		i++
		digits()
	}
	if i >= n || s.lines[line][i] != '}' || (lead == 0 && !comma) {
		s.col++ // literal {
		return
	}
	i++
	if i < n && s.lines[line][i] == '?' {
		i++
	}
	s.emit(line, start, i, "regex.quantifier")
	s.col = i
}

// scanGroupOpen consumes a ( opener with its ?-modifier: plain and
// non-capturing groups, lookarounds, atomic groups, named groups (whose name
// gets its own capture), inline flags and (?#…) comments. All opener
// punctuation shares the rainbow colour of its depth so it visually pairs
// with the closing paren.
func (s *regexScanner) scanGroupOpen() {
	line, start := s.line, s.col
	s.col++ // (
	open := func(punctEnd int) {
		s.emit(line, start, punctEnd, groupCapture(s.depth))
		s.depth++
	}
	if s.col >= len(s.lines[line]) || s.lines[line][s.col] != '?' {
		open(s.col)
		return
	}
	s.col++ // ?
	if s.col >= len(s.lines[line]) {
		open(s.col)
		return
	}
	switch r := s.peek(); {
	case r == '#':
		// (?#comment) — self-contained, never pushes a group.
		for s.col < len(s.lines[line]) {
			c := s.lines[line][s.col]
			s.col++
			if c == ')' {
				break
			}
		}
		s.emit(line, start, s.col, "regex.comment")
	case r == ':' || r == '=' || r == '!' || r == '>':
		s.col++
		open(s.col)
	case r == '<' && (s.peekAt(1) == '=' || s.peekAt(1) == '!'):
		s.col += 2
		open(s.col)
	case r == '<':
		s.col++
		open(s.col)
		s.scanGroupName(line, '>')
	case r == 'P' && s.peekAt(1) == '<':
		s.col += 2
		open(s.col)
		s.scanGroupName(line, '>')
	case r == 'P' && s.peekAt(1) == '=':
		s.col += 2
		open(s.col)
		s.scanGroupName(line, 0)
	case r == '\'':
		s.col++
		open(s.col)
		s.scanGroupName(line, '\'')
	default:
		// Inline flags: (?imsUx-…) or (?flags:…).
		flagStart := s.col
		for s.col < len(s.lines[line]) {
			c := s.lines[line][s.col]
			if !(c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
				break
			}
			s.col++
		}
		open(flagStart)
		s.emit(line, flagStart, s.col, "regex.flags")
		if s.col < len(s.lines[line]) && s.lines[line][s.col] == ':' {
			s.emit(line, s.col, s.col+1, groupCapture(s.depth-1))
			s.col++
		}
	}
}

// scanGroupName consumes a group name up to the closing delimiter (0: no
// delimiter, the surrounding group's ) ends the name). The name colours as
// regex.group.name, the delimiter with the group's rainbow colour.
func (s *regexScanner) scanGroupName(line int, closer rune) {
	nameStart := s.col
	for s.col < len(s.lines[line]) {
		c := s.lines[line][s.col]
		if c == closer || c == ')' {
			break
		}
		s.col++
	}
	s.emit(line, nameStart, s.col, "regex.group.name")
	if closer != 0 && s.col < len(s.lines[line]) && s.lines[line][s.col] == closer {
		s.emit(line, s.col, s.col+1, groupCapture(s.depth-1))
		s.col++
	}
}
