package secret

import (
	"strings"

	"ike/internal/lang"
)

// spans.go holds the shared span producers of the masking family (#2345),
// closing the gaps the capability audit (#2337) surfaced: a credential is
// named the same way in a TOML `password = …`, an ini `password: …`, a
// crontab environment line and a Go/PHP/JS assignment, so the recognition
// lives here once rather than per plugin. Two shapes cover them all:
//
//   - PairSpans reads the `key = value` / `key: value` lines of the config
//     formats (toml, ini, crontab) — the dotenv recipe with a configurable
//     separator set.
//   - AssignSpans reads the assignment statements of the C-family source
//     languages (Go, PHP, JS/TS) — the Python producer's stance
//     (plugins/languages/python/mask.go) ported to their shared surface
//     syntax: only the string literals of the value mask, and a literal that
//     names a key being looked up stays readable.
//
// Both ask Suspect, so the built-in tables and the user's own patterns
// (editor.secret_masking_keys) hold identically everywhere.

// Span builds the stand-in span masking [start, end) of line li — the shape
// every producer of this family emits.
func Span(li, start, end int) lang.Span {
	return lang.Span{
		Line: li, StartCol: start, EndCol: end,
		Capture: Capture, Replace: Mask,
	}
}

// PairSpans masks the values of secret-suspect keys on `key<sep>value` lines,
// seps naming the separator runes the format uses (`=` for TOML and crontab,
// `=:` for ini). Comment lines (`#`, `;`) and section headers are skipped; a
// key holding whitespace is no key — it keeps a crontab job line, whose
// command may contain an `=`, from reading as an assignment. A quoted value
// masks its content and keeps the quotes, the JSON producer's convention; a
// bare value masks to the end of the line — cutting at a would-be comment
// could leak the tail of the very value being hidden.
func PairSpans(lines []string, seps string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		start := skipBlank(runes, 0)
		if start >= len(runes) {
			continue
		}
		switch runes[start] {
		case '#', ';', '[':
			continue
		}
		sep := -1
		for i := start; i < len(runes); i++ {
			if strings.ContainsRune(seps, runes[i]) {
				sep = i
				break
			}
		}
		if sep < 0 {
			continue
		}
		key := strings.Trim(strings.TrimSpace(string(runes[start:sep])), `"'`)
		if key == "" || strings.ContainsAny(key, " \t") || !Suspect(key) {
			continue
		}
		vs := skipBlank(runes, sep+1)
		ve := trimBlank(runes, vs, len(runes))
		if vs >= ve {
			continue
		}
		if q := runes[vs]; (q == '"' || q == '\'') && ve-vs >= 2 && runes[ve-1] == q {
			vs, ve = vs+1, ve-1
		}
		if vs < ve {
			out = append(out, Span(li, vs, ve))
		}
	}
	return out
}

// assignKeywords are the declaration words that may precede an assignment
// target in the C-family languages: `const DB_PASSWORD = …`,
// `export let token = …`, `private static $secret = …`, `var key = …`.
var assignKeywords = map[string]bool{
	"const": true, "let": true, "var": true, "export": true,
	"public": true, "protected": true, "private": true,
	"static": true, "readonly": true, "final": true,
}

// AssignSpans masks the string literals in the value of an assignment whose
// target names a secret, for the source languages whose statements share the
// C-family shape: Go (`password := "…"`), PHP (`$this->password = "…"`) and
// JS/TS (`const apiKey = "…"`). The recognition is one statement line, like
// the Python producer's: declaration keywords and an optional `$` sigil are
// skipped, the target may be dotted (`cfg.db.password`) or arrowed
// (`$this->secret`), its last component decides, and an optional type
// annotation between name and `=` is skipped, not parsed. Only a bare `=` or
// Go's `:=` counts — comparisons and compound assignments never mask.
func AssignSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		name, i, ok := assignTarget(runes)
		if !ok || !Suspect(name) {
			continue
		}
		out = literalSpans(out, li, runes, skipBlank(runes, i))
	}
	return out
}

// assignTarget reads the assignment target at the start of a line, returning
// the name that decides and the index just past the `=`/`:=`.
func assignTarget(runes []rune) (string, int, bool) {
	i := skipBlank(runes, 0)
	var name string
	for {
		if i < len(runes) && runes[i] == '$' {
			i++
		}
		w, j := identSpan(runes, i)
		if w == "" {
			return "", 0, false
		}
		if name == "" && assignKeywords[w] && (j >= len(runes) || isBlank(runes[j])) {
			i = skipBlank(runes, j)
			continue
		}
		name = w
		i = j
		break
	}
	// A dotted or arrowed target: the last component names the value.
	for i < len(runes) && (runes[i] == '.' || (runes[i] == '-' && i+1 < len(runes) && runes[i+1] == '>')) {
		if runes[i] == '.' {
			i++
		} else {
			i += 2
		}
		w, j := identSpan(runes, i)
		if w == "" {
			return "", 0, false
		}
		name, i = w, j
	}
	i = skipBlank(runes, i)
	if i+1 < len(runes) && runes[i] == ':' && runes[i+1] == '=' {
		return name, i + 2, true // Go's short declaration
	}
	if i < len(runes) && runes[i] == ':' {
		// A type annotation (TS `const token: string = …`): skipped, not
		// parsed. Anything outside the annotation rune set means the line is
		// not the assignment it looked like — a case label, an object member.
		for i++; i < len(runes) && runes[i] != '='; i++ {
			if !annotRune(runes[i]) {
				return "", 0, false
			}
		}
	} else if w, j := identSpan(runes, i); w != "" {
		// One bare type token (Go `var password string = …`).
		i = skipBlank(runes, j)
	}
	if i >= len(runes) || runes[i] != '=' {
		return "", 0, false
	}
	if i+1 < len(runes) && (runes[i+1] == '=' || runes[i+1] == '>') {
		return "", 0, false // a comparison or an arrow function
	}
	return name, i + 1, true
}

// literalSpans appends the masks for the string literals in the value
// starting at rune index start, leaving the expression around them readable —
// the Python producer's #1930 rule. A literal that is not the whole value,
// is identifier-shaped and is itself secret-suspect is the name of something
// being looked up (`os.Getenv("DB_PASSWORD")`) and stays readable.
func literalSpans(out []lang.Span, li int, runes []rune, start int) []lang.Span {
	end := valueLimit(runes, start)
	for i := start; i < end; i++ {
		q := runes[i]
		if q != '\'' && q != '"' && q != '`' {
			continue
		}
		close, closed := literalEnd(runes, i, q)
		if !closed {
			// An unterminated string: everything to the end is inside it, and
			// masking that whole is the safe reading.
			if i+1 < end {
				out = append(out, Span(li, i+1, end))
			}
			return out
		}
		cs, ce := i+1, close-1
		// A literal that is the entire right-hand side is the value itself,
		// whatever it says; only a literal inside a larger expression can be
		// the name of something being looked up.
		sole := i == start && close == end
		if ce > cs && (sole || !nameShaped(runes[cs:ce])) {
			out = append(out, Span(li, cs, ce))
		}
		i = close - 1
	}
	return out
}

// valueLimit returns the end of the value: the start of a trailing `//` or
// `#` comment outside quotes, or the line end.
func valueLimit(runes []rune, start int) int {
	for i := start; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '\'', '"', '`':
			close, closed := literalEnd(runes, i, r)
			if !closed {
				return len(runes)
			}
			i = close - 1
		case '#':
			return trimBlank(runes, start, i)
		case '/':
			if i+1 < len(runes) && runes[i+1] == '/' {
				return trimBlank(runes, start, i)
			}
		}
	}
	return trimBlank(runes, start, len(runes))
}

// literalEnd returns the index just past the string literal opening at rune
// index i with quote q, and whether it closed on this line. Backslash escapes
// are honoured except in backtick literals, which Go leaves raw.
func literalEnd(runes []rune, i int, q rune) (int, bool) {
	for i++; i < len(runes); i++ {
		if runes[i] == '\\' && q != '`' {
			i++
			continue
		}
		if runes[i] == q {
			return i + 1, true
		}
	}
	return len(runes), false
}

// nameShaped reports whether a literal's content reads as the name of
// something rather than a value: identifier-shaped and secret-suspect on its
// own, the way `process.env.DB_PASSWORD` and `os.Getenv("DB_PASSWORD")`
// spell a lookup.
func nameShaped(content []rune) bool {
	if len(content) == 0 || !identStart(content[0]) {
		return false
	}
	for _, r := range content {
		if !identStart(r) && !(r >= '0' && r <= '9') && r != '.' {
			return false
		}
	}
	return Suspect(string(content))
}

// annotRune is the rune set a skipped type annotation may hold: identifiers,
// generics, unions, arrays and quoted literal types.
func annotRune(r rune) bool {
	switch r {
	case '_', '.', '[', ']', '<', '>', ',', ' ', '\t', '"', '\'', '|', '&', '?':
		return true
	}
	return identStart(r) || (r >= '0' && r <= '9')
}

// identSpan reads the identifier starting at rune index i, returning it and
// the index past it — "" when i does not start one.
func identSpan(runes []rune, i int) (string, int) {
	if i >= len(runes) || !identStart(runes[i]) {
		return "", i
	}
	j := i
	for j < len(runes) && (identStart(runes[j]) || runes[j] >= '0' && runes[j] <= '9') {
		j++
	}
	return string(runes[i:j]), j
}

func identStart(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isBlank(r rune) bool { return r == ' ' || r == '\t' }

func skipBlank(runes []rune, i int) int {
	for i < len(runes) && isBlank(runes[i]) {
		i++
	}
	return i
}

func trimBlank(runes []rune, start, end int) int {
	if end > len(runes) {
		end = len(runes)
	}
	for end > start && isBlank(runes[end-1]) {
		end--
	}
	return end
}
