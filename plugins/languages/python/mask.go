package langpython

// mask.go extends secret masking (#1623) from dotenv files to Python source
// (#1811): a credential is just as exposed in `self.password = "hunter2"` as
// in `DB_PASSWORD=hunter2`, and the name on the left says the same thing about
// the value on the right. The assignment target names the value, so
// secret.Suspect decides — the same built-in table and the same user
// extensions/exemptions (editor.secret_masking_keys) the dotenv producer uses,
// which is what lets `self.timeout = 500` mask through a configured
// `*timeout*` pattern.
//
// The recognition is deliberately shallow: one statement line, an identifier
// (optionally dotted, `self.password`) with an optional annotation, a bare `=`,
// and whatever follows to the end of the statement. No parser — a language
// plugin that misreads an exotic line masks a value that did not need masking,
// which costs one toggle; the shapes people actually write their secrets in
// are all in here.
//
// What the mask covers is narrower than the assignment, though (#1930): only a
// *string literal* can put a credential on screen, so only literal contents
// mask, never the expression around them. `token = item["token"]`,
// `token = get_token()` and `token = other` carry no secret and no mask, and
// `PROXY_API_KEY = os.environ.get("PROXY_API_KEY", "8479…")` masks the
// fallback alone — hiding the environment variable's name would destroy the
// line's meaning without hiding anything. Quotes stay visible and the content
// masks, the JSON producer's convention (#1813), so the value still reads as a
// string.
//
// Telling a key name from a value is a heuristic: a literal that is not the
// whole right-hand side, is shaped like an identifier and is itself
// secret-suspect ("PARSER_PROXY_API_KEY", "token") is a name being looked up,
// so it stays readable — which covers `os.environ.get`, `config["api_key"]`
// and `d.get("token")` at once. Everything else masks, keeping the
// internal/secret stance of erring towards hiding: `password = "token"` is the
// whole value and masks, and a false positive costs one toggle.
//
// Everything downstream is the dotenv behaviour unchanged: the span carries
// secret.Capture and the fixed-width secret.Mask, so the conceal layer reveals
// it positionally under the caret (#1594), the view toggle
// (view.toggleSecretMasking) applies, and so do the `secret_masking=` conceal
// file rules (#1620).

import (
	"ike/internal/lang"
	"ike/internal/secret"
)

// maskState is the lexical state a line hands to the next one: an open
// triple-quoted string, and whether that string is the value of a suspect
// assignment — a PEM private key pasted between `"""` runs over many lines,
// and masking only its first one would hide nothing worth hiding.
type maskState struct {
	triple rune // quote rune of an open triple-quoted string, 0 when none
	hide   bool // the open string is a suspect value, so its lines mask whole
}

// maskSpans produces the mask spans for a Python buffer: one per secret string
// literal inside an assignment whose target names a secret.
func maskSpans(lines []string) []lang.Span {
	var out []lang.Span
	var st maskState
	for li, line := range lines {
		runes := []rune(line)
		if st.triple != 0 {
			end, closed := tripleEnd(runes, 0, st.triple)
			if st.hide {
				out = appendMask(out, li, runes, 0, end)
			}
			if !closed {
				continue
			}
			st = maskState{triple: scanState(runes, end)}
			continue
		}
		out = assignMasks(out, li, runes, &st)
	}
	return out
}

// assignMasks reads one statement line and appends the mask spans for the
// string literals in its value, updating st with any string the line leaves
// open. A line that assigns nothing, assigns to a name no pattern finds
// suspect, or holds no literal worth hiding only contributes its lexical
// state.
func assignMasks(out []lang.Span, li int, runes []rune, st *maskState) []lang.Span {
	name, i, ok := targetName(runes)
	if !ok {
		*st = maskState{triple: scanState(runes, 0)}
		return out
	}
	if !secret.Suspect(name) {
		*st = maskState{triple: scanState(runes, i)}
		return out
	}
	start := skipSpaces(runes, i)
	end, open := valueEnd(runes, start)
	st.triple = open
	st.hide = open != 0
	if start >= end {
		// `password = ` with nothing after it, or a comment where the value
		// should be: a mask over nothing only reads as a missing value.
		return out
	}
	if open != 0 {
		// A triple-quoted value left open runs over the following lines — a
		// pasted PEM key — and masks whole from here, quotes included: there is
		// no closed content span to cover, and hiding part of a private key
		// hides nothing.
		return appendMask(out, li, runes, start, end)
	}
	return literalMasks(out, li, runes, start, end)
}

// literalMasks appends the masks for the string literals in the value spanning
// [start, end), leaving the expression around them readable.
func literalMasks(out []lang.Span, li int, runes []rune, start, end int) []lang.Span {
	for i := start; i < end; i++ {
		if r := runes[i]; r != '\'' && r != '"' {
			continue
		}
		lit, ok := literalAt(runes, i, end)
		if !ok {
			// An unterminated string: everything to the end of the value is
			// inside it, and masking that whole is the safe reading.
			return appendMask(out, li, runes, i, end)
		}
		i = lit.next - 1
		// A literal that is the entire right-hand side is the value itself,
		// whatever it says; only a literal sitting inside a larger expression
		// can be the name of something being looked up.
		sole := lit.start == start && lit.next == end
		if !sole && nameLiteral(runes, lit) {
			continue
		}
		if lit.fstring {
			out = appendFStringMasks(out, li, runes, lit)
			continue
		}
		out = appendSpan(out, li, lit.contentStart, lit.contentEnd)
	}
	return out
}

// literal is one string literal of a value: its outer bounds (prefix letters
// included), the bounds of its content between the quotes, the index just past
// it, and whether an `f` prefix makes `{...}` runs in the content code rather
// than text.
type literal struct {
	start        int
	contentStart int
	contentEnd   int
	next         int
	fstring      bool
}

// literalAt reads the string literal whose quote sits at rune index q, bounded
// by limit, and reports whether it closed inside those bounds. The quote run
// tells one-line from triple-quoted; a triple-quoted literal that closes here
// is an ordinary value, an open one never reaches this far.
func literalAt(runes []rune, q, limit int) (literal, bool) {
	lit := literal{start: q}
	for j := q - 1; j >= 0 && q-j <= 2 && isPrefixRune(runes[j]); j-- {
		lit.start = j
		if runes[j] == 'f' || runes[j] == 'F' {
			lit.fstring = true
		}
	}
	r := runes[q]
	if q+2 < limit && runes[q+1] == r && runes[q+2] == r {
		end, closed := tripleEnd(runes, q+3, r)
		if !closed || end > limit {
			return literal{}, false
		}
		lit.contentStart, lit.contentEnd, lit.next = q+3, end-3, end
		return lit, true
	}
	end, closed := quoteEnd(runes, q)
	if !closed || end > limit {
		return literal{}, false
	}
	lit.contentStart, lit.contentEnd, lit.next = q+1, end-1, end
	return lit, true
}

// nameLiteral reports whether the literal's content reads as the name of
// something rather than a value: identifier-shaped and secret-suspect on its
// own, the way `os.environ.get("PROXY_API_KEY", …)` and `d.get("token")` spell
// a lookup. The identifier shape is what keeps a hyphenated or spaced value
// ("my-secret") out of the exemption — no key is written that way.
func nameLiteral(runes []rune, lit literal) bool {
	if lit.contentEnd <= lit.contentStart {
		return false
	}
	content := runes[lit.contentStart:lit.contentEnd]
	if !isIdentStart(content[0]) {
		return false
	}
	for _, r := range content {
		if !isIdentStart(r) && !isDigitRune(r) && r != '.' {
			return false
		}
	}
	return secret.Suspect(string(content))
}

// appendFStringMasks masks the literal text of an f-string and leaves its
// `{...}` interpolations readable: an interpolation is an expression, and an
// expression is exactly what this producer declines to hide. A doubled
// `{{`/`}}` is an escaped brace, so it is text like any other.
func appendFStringMasks(out []lang.Span, li int, runes []rune, lit literal) []lang.Span {
	text := lit.contentStart
	for i := lit.contentStart; i < lit.contentEnd; i++ {
		switch runes[i] {
		case '\\':
			i++
		case '{':
			if i+1 < lit.contentEnd && runes[i+1] == '{' {
				i++
				continue
			}
			out = appendSpan(out, li, text, i)
			for depth := 1; i+1 < lit.contentEnd && depth > 0; {
				i++
				switch runes[i] {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			text = i + 1
		case '}':
			if i+1 < lit.contentEnd && runes[i+1] == '}' {
				i++
			}
		}
	}
	return appendSpan(out, li, text, lit.contentEnd)
}

// isPrefixRune reports whether r may be part of a string literal's prefix
// (`r`, `b`, `u`, `f` and their combinations, in either case).
func isPrefixRune(r rune) bool {
	switch r {
	case 'r', 'R', 'b', 'B', 'u', 'U', 'f', 'F':
		return true
	}
	return false
}

// targetName reads the assignment target at the start of a line and returns
// the name that decides — the last component of a dotted target, so
// `self.password` and `cfg.db.secret` are read by `password` and `secret` —
// plus the index just past the `=`. Anything that is not a plain assignment
// (a comparison, an augmented assignment, a call, a bare expression) reports
// false.
func targetName(runes []rune) (string, int, bool) {
	i := skipSpaces(runes, 0)
	name, j := identifier(runes, i)
	if name == "" {
		return "", 0, false
	}
	for j < len(runes) && runes[j] == '.' {
		next, k := identifier(runes, j+1)
		if next == "" {
			return "", 0, false
		}
		name, j = next, k
	}
	j = skipSpaces(runes, j)
	if j < len(runes) && runes[j] == ':' {
		// An annotated assignment: the type is skipped, not parsed. The rune
		// set covers `str`, `Optional[str]` and a quoted forward reference;
		// anything else means the line is not the assignment it looked like.
		for j++; j < len(runes) && runes[j] != '='; j++ {
			if !annotationRune(runes[j]) {
				return "", 0, false
			}
		}
	}
	if j >= len(runes) || runes[j] != '=' || (j+1 < len(runes) && runes[j+1] == '=') {
		return "", 0, false
	}
	return name, j + 1, true
}

// valueEnd returns the end column of the value starting at rune index start,
// and the quote rune of a triple-quoted string the value leaves open. The scan
// is quote-aware so a `#` inside the value is part of it — cutting the mask at
// a `#` in `"a#b"` would leak the tail of the very value being hidden. An
// unterminated one-line string masks to the end of the line, the safe reading.
func valueEnd(runes []rune, start int) (int, rune) {
	for i := start; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '#':
			return trimSpaces(runes, start, i), 0
		case '\'', '"':
			if i+2 < len(runes) && runes[i+1] == r && runes[i+2] == r {
				end, closed := tripleEnd(runes, i+3, r)
				if !closed {
					return len(runes), r
				}
				i = end - 1
				continue
			}
			end, closed := quoteEnd(runes, i)
			if !closed {
				return len(runes), 0
			}
			i = end - 1
		}
	}
	return trimSpaces(runes, start, len(runes)), 0
}

// scanState walks a line from rune index i for its lexical state alone and
// returns the quote rune of a triple-quoted string left open, or 0. Comments
// and one-line strings are consumed so a `"""` inside either never opens one.
func scanState(runes []rune, i int) rune {
	for ; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '#':
			return 0
		case '\'', '"':
			if i+2 < len(runes) && runes[i+1] == r && runes[i+2] == r {
				end, closed := tripleEnd(runes, i+3, r)
				if !closed {
					return r
				}
				i = end - 1
				continue
			}
			end, closed := quoteEnd(runes, i)
			if !closed {
				return 0
			}
			i = end - 1
		}
	}
	return 0
}

// appendMask adds the mask covering [start, end) of a continuation line,
// skipping a range that holds nothing but whitespace.
func appendMask(out []lang.Span, li int, runes []rune, start, end int) []lang.Span {
	s := skipSpaces(runes, start)
	e := trimSpaces(runes, s, end)
	if s >= e {
		return out
	}
	return append(out, maskSpan(li, s, e))
}

// appendSpan adds the mask covering [start, end) verbatim, skipping an empty
// range: there is nothing to hide in `password = ""`, and a mask over nothing
// only reads as a value that is not there.
func appendSpan(out []lang.Span, li, start, end int) []lang.Span {
	if end <= start {
		return out
	}
	return append(out, maskSpan(li, start, end))
}

// maskSpan builds the stand-in span the conceal layer draws instead of the
// value.
func maskSpan(li, start, end int) lang.Span {
	return lang.Span{
		Line:     li,
		StartCol: start,
		EndCol:   end,
		Capture:  secret.Capture,
		Replace:  secret.Mask,
	}
}

// quoteEnd returns the index just past the one-line string opening at rune
// index i, honouring backslash escapes, and whether it closed on this line.
func quoteEnd(runes []rune, i int) (int, bool) {
	q := runes[i]
	for i++; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			i++
		case q:
			return i + 1, true
		}
	}
	return len(runes), false
}

// tripleEnd returns the index just past the closing triple quote of kind q
// searched from rune index i, and whether this line holds one.
func tripleEnd(runes []rune, i int, q rune) (int, bool) {
	for ; i < len(runes); i++ {
		if runes[i] == '\\' {
			i++
			continue
		}
		if runes[i] == q && i+2 < len(runes) && runes[i+1] == q && runes[i+2] == q {
			return i + 3, true
		}
	}
	return len(runes), false
}

// identifier reads the identifier starting at rune index i, returning it and
// the index past it — "" when i does not start one.
func identifier(runes []rune, i int) (string, int) {
	if i >= len(runes) || (!isIdentStart(runes[i])) {
		return "", i
	}
	j := i
	for j < len(runes) && (isIdentStart(runes[j]) || isDigitRune(runes[j])) {
		j++
	}
	return string(runes[i:j]), j
}

// annotationRune is the rune set a skipped annotation may hold.
func annotationRune(r rune) bool {
	switch r {
	case '_', '.', '[', ']', ',', ' ', '\t', '"', '\'', '|':
		return true
	}
	return isIdentStart(r) || isDigitRune(r)
}

func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isDigitRune(r rune) bool { return r >= '0' && r <= '9' }

func skipSpaces(runes []rune, i int) int {
	for i < len(runes) && isSpaceRune(runes[i]) {
		i++
	}
	return i
}

func trimSpaces(runes []rune, start, end int) int {
	if end > len(runes) {
		end = len(runes)
	}
	for end > start && isSpaceRune(runes[end-1]) {
		end--
	}
	return end
}

func isSpaceRune(r rune) bool { return r == ' ' || r == '\t' }
