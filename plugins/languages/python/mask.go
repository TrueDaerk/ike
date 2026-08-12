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

// maskSpans produces the mask spans for a Python buffer: one per assignment
// whose target names a secret, covering the value.
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
		span, ok := assignMask(li, runes, &st)
		if ok {
			out = append(out, span)
		}
	}
	return out
}

// assignMask reads one statement line and returns the mask span for its value,
// updating st with any string the line leaves open. A line that assigns
// nothing, or assigns to a name no pattern finds suspect, only contributes its
// lexical state.
func assignMask(li int, runes []rune, st *maskState) (lang.Span, bool) {
	name, i, ok := targetName(runes)
	if !ok {
		*st = maskState{triple: scanState(runes, 0)}
		return lang.Span{}, false
	}
	if !secret.Suspect(name) {
		*st = maskState{triple: scanState(runes, i)}
		return lang.Span{}, false
	}
	start := skipSpaces(runes, i)
	end, open := valueEnd(runes, start)
	st.triple = open
	st.hide = open != 0
	if start >= end {
		// `password = ` with nothing after it, or a comment where the value
		// should be: a mask over nothing only reads as a missing value.
		return lang.Span{}, false
	}
	return maskSpan(li, start, end), true
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
