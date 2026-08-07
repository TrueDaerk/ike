package docpath

import "strings"

// json.go is the container-stack scanner shared by JSON buffers and YAML flow
// collections (`{a: 1}`, `[1, 2]`) — the two are the same grammar, and YAML's
// unquoted flow keys are simply one more scalar shape the token loop accepts.
//
// The scan reads the buffer only up to the caret, one line at a time, so it
// never needs the document to be complete or even valid below the caret.

// jframe is one open container. For an object, key names it once a string has
// been read in key position; afterColon marks that the following scalar is a
// value, not the next key. For an array, index counts the commas at this
// level.
type jframe struct {
	seq        bool
	index      int
	key        string
	haveKey    bool
	afterColon bool
}

// jscan is the scanner state. It survives across lines: a container opened on
// one line stays open, and a `/* */` comment may span lines. done marks that
// the caret was reached and no further input matters.
type jscan struct {
	stack   []jframe
	inBlock bool
	done    bool
}

// scanJSON returns the path to the caret at (line, col) in a JSON buffer.
func scanJSON(src Source, line, col int) []Step {
	s := &jscan{}
	n := src.LineCount()
	if n == 0 {
		return nil
	}
	if line >= n {
		// Past the last line is the end of the document, not its first cell.
		line, col = n-1, len([]rune(src.Line(n-1)))
	}
	for ln := 0; ln <= line; ln++ {
		stop := -1
		if ln == line {
			stop = col
		}
		s.feed([]rune(src.Line(ln)), 0, stop)
		if s.done {
			break
		}
	}
	return s.steps()
}

// feed consumes the runes of one line from column from. stop is the caret
// column on the caret's line and -1 on every line above it: the loop reads
// whole tokens, so a caret *inside* a key still sees that key applied, and
// only a token starting past the caret ends the scan.
func (s *jscan) feed(r []rune, from, stop int) {
	i := from
	for i < len(r) {
		if s.inBlock {
			j := indexFrom(r, i, "*/")
			if j < 0 {
				return
			}
			i, s.inBlock = j+2, false
			continue
		}
		c := r[i]
		if c == ' ' || c == '\t' {
			i++
			continue
		}
		if stop >= 0 && i > stop {
			s.done = true
			return
		}
		switch {
		// Comments: `//` and `/* */` are JSONC's, `#` is YAML's inside a flow
		// collection. None of them can open a container, so all three simply
		// end the line (or, for a block comment, span until it closes).
		case c == '/' && i+1 < len(r) && r[i+1] == '/', c == '#':
			return
		case c == '/' && i+1 < len(r) && r[i+1] == '*':
			s.inBlock = true
			i += 2
		case c == '{' || c == '[':
			s.stack = append(s.stack, jframe{seq: c == '['})
			i++
		case c == '}' || c == ']':
			// A caret *on* a closer is still inside the container it closes,
			// so the frame is reported before it is popped.
			if stop >= 0 && i >= stop {
				s.done = true
				return
			}
			if len(s.stack) > 0 {
				s.stack = s.stack[:len(s.stack)-1]
			}
			i++
		case c == ':':
			if f := s.top(); f != nil {
				f.afterColon = true
			}
			i++
		case c == ',':
			if f := s.top(); f != nil {
				if f.seq {
					f.index++
				} else {
					f.key, f.haveKey, f.afterColon = "", false, false
				}
			}
			i++
		case c == '"' || c == '\'':
			text, next := readQuoted(r, i)
			s.scalar(text)
			i = next
		default:
			text, next := readPlain(r, i)
			if next == i {
				i++ // a rune no token starts with (a stray `&`, a tag): skip it
				continue
			}
			s.scalar(text)
			i = next
		}
	}
}

// top returns the innermost open frame, or nil at the document root.
func (s *jscan) top() *jframe {
	if len(s.stack) == 0 {
		return nil
	}
	return &s.stack[len(s.stack)-1]
}

// scalar applies a scalar token: in an object frame that is still waiting for
// its key, the token *is* the key. Everywhere else it is a value and changes
// nothing about the path.
func (s *jscan) scalar(text string) {
	f := s.top()
	if f == nil || f.seq || f.afterColon || f.haveKey {
		return
	}
	f.key, f.haveKey = text, true
}

// steps renders the open frames as the path. An object frame with no key yet
// (the caret sits right after its `{`) contributes nothing — the enclosing
// path is still the honest answer.
func (s *jscan) steps() []Step {
	out := make([]Step, 0, len(s.stack))
	for _, f := range s.stack {
		switch {
		case f.seq:
			out = append(out, Step{Seq: true, Index: f.index})
		case f.haveKey:
			out = append(out, Step{Key: f.key})
		}
	}
	return out
}

// readQuoted reads the quoted scalar starting at i and returns its text plus
// the column after the closing quote. A quote left open at the end of the line
// ends there: a scan that swallowed the rest of the buffer would lose the path
// for every line below a stray quote.
func readQuoted(r []rune, i int) (string, int) {
	q := r[i]
	var b strings.Builder
	for j := i + 1; j < len(r); j++ {
		switch {
		case r[j] == '\\' && q == '"' && j+1 < len(r):
			j++
			b.WriteRune(unescape(r[j]))
		case r[j] == q:
			return b.String(), j + 1
		default:
			b.WriteRune(r[j])
		}
	}
	return b.String(), len(r)
}

// unescape maps the JSON escapes that can appear in a key to their character;
// anything else (including \uXXXX) keeps its literal rune, which is what the
// document shows.
func unescape(c rune) rune {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	}
	return c
}

// readPlain reads an unquoted scalar — a number, `true`, or a YAML flow key —
// up to the next structural rune, and returns it trimmed. Spaces are allowed
// inside it, so a YAML flow key like `{my key: 1}` stays one token.
func readPlain(r []rune, i int) (string, int) {
	j := i
	for j < len(r) && !strings.ContainsRune(",:{}[]", r[j]) {
		j++
	}
	text := strings.TrimSpace(string(r[i:j]))
	if text == "" {
		return "", i
	}
	return text, j
}

// indexFrom is strings.Index over a rune slice, starting at from.
func indexFrom(r []rune, from int, sub string) int {
	s := []rune(sub)
	for i := from; i+len(s) <= len(r); i++ {
		if string(r[i:i+len(s)]) == sub {
			return i
		}
	}
	return -1
}
