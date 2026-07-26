package textobject

import (
	"strings"
	"unicode"

	"ike/internal/editor/buffer"
)

// tag.go implements the XML/HTML tag text object "it" / "at" (#1193) with a
// plain text scan — no parse tree, so tags inside string literals or comments
// false-positive; acceptable, mirroring the bracket objects.

// tagToken is one scanned tag: an opening <name ...>, closing </name>, or
// self-closing <name .../> (ignored for matching).
type tagToken struct {
	name        string
	start, end  buffer.Position // '<' position and one past '>'
	closing     bool
	selfClosing bool
}

// Tag resolves the innermost tag pair enclosing the cursor. Inner spans the
// content between the opening tag's '>' and the closing tag's '<'; around
// spans the whole element including both tags.
func Tag(b *buffer.Buffer, p buffer.Position, around bool) Result {
	toks := scanTags(b)
	// Match open/close pairs with a stack; keep the innermost pair whose
	// element (open '<' .. close '>') encloses the cursor.
	type openTag struct{ tok tagToken }
	var stack []openTag
	best := Result{}
	cursorAfter := func(a buffer.Position) bool { return !p.Before(a) }
	for _, t := range toks {
		if t.selfClosing {
			continue
		}
		if !t.closing {
			stack = append(stack, openTag{t})
			continue
		}
		// Unwind to the matching open tag (tolerates unclosed tags in between).
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].tok.name != t.name {
				continue
			}
			open := stack[i].tok
			stack = stack[:i]
			if cursorAfter(open.start) && p.Before(t.end) {
				// Innermost match wins: later matches enclosing the cursor are
				// outer elements, so keep the first hit.
				if !best.OK {
					if around {
						best = Result{Range: buffer.Range{Start: open.start, End: t.end}, OK: true}
					} else {
						best = Result{Range: buffer.Range{Start: open.end, End: t.start}, OK: true}
					}
				}
			}
			break
		}
	}
	return best
}

// scanTags walks the whole buffer and returns every tag token in order.
func scanTags(b *buffer.Buffer) []tagToken {
	var toks []tagToken
	for l := 0; l < b.LineCount(); l++ {
		line := []rune(b.Line(l))
		for c := 0; c < len(line); c++ {
			if line[c] != '<' {
				continue
			}
			tok, endCol, ok := parseTag(line, l, c)
			if !ok {
				continue
			}
			toks = append(toks, tok)
			c = endCol - 1
		}
	}
	return toks
}

// parseTag parses a tag starting at line[c] == '<' on line l. Multi-line tags
// are not recognised (they end the scan for this candidate).
func parseTag(line []rune, l, c int) (tagToken, int, bool) {
	i := c + 1
	closing := false
	if i < len(line) && line[i] == '/' {
		closing = true
		i++
	}
	nameStart := i
	for i < len(line) && (unicode.IsLetter(line[i]) || unicode.IsDigit(line[i]) || line[i] == '-' || line[i] == '_' || line[i] == ':') {
		i++
	}
	if i == nameStart {
		return tagToken{}, 0, false
	}
	name := strings.ToLower(string(line[nameStart:i]))
	for i < len(line) && line[i] != '>' {
		i++
	}
	if i >= len(line) {
		return tagToken{}, 0, false
	}
	self := !closing && line[i-1] == '/'
	if voidTags[name] {
		self = true
	}
	return tagToken{
		name:        name,
		start:       buffer.Position{Line: l, Col: c},
		end:         buffer.Position{Line: l, Col: i + 1},
		closing:     closing,
		selfClosing: self,
	}, i + 1, true
}

// voidTags are HTML elements that never have a closing tag.
var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"source": true, "track": true, "wbr": true,
}
