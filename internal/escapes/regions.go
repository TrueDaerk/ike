package escapes

import (
	"unicode"
	"unicode/utf16"

	"ike/internal/lang"
)

// regions.go holds the two unicode-escape dialects the quote-state scanner in
// escapes.go cannot express (#2345):
//
//   - CSS writes an escape as a backslash followed by one to six hex digits,
//     with no `u` prefix at all, and the escape is valid everywhere — in
//     identifiers just as in strings — so there is no quote state to track. An
//     optional single whitespace character terminates the digit run and is
//     part of the escape (`caf\e9 s` is "cafés").
//   - Shell decodes escapes only inside ANSI-C quoting (`$'…'`): a plain
//     '…' literal is raw, and "…" processes `\$` but no unicode forms, so the
//     scan keys on the `$'` opener rather than on a quote set.
//
// Both emit the same conceal-with-stand-in spans as UnicodeSpansIn, under the
// same capture, so the editor's escape.unicode toggle governs them alike.

// UnicodeCSSSpans produces the stand-in spans for CSS character escapes:
// `\e9`, `\00e9`, `content: "\f00c"`. Only escapes naming a graphic code
// point conceal; an identity escape (`\:`, `\\`) is punctuation, not text,
// and stays raw.
func UnicodeCSSSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		for i := 0; i+1 < len(runes); i++ {
			if runes[i] != '\\' {
				continue
			}
			v, n := 0, 0
			for j := i + 1; j < len(runes) && n < 6; j++ {
				d := hexDigit(runes[j])
				if d < 0 {
					break
				}
				v, n = v<<4|d, n+1
			}
			if n == 0 {
				i++ // an identity escape: skip its payload so \\ never reopens
				continue
			}
			end := i + 1 + n
			if v == 0 || v > unicode.MaxRune ||
				utf16.IsSurrogate(rune(v)) || !unicode.IsGraphic(rune(v)) {
				i = end - 1
				continue
			}
			// A single following whitespace terminates the digit run and
			// belongs to the escape — `\e9 s` is "és", not "é s".
			if end < len(runes) && (runes[end] == ' ' || runes[end] == '\t') {
				end++
			}
			out = append(out, lang.Span{
				Line: li, StartCol: i, EndCol: end,
				Capture: UnicodeCapture, Replace: string(rune(v)),
			})
			i = end - 1
		}
	}
	return out
}

// ansiCDialect is what bash's $'…' quoting decodes: \uXXXX, \UXXXXXXXX and
// \xNN name code points; there is no brace form.
var ansiCDialect = UnicodeDialect{Hex: true, Long: true}

// UnicodeANSICSpans produces the stand-in spans for the unicode escapes
// inside shell ANSI-C quoting: `$'café'`. Plain '…' literals are raw and
// "…" processes no unicode forms, so both are skipped whole — a quote inside
// them cannot open a phantom region.
func UnicodeANSICSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		for i := 0; i < len(runes); i++ {
			switch runes[i] {
			case '\\':
				i++ // an escaped rune outside quotes ends no region
			case '"':
				for i++; i < len(runes) && runes[i] != '"'; i++ {
					if runes[i] == '\\' {
						i++
					}
				}
			case '\'':
				for i++; i < len(runes) && runes[i] != '\''; i++ {
				}
			case '$':
				if i+1 >= len(runes) || runes[i+1] != '\'' {
					continue
				}
				out, i = appendANSICSpans(out, li, runes, i+2)
			}
		}
	}
	return out
}

// appendANSICSpans scans the inside of a $'…' region from rune index i,
// returning the index of the closing quote (or the line end when the region
// never closes — decoding stops there rather than running into plain text).
func appendANSICSpans(out []lang.Span, li int, runes []rune, i int) ([]lang.Span, int) {
	for ; i < len(runes); i++ {
		switch runes[i] {
		case '\'':
			return out, i
		case '\\':
			if end, text, ok := unicodeEscapeAt(runes, i, ansiCDialect); ok {
				out = append(out, lang.Span{
					Line: li, StartCol: i, EndCol: end,
					Capture: UnicodeCapture, Replace: text,
				})
				i = end - 1
				continue
			}
			i++ // an ordinary escape (\', \n, \t): skip its payload
		}
	}
	return out, len(runes)
}
