package structval

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// literal.go is the rule every other language gets, and the fallback the four
// structured families fall through to: the innermost string literal the caret
// is inside, decoded. A Go, Python, Rust or shell buffer has no "value under
// the cursor" in the manifest sense, but it does have literals — and copying
// one without its quotes and with its escapes resolved is the same gesture.
//
// Which node is a literal is decided by kind name rather than by a per-grammar
// table: every grammar spells it with "string" (interpreted_string_literal,
// template_string, string_literal, plain "string") or with "char". The
// *_content / *_start / *_end pieces that grammars split literals into are
// excluded — they hold a fragment of the text between two escapes, never the
// whole literal.

// literalValue implements the string-literal rule. Outer is the literal with
// its quotes and prefix, Inner its decoded text.
func literalValue(src string, chain []Node) (Value, bool) {
	for _, n := range chain {
		if !isStringLiteral(n.Kind) {
			continue
		}
		raw := text(src, n)
		return Value{Inner: decodeLiteral(raw), Outer: raw}, true
	}
	return Value{}, false
}

// isStringLiteral reports whether kind names a whole string (or character)
// literal rather than one of the pieces a grammar splits it into.
func isStringLiteral(kind string) bool {
	k := strings.ToLower(kind)
	for _, piece := range []string{"content", "fragment", "start", "end", "escape"} {
		if strings.Contains(k, piece) {
			return false
		}
	}
	return strings.Contains(k, "string") || strings.Contains(k, "char")
}

// decodeLiteral strips a literal's delimiters and resolves its escapes.
//
// strconv.Unquote answers the Go-shaped cases exactly — interpreted strings,
// raw backtick strings, rune literals — and, since Go's escape syntax is the
// one JSON, JavaScript, Rust and C share, most others too. What it rejects
// (Python's `'…'` with more than one character, its `r"…"` / `f"…"` prefixes,
// triple-quoted forms) is handled by hand: strip the prefix and the quote run,
// then resolve the escapes unless the prefix says the literal is raw.
func decodeLiteral(raw string) string {
	if s, err := strconv.Unquote(raw); err == nil {
		return s
	}
	prefix, body, ok := splitLiteral(raw)
	if !ok {
		return raw
	}
	if strings.ContainsAny(prefix, "rR") {
		return body // a raw literal: backslashes are text
	}
	return unescape(body)
}

// quoteRuns are the delimiters a literal can carry, longest first so `"""`
// wins over `"`.
var quoteRuns = []string{`"""`, "'''", "```", `"`, "'", "`"}

// splitLiteral splits a literal into its letter prefix (Python's r/b/f/u,
// Rust's b, C's L …) and the body between its delimiters. ok is false when raw
// carries no recognisable delimiters at all — a bare YAML plain scalar, say,
// which is already its own text.
func splitLiteral(raw string) (prefix, body string, ok bool) {
	i := 0
	for i < len(raw) && isPrefixLetter(raw[i]) {
		i++
	}
	prefix, rest := raw[:i], raw[i:]
	for _, q := range quoteRuns {
		if len(rest) >= 2*len(q) && strings.HasPrefix(rest, q) && strings.HasSuffix(rest, q) {
			return prefix, rest[len(q) : len(rest)-len(q)], true
		}
	}
	return "", "", false
}

// isPrefixLetter reports whether b can be part of a literal's letter prefix.
func isPrefixLetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// unescape resolves the backslash escapes the mainstream languages agree on.
// An escape it does not know is left standing, backslash included: guessing
// would silently rewrite text the language means literally.
func unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			i++
			continue
		}
		e := s[i+1]
		if r, ok := simpleEscapes[e]; ok {
			b.WriteByte(r)
			i += 2
			continue
		}
		width := 0
		switch e {
		case 'x':
			width = 2
		case 'u':
			width = 4
		case 'U':
			width = 8
		}
		if width > 0 && i+2+width <= len(s) {
			if v, err := strconv.ParseUint(s[i+2:i+2+width], 16, 32); err == nil {
				if e == 'x' {
					b.WriteByte(byte(v))
				} else {
					b.WriteRune(rune(v))
				}
				i += 2 + width
				continue
			}
		}
		// Unknown escape: keep it exactly as written.
		_, n := utf8.DecodeRuneInString(s[i+1:])
		b.WriteString(s[i : i+1+n])
		i += 1 + n
	}
	return b.String()
}

// simpleEscapes are the one-letter escapes; the list is the intersection every
// C-descended language shares, plus JSON's `\/`.
var simpleEscapes = map[byte]byte{
	'n': '\n', 't': '\t', 'r': '\r', 'a': 7, 'b': 8, 'f': 12, 'v': 11,
	'0': 0, '\\': '\\', '\'': '\'', '"': '"', '`': '`', '/': '/',
}
