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
//   - Unicode escapes: \uXXXX (with UTF-16 surrogate pairs combined),
//     \UXXXXXXXX, \u{X…} and \xNN inside single-line string or rune literals
//     — Go, JS/TS, JSON, Python, PHP, YAML and TOML all write non-ASCII text
//     this way. Which forms and which quotes count is per language, carried
//     by a UnicodeDialect. An escape outside quotes, a truncated escape, a
//     lone surrogate or a non-graphic code point stays raw.
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

// UnicodeDialect describes how one language writes escapes, so the otherwise
// language-neutral scanner does not decode text a language leaves raw. Every
// language ships its own value below; the zero value decodes nothing.
//
// Quotes split into two sets because most languages have one quote that
// processes escapes and one that does not: PHP's '…', YAML's '…' and TOML's
// literal '…' are all raw, and so is Go's `…`. A raw literal is skipped whole
// rather than ignored, so a `"` inside it cannot open a phantom literal.
type UnicodeDialect struct {
	// EscapeQuotes are the quote runes opening a literal whose backslash
	// escapes are processed.
	EscapeQuotes string
	// RawQuotes are the quote runes opening a literal without escape
	// processing.
	RawQuotes string
	// RawEscapesQuote reports that a backslash inside a raw literal still
	// escapes the following rune, so it cannot end the literal — true for
	// PHP's '…' and Python's r"…", false for Go's `…`, YAML's '…' and TOML's
	// '…'.
	RawEscapesQuote bool
	// StringPrefixes enables Python's literal prefixes: an r/R/b/B prefix on
	// a literal (r"…", rb"…", b"…") turns the whole literal raw — in a raw
	// string a backslash is literal text, and in a bytes literal \u is not an
	// escape at all.
	StringPrefixes bool
	// Brace enables the ES6/PHP \u{X…} form, one to six hex digits.
	Brace bool
	// Long records the eight-digit \UXXXXXXXX form — Go, Python, YAML and
	// TOML have it, JS/TS, JSON and PHP do not. Only the *encoder*
	// (encode.go) reads it, to pick the spelling for a code point above the
	// BMP; decoding stays deliberately lenient and resolves a \U wherever it
	// finds one, since a file may well have been escaped by another tool.
	Long bool
	// Hex enables \xNN, and only where it names a code point (Python, JS/TS,
	// YAML). In Go and PHP a \xNN is a raw byte, so "\xc3\xbc" is one ü in
	// two escapes — decoding them one by one would render "Ã¼" and lie.
	Hex bool
}

// The per-language dialects. Rule of thumb: a quote is an escape quote only
// when the language's own spec says backslash escapes are processed inside
// it, and a form is enabled only when the language has it.
var (
	// UnicodeGo: "…" strings and '…' runes take \uXXXX and \UXXXXXXXX;
	// `…` is raw; \xNN is a byte.
	UnicodeGo = UnicodeDialect{EscapeQuotes: `"'`, RawQuotes: "`", Long: true}
	// UnicodeJSON: only "…" exists, and only \uXXXX. The single quote stays
	// an escape quote for the JSONC/JSON5 dialects that allow it.
	UnicodeJSON = UnicodeDialect{EscapeQuotes: `"'`}
	// UnicodeScript (JS/TS): "…", '…' and `…` templates all process escapes,
	// and ES6 added \u{X…}; \xNN is a code point.
	UnicodeScript = UnicodeDialect{EscapeQuotes: "\"'`", Brace: true, Hex: true}
	// UnicodePython: "…" and '…' process escapes, r/b prefixes turn a literal
	// raw, \xNN is a code point. \N{NAME} is deliberately absent — see
	// namedEscapeNote.
	UnicodePython = UnicodeDialect{
		EscapeQuotes: `"'`, RawEscapesQuote: true, StringPrefixes: true,
		Hex: true, Long: true,
	}
	// UnicodePHP: only "…" (and heredocs, which are multi-line and thus out
	// of reach) processes escapes; '…' does not, though a backslash there
	// still escapes the closing quote. \u{X…} arrived in PHP 7.0; \xNN is a
	// byte.
	UnicodePHP = UnicodeDialect{
		EscapeQuotes: `"`, RawQuotes: `'`, RawEscapesQuote: true, Brace: true,
	}
	// UnicodeYAML: only the double-quoted scalar processes escapes; the
	// single-quoted one escapes nothing (it doubles '' instead). YAML's \xNN
	// names a code point.
	UnicodeYAML = UnicodeDialect{EscapeQuotes: `"`, RawQuotes: `'`, Hex: true, Long: true}
	// UnicodeTOML: basic strings "…" take \uXXXX and \UXXXXXXXX; literal
	// strings '…' take nothing, and TOML 1.0 has no \xNN.
	UnicodeTOML = UnicodeDialect{EscapeQuotes: `"`, RawQuotes: `'`, Long: true}
)

// namedEscapeNote records why Python's \N{LATIN SMALL LETTER A} stays raw:
// resolving it needs the Unicode character-name table, which the Go standard
// library does not carry (package unicode has no name lookup). Shipping the
// ~34k names to decode a form that is rare in real code — it exists to make a
// literal self-documenting, i.e. exactly where the raw text is the point — is
// not worth the binary size.
const namedEscapeNote = `\N{NAME} stays raw: no Unicode name table in the standard library`

// UnicodeSpans produces conceal-with-stand-in spans for the unicode escapes
// in lines, ready to be appended to a language's lang.Language.Spans output.
// It scans in the Go dialect; UnicodeSpansIn takes the language's own.
func UnicodeSpans(lines []string) []lang.Span {
	return UnicodeSpansIn(lines, UnicodeGo)
}

// UnicodeSpansIn is UnicodeSpans in the dialect d.
func UnicodeSpansIn(lines []string, d UnicodeDialect) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		out = appendUnicodeSpans(out, li, line, d)
	}
	return out
}

// UnicodeLineSpans is UnicodeSpans for a single line at index li — for
// producers that scan only part of a buffer.
func UnicodeLineSpans(li int, line string) []lang.Span {
	return appendUnicodeSpans(nil, li, line, UnicodeGo)
}

// UnicodeLineSpansIn is UnicodeLineSpans in the dialect d.
func UnicodeLineSpansIn(li int, line string, d UnicodeDialect) []lang.Span {
	return appendUnicodeSpans(nil, li, line, d)
}

// appendUnicodeSpans scans one line. Escapes only decode inside a single-line
// quoted literal that the dialect says processes escapes — that is where the
// languages put them, and it keeps regex sources and prose out. The scanner
// walks the quote state and consumes backslash escapes pairwise, so \\u0041
// (an escaped backslash and literal text) never decodes. Multi-line literals
// (Python's triple quotes, PHP heredocs, YAML block scalars) stay out of
// scope: the hook sees one line at a time and cannot tell an opener from a
// closer.
func appendUnicodeSpans(out []lang.Span, li int, line string, d UnicodeDialect) []lang.Span {
	runes := []rune(line)
	var quote rune // the enclosing escape-processing quote, 0 outside
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote == 0 {
			switch {
			case strings.ContainsRune(d.RawQuotes, r):
				i = skipRawLiteral(runes, i, d.RawEscapesQuote)
			case strings.ContainsRune(d.EscapeQuotes, r):
				if d.StringPrefixes && rawPrefixed(runes, i) {
					// r"…" / b"…" / rb"…": literal text, never a decode.
					i = skipRawLiteral(runes, i, true)
					continue
				}
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
		if end, text, ok := unicodeEscapeAt(runes, i, d); ok {
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

// skipRawLiteral returns the index of the closing quote of the raw literal
// opened at runes[open] — or the end of the line when it never closes, which
// leaves the rest unscanned rather than decoding inside an unterminated
// literal. escaping says a backslash still escapes the next rune, so a \'
// cannot close the literal.
func skipRawLiteral(runes []rune, open int, escaping bool) int {
	q := runes[open]
	for i := open + 1; i < len(runes); i++ {
		if escaping && runes[i] == '\\' {
			i++
			continue
		}
		if runes[i] == q {
			return i
		}
	}
	return len(runes)
}

// rawPrefixed reports whether the literal opening at runes[i] carries a
// Python r/b prefix (r, R, b, B and the two-letter mixes rb, br, rf, fr…). An
// f or u prefix alone still processes escapes, so only a prefix run
// containing r or b counts, and the run must start a token — the quote in
// `for"…"` does not follow a prefix.
func rawPrefixed(runes []rune, i int) bool {
	start := i
	for start > 0 && isPrefixLetter(runes[start-1]) {
		start--
	}
	if start == i || i-start > 2 {
		return false
	}
	if start > 0 && isIdentRune(runes[start-1]) {
		return false
	}
	for _, r := range runes[start:i] {
		switch r {
		case 'r', 'R', 'b', 'B':
			return true
		}
	}
	return false
}

// isPrefixLetter reports a letter that may appear in a Python string prefix.
func isPrefixLetter(r rune) bool {
	switch r {
	case 'r', 'R', 'b', 'B', 'f', 'F', 'u', 'U':
		return true
	}
	return false
}

// isIdentRune reports a rune that continues an identifier.
func isIdentRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' ||
		r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// unicodeEscapeAt decodes the escape starting at the backslash runes[i]:
// \uXXXX (a high surrogate must pair with a following \uXXXX low surrogate —
// the pair decodes as one span), Go's \UXXXXXXXX, and, where the dialect has
// them, \u{X…} and \xNN. ok=false for anything else: a truncated escape, a
// lone surrogate, a value beyond the Unicode range or a non-graphic code
// point all stay raw.
func unicodeEscapeAt(runes []rune, i int, d UnicodeDialect) (end int, text string, ok bool) {
	switch charAt(runes, i+1) {
	case 'u':
		if d.Brace && charAt(runes, i+2) == '{' {
			return braceEscapeAt(runes, i)
		}
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
	case 'x':
		if !d.Hex {
			return 0, "", false
		}
		v, ok := hexVal(runes, i+2, 2)
		if !ok || !unicode.IsGraphic(rune(v)) {
			return 0, "", false
		}
		return i + 4, string(rune(v)), true
	}
	return 0, "", false
}

// braceEscapeAt decodes \u{X…} at the backslash runes[i]: one to six hex
// digits closed by "}". An empty, over-long, out-of-range, surrogate or
// non-graphic value stays raw.
func braceEscapeAt(runes []rune, i int) (end int, text string, ok bool) {
	v, n := 0, 0
	for j := i + 3; j < len(runes); j++ {
		if runes[j] == '}' {
			if n == 0 || v > unicode.MaxRune ||
				utf16.IsSurrogate(rune(v)) || !unicode.IsGraphic(rune(v)) {
				return 0, "", false
			}
			return j + 1, string(rune(v)), true
		}
		dg := hexDigit(runes[j])
		if dg < 0 || n == 6 {
			return 0, "", false
		}
		v, n = v<<4|dg, n+1
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
