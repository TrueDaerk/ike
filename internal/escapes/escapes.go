// Package escapes detects escaped text in a line and renders it decoded
// (#1620) — the generalization of the .http percent-decoding (#1585) to other
// escape families. Like epochtime (#1618) it is the detection half only: the
// produced spans carry a Replace stand-in, the editor's conceal layer renders
// the decoded form on lines the caret is not on, and the raw bytes reappear
// under the caret or a selection (#1594's positional reveal). Each family has
// its own capture name, so the editor gates each behind its own toggle.
//
// Three families:
//
//   - Unicode escapes: \uXXXX (with UTF-16 surrogate pairs combined) and Go's
//     \UXXXXXXXX inside single-line string or rune literals — JSON, JS/TS and
//     Go all write non-ASCII text this way. An escape outside quotes, a
//     truncated escape, a lone surrogate or a non-graphic code point stays
//     raw.
//   - HTML/XML entities: &name;, &#123; and &#x1F600;. HTML decodes the full
//     named-entity table; XML only its five predefined entities (custom
//     entities are document-defined, guessing HTML names there would lie).
//   - Base64 values where base64 is the convention: the data: block of a
//     Kubernetes Secret document in YAML. A value only decodes when the
//     payload is printable text — binary secrets stay raw.
//
// It is a leaf package — pure Go over internal/lang's span type — so every
// language's span producer can call it without a dependency cycle.
package escapes

import (
	"encoding/base64"
	"html"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"ike/internal/lang"
)

// The capture names carried by the produced spans. The editor routes spans
// with these captures into per-family conceal channels, each gated by its own
// config toggle, so one family switches off without the others.
const (
	UnicodeCapture = "escape.unicode"
	EntityCapture  = "escape.entity"
	Base64Capture  = "escape.base64"
)

// --- Unicode escapes -------------------------------------------------------

// UnicodeSpans produces conceal-with-stand-in spans for the \uXXXX and
// \UXXXXXXXX escapes in lines, ready to be appended to a language's
// lang.Language.Spans output.
func UnicodeSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		out = appendUnicodeSpans(out, li, line)
	}
	return out
}

// UnicodeLineSpans is UnicodeSpans for a single line at index li — for
// producers that scan only part of a buffer.
func UnicodeLineSpans(li int, line string) []lang.Span {
	return appendUnicodeSpans(nil, li, line)
}

// appendUnicodeSpans scans one line. Escapes only decode inside a single-line
// quoted literal (" or ') — that is where JSON, JS and Go put them, and it
// keeps regex sources and prose out. The scanner walks the quote state and
// consumes backslash escapes pairwise, so \\u0041 (an escaped backslash and
// literal text) never decodes.
func appendUnicodeSpans(out []lang.Span, li int, line string) []lang.Span {
	runes := []rune(line)
	var quote rune // the enclosing quote while inside a literal, 0 outside
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote == 0 {
			if r == '"' || r == '\'' {
				quote = r
			}
			continue
		}
		if r == quote {
			quote = 0
			continue
		}
		if r != '\\' {
			continue
		}
		if end, text, ok := unicodeEscapeAt(runes, i); ok {
			out = append(out, lang.Span{
				Line: li, StartCol: i, EndCol: end,
				Capture: UnicodeCapture, Replace: text,
			})
			i = end - 1
			continue
		}
		i++ // an ordinary escape (\\, \", \n, or a rejected \u): skip its payload
	}
	return out
}

// unicodeEscapeAt decodes the escape starting at the backslash runes[i]:
// \uXXXX (a high surrogate must pair with a following \uXXXX low surrogate —
// the pair decodes as one span) or Go's \UXXXXXXXX. ok=false for anything
// else: a truncated escape, a lone surrogate, a value beyond the Unicode
// range or a non-graphic code point all stay raw.
func unicodeEscapeAt(runes []rune, i int) (end int, text string, ok bool) {
	switch charAt(runes, i+1) {
	case 'u':
		v, ok := hexVal(runes, i+2, 4)
		if !ok {
			return 0, "", false
		}
		if utf16.IsSurrogate(rune(v)) {
			if charAt(runes, i+6) != '\\' || charAt(runes, i+7) != 'u' {
				return 0, "", false
			}
			lo, ok := hexVal(runes, i+8, 4)
			if !ok {
				return 0, "", false
			}
			r := utf16.DecodeRune(rune(v), rune(lo))
			if r == utf8.RuneError || !unicode.IsGraphic(r) {
				return 0, "", false
			}
			return i + 12, string(r), true
		}
		if !unicode.IsGraphic(rune(v)) {
			return 0, "", false
		}
		return i + 6, string(rune(v)), true
	case 'U':
		v, ok := hexVal(runes, i+2, 8)
		if !ok || v > unicode.MaxRune || utf16.IsSurrogate(rune(v)) || !unicode.IsGraphic(rune(v)) {
			return 0, "", false
		}
		return i + 10, string(rune(v)), true
	}
	return 0, "", false
}

// hexVal parses exactly n hex digits at runes[from:], ok=false when the line
// ends early or a non-hex rune appears.
func hexVal(runes []rune, from, n int) (int, bool) {
	if from+n > len(runes) {
		return 0, false
	}
	v := 0
	for _, r := range runes[from : from+n] {
		d := hexDigit(r)
		if d < 0 {
			return 0, false
		}
		v = v<<4 | d
	}
	return v, true
}

func hexDigit(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10
	}
	return -1
}

// --- HTML/XML entities -----------------------------------------------------

// EntitySet selects which named entities decode.
type EntitySet int

const (
	// EntityHTML decodes the full HTML named-entity table plus numeric
	// references.
	EntityHTML EntitySet = iota
	// EntityXML decodes only XML's five predefined entities (amp, lt, gt,
	// quot, apos) plus numeric references — other names are document-defined
	// in XML, so decoding them by the HTML table would guess.
	EntityXML
)

// xmlEntities are XML's predefined entities — the only named ones every XML
// document defines.
var xmlEntities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": `"`, "apos": "'",
}

// EntitySpans produces conceal-with-stand-in spans for the character
// references in lines: &name;, &#123; and &#x1F600;.
func EntitySpans(lines []string, set EntitySet) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		out = appendEntitySpans(out, li, line, set)
	}
	return out
}

// EntityLineSpans is EntitySpans for a single line at index li.
func EntityLineSpans(li int, line string, set EntitySet) []lang.Span {
	return appendEntitySpans(nil, li, line, set)
}

// maxEntityBody bounds the scan for the closing ";" — the longest HTML entity
// name is 31 characters ("CounterClockwiseContourIntegral").
const maxEntityBody = 32

func appendEntitySpans(out []lang.Span, li int, line string, set EntitySet) []lang.Span {
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '&' {
			continue
		}
		j := -1
		for k := i + 1; k < len(runes) && k <= i+maxEntityBody; k++ {
			if runes[k] == ';' {
				j = k
				break
			}
			if !entityBodyRune(runes[k]) {
				break
			}
		}
		if j <= i+1 {
			continue
		}
		text, ok := decodeEntity(string(runes[i+1:j]), set)
		if !ok {
			continue
		}
		out = append(out, lang.Span{
			Line: li, StartCol: i, EndCol: j + 1,
			Capture: EntityCapture, Replace: text,
		})
		i = j
	}
	return out
}

// entityBodyRune reports a rune that may appear between "&" and ";".
func entityBodyRune(r rune) bool {
	return r == '#' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// decodeEntity decodes one reference body (the text between "&" and ";").
// Numeric references parse directly; named ones resolve by the set's table.
// Anything decoding to a non-graphic code point stays raw — a stand-in that
// renders as nothing (or tears the terminal) would hide that the reference is
// there at all.
func decodeEntity(body string, set EntitySet) (string, bool) {
	if strings.HasPrefix(body, "#") {
		n, base := body[1:], 10
		if len(n) > 0 && (n[0] == 'x' || n[0] == 'X') {
			n, base = n[1:], 16
		}
		if n == "" {
			return "", false
		}
		v, err := strconv.ParseUint(n, base, 32)
		if err != nil || v > uint64(unicode.MaxRune) {
			return "", false
		}
		r := rune(v)
		if utf16.IsSurrogate(r) || !unicode.IsGraphic(r) {
			return "", false
		}
		return string(r), true
	}
	if set == EntityXML {
		text, ok := xmlEntities[body]
		return text, ok
	}
	ref := "&" + body + ";"
	decoded := html.UnescapeString(ref)
	if decoded == ref {
		return "", false
	}
	// UnescapeString also resolves prefix matches ("&notit;" → "¬it;", HTML's
	// text-mode behaviour). Only a fully consumed reference conceals: a
	// leftover always keeps the trailing ";" (&semi;, which IS ";", excepted).
	if len(decoded) > 1 && strings.ContainsRune(decoded, ';') {
		return "", false
	}
	for _, r := range decoded {
		if !unicode.IsGraphic(r) {
			return "", false
		}
	}
	return decoded, true
}

// --- base64 in YAML --------------------------------------------------------

// Base64YAMLSpans produces conceal-with-stand-in spans for the base64 values
// in contexts where base64 is the convention: the data: block of a Kubernetes
// Secret document (kind: Secret). Only values whose decoded payload is
// printable single-line text conceal — binary secrets and anything that does
// not round-trip as clean UTF-8 stay raw. stringData: blocks hold plaintext
// by definition and are never touched.
func Base64YAMLSpans(lines []string) []lang.Span {
	var out []lang.Span
	start := 0
	for i := 0; i <= len(lines); i++ {
		if i < len(lines) && strings.TrimRight(lines[i], " \t") != "---" {
			continue
		}
		out = appendSecretSpans(out, lines, start, i)
		start = i + 1
	}
	return out
}

// appendSecretSpans scans one YAML document, lines[start:end).
func appendSecretSpans(out []lang.Span, lines []string, start, end int) []lang.Span {
	if !isSecretDoc(lines[start:end]) {
		return out
	}
	for i := start; i < end; i++ {
		if strings.TrimSpace(lines[i]) != "data:" {
			continue
		}
		ind := indentOf(lines[i])
		for j := i + 1; j < end; j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			if indentOf(lines[j]) <= ind {
				i = j - 1
				break
			}
			out = appendDataEntry(out, j, lines[j])
		}
	}
	return out
}

// isSecretDoc reports whether the document declares kind: Secret at the top
// level.
func isSecretDoc(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, "kind:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "kind:"))
			return strings.Trim(v, `"'`) == "Secret"
		}
	}
	return false
}

// indentOf counts the leading blank columns (a tab counts one — YAML forbids
// tabs in indentation anyway, and relative comparison is all that is needed).
func indentOf(line string) int {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return len(line)
}

// appendDataEntry decodes one "key: value" mapping line inside a data: block.
// The span covers the raw value token — quotes included when the scalar is
// quoted — so the whole token reads as the decoded text.
func appendDataEntry(out []lang.Span, li int, line string) []lang.Span {
	runes := []rune(line)
	colon := -1
	for i, r := range runes {
		if r == ':' {
			colon = i
			break
		}
	}
	if colon < 0 {
		return out
	}
	from := colon + 1
	for from < len(runes) && (runes[from] == ' ' || runes[from] == '\t') {
		from++
	}
	to := len(runes)
	// Drop a trailing comment — YAML only starts one after blank space.
	for i := from; i+1 < len(runes); i++ {
		if (runes[i] == ' ' || runes[i] == '\t') && runes[i+1] == '#' {
			to = i
			break
		}
	}
	for to > from && (runes[to-1] == ' ' || runes[to-1] == '\t') {
		to--
	}
	if to <= from {
		return out
	}
	val := string(runes[from:to])
	if q := val[0]; (q == '"' || q == '\'') && len(val) > 1 && val[len(val)-1] == q {
		val = val[1 : len(val)-1]
	}
	text, ok := decodeBase64(val)
	if !ok {
		return out
	}
	return append(out, lang.Span{
		Line: li, StartCol: from, EndCol: to,
		Capture: Base64Capture, Replace: text,
	})
}

// decodeBase64 decodes a standard-alphabet, padded base64 scalar whose
// payload is printable single-line UTF-8 text. One trailing newline is
// forgiven — `echo secret | base64` puts it there.
func decodeBase64(s string) (string, bool) {
	if len(s) < 4 || len(s)%4 != 0 {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || !utf8.Valid(raw) {
		return "", false
	}
	text := strings.TrimSuffix(string(raw), "\n")
	if text == "" {
		return "", false
	}
	for _, r := range text {
		if !unicode.IsGraphic(r) {
			return "", false
		}
	}
	return text, true
}

// charAt returns the rune at i, or 0 outside the line.
func charAt(runes []rune, i int) rune {
	if i < 0 || i >= len(runes) {
		return 0
	}
	return runes[i]
}
