package escapes

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// encode.go is the writing direction of the unicode-escape family (#2338):
// escapes.go detects escapes so the editor can *render* them decoded, this
// file rewrites text between the escaped and the plain form so the editor's
// "Escape Selection as Unicode" / "Unescape Selection" commands can change the
// buffer. Both directions read the same UnicodeDialect, so the per-language
// knowledge — which forms exist, which quotes process escapes — stays in one
// place; the editor only picks the dialect for the buffer's language and hands
// over the text.

// DialectFor returns the escape dialect of the language id, as registered by
// the language plugins' span hooks (plugins/languages/*). ok is false for a
// language with no unicode escapes of its own — the caller then either
// declines or falls back to UnicodeFallback.
//
// The table is keyed by lang.Language.ID rather than carried on the Language
// value itself: internal/lang is this package's *dependency* (spans are
// lang.Span), so a dialect field there would be a cycle. Keeping the mapping
// next to the dialect constants also means the language and the form it writes
// are read side by side.
func DialectFor(langID string) (UnicodeDialect, bool) {
	d, ok := dialects[langID]
	return d, ok
}

// dialects maps a language id onto the dialect its span producer scans in.
// Every entry mirrors one plugins/languages/* call of UnicodeSpansIn; a
// language whose producer passes no dialect (or has no producer at all) is
// deliberately absent.
var dialects = map[string]UnicodeDialect{
	"go":         UnicodeGo,
	"json":       UnicodeJSON,
	"ndjson":     UnicodeJSON,
	"typescript": UnicodeScript, // the id covers .js/.jsx/.ts/.tsx alike
	"python":     UnicodePython,
	"php":        UnicodePHP,
	"yaml":       UnicodeYAML,
	"ansible":    UnicodeYAML, // shares the yaml span hook
	"toml":       UnicodeTOML,
}

// UnicodeFallback is the dialect for text whose language has no escape syntax
// of its own — a log excerpt, a plain .txt, an unsaved buffer. It writes and
// reads the \uXXXX form with surrogate pairs, the one spelling every consumer
// of escaped text understands, and treats both quote characters as
// escape-processing so the caret fallback still finds a literal. Declining
// outright would lock the commands out of exactly the files (log dumps,
// scratch buffers) where escaped text most often has to be unfolded by hand.
var UnicodeFallback = UnicodeDialect{EscapeQuotes: `"'`}

// EncodeUnicode returns s with every non-ASCII rune rewritten as an escape in
// dialect d: "über" becomes `über`.
//
// Only runes above U+007F are escaped. Everything ASCII — including a
// backslash — is copied verbatim, which is what keeps an already-escaped
// sequence from being escaped a second time: `ü` is seven ASCII
// characters and comes back out unchanged, and so does `\\u00fc`. Control
// characters are left alone too: a selection spans whole lines, and turning
// its newlines into escapes would rewrite the buffer's structure rather than
// its text. Bytes that are not valid UTF-8 stay as they are — an escape for
// U+FFFD would claim the file says something it does not.
func EncodeUnicode(s string, d UnicodeDialect) string {
	var b strings.Builder
	b.Grow(len(s))
	for i, w := 0, 0; i < len(s); i += w {
		r, size := utf8.DecodeRuneInString(s[i:])
		w = size
		if r < utf8.RuneSelf {
			b.WriteByte(s[i])
			continue
		}
		if r == utf8.RuneError && size == 1 {
			b.WriteByte(s[i])
			continue
		}
		b.WriteString(escapeRune(r, d))
	}
	return b.String()
}

// escapeRune writes one rune in the dialect's form. A BMP code point is always
// the plain \uXXXX — the form every dialect reads. Above the BMP the dialects
// diverge: Go, Python, YAML and TOML have the eight-digit \UXXXXXXXX, ES6 and
// PHP 7 have \u{X…}, and JSON has neither, so it takes the UTF-16 surrogate
// pair its own \uXXXX escapes already encode.
func escapeRune(r rune, d UnicodeDialect) string {
	if r <= 0xFFFF {
		return `\u` + hex4(int(r))
	}
	switch {
	case d.Long:
		return `\U` + hex(int(r), 8)
	case d.Brace:
		return `\u{` + hex(int(r), 1) + "}"
	default:
		hi, lo := utf16.EncodeRune(r)
		return `\u` + hex4(int(hi)) + `\u` + hex4(int(lo))
	}
}

// hex4 is hex(v, 4), the width of a \uXXXX payload.
func hex4(v int) string { return hex(v, 4) }

// hex formats v as lowercase hex, zero-padded to at least width digits.
// Lowercase is the spelling the standard libraries and formatters of every
// dialect emit ("ü"), so a round trip through the commands leaves a file
// looking like its neighbours.
func hex(v, width int) string {
	const digits = "0123456789abcdef"
	var buf [8]byte
	n := 0
	for {
		buf[n] = digits[v&0xf]
		n++
		v >>= 4
		if v == 0 {
			break
		}
	}
	var b strings.Builder
	for i := n; i < width; i++ {
		b.WriteByte('0')
	}
	for i := n - 1; i >= 0; i-- {
		b.WriteByte(buf[i])
	}
	return b.String()
}

// DecodeUnicode returns s with every escape dialect d knows replaced by the
// character it names: `über` becomes "über". It is the exact inverse of
// EncodeUnicode, and forgiving beyond it — every form the *scanner* decodes
// (\UXXXXXXXX, \u{X…}, \xNN, surrogate pairs) resolves here too, so text
// escaped by another tool unfolds as well.
//
// Unlike the span scanner this does not walk the quote state: the caller has
// already picked the range — a selection or the literal under the caret — and
// inside it every escape is meant. Escapes are still consumed pairwise, so an
// escaped backslash keeps what follows it literal: `\\u00fc` stays as it is,
// which makes decode(encode(x)) == x for text that already contained one.
// Anything the dialect does not decode (a truncated escape, a lone surrogate,
// a non-graphic code point, Python's \N{NAME}) is copied through untouched.
func DecodeUnicode(s string, d UnicodeDialect) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(runes); i++ {
		if runes[i] != '\\' {
			b.WriteRune(runes[i])
			continue
		}
		if end, text, ok := unicodeEscapeAt(runes, i, d); ok {
			b.WriteString(text)
			i = end - 1
			continue
		}
		// An ordinary escape: the backslash and its payload travel together,
		// so the "u" of a \\u sequence cannot start a decode of its own.
		b.WriteRune(runes[i])
		if i+1 < len(runes) {
			b.WriteRune(runes[i+1])
			i++
		}
	}
	return b.String()
}

// LiteralAt returns the rune range [start, end) of the *body* of the
// escape-processing string literal containing col in line — quotes excluded,
// so escaping a literal never touches its delimiters. It is what the editor
// commands act on when there is no selection.
//
// ok is false when col sits outside any literal, and equally when it sits
// inside a raw one (Go's `…`, Python's r"…", YAML's '…'): there a backslash is
// literal text, so writing an escape into it would change what the file says
// instead of how it spells it. An unterminated literal ends at the end of the
// line.
func LiteralAt(line string, col int, d UnicodeDialect) (start, end int, ok bool) {
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case strings.ContainsRune(d.RawQuotes, r):
			close := skipRawLiteral(runes, i, d.RawEscapesQuote)
			if col >= i && col <= close {
				return 0, 0, false // inside a raw literal: nothing to rewrite
			}
			i = close
		case strings.ContainsRune(d.EscapeQuotes, r):
			if d.StringPrefixes && rawPrefixed(runes, i) {
				close := skipRawLiteral(runes, i, true)
				if col >= i && col <= close {
					return 0, 0, false
				}
				i = close
				continue
			}
			close := skipEscapeLiteral(runes, i)
			if col >= i && col <= close {
				return i + 1, min(close, len(runes)), true
			}
			i = close
		}
	}
	return 0, 0, false
}

// skipEscapeLiteral returns the index of the closing quote of the
// escape-processing literal opened at runes[open], or the end of the line when
// it never closes. A backslash consumes the next rune, so a \" cannot close.
func skipEscapeLiteral(runes []rune, open int) int {
	q := runes[open]
	for i := open + 1; i < len(runes); i++ {
		if runes[i] == '\\' {
			i++
			continue
		}
		if runes[i] == q {
			return i
		}
	}
	return len(runes)
}
