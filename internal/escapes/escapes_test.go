package escapes

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

// one asserts a single span with the given range and stand-in.
func one(t *testing.T, spans []lang.Span, start, end int, text, capture string) {
	t.Helper()
	if len(spans) != 1 {
		t.Fatalf("got %d spans (%v), want 1", len(spans), spans)
	}
	s := spans[0]
	if s.StartCol != start || s.EndCol != end || s.Replace != text || s.Capture != capture {
		t.Errorf("span = [%d,%d) %q capture %q, want [%d,%d) %q capture %q",
			s.StartCol, s.EndCol, s.Replace, s.Capture, start, end, text, capture)
	}
}

// --- unicode escapes -------------------------------------------------------

func TestUnicodeBasicEscape(t *testing.T) {
	spans := UnicodeLineSpans(0, `{"name": "\u00e4"}`)
	one(t, spans, 10, 16, "ä", UnicodeCapture)
}

func TestUnicodeSurrogatePair(t *testing.T) {
	spans := UnicodeLineSpans(0, `"\ud83d\ude00"`)
	one(t, spans, 1, 13, "😀", UnicodeCapture)
}

func TestUnicodeGoLongEscape(t *testing.T) {
	spans := UnicodeLineSpans(0, `s := "\U0001F600"`)
	one(t, spans, 6, 16, "😀", UnicodeCapture)
}

func TestUnicodeRuneLiteral(t *testing.T) {
	spans := UnicodeLineSpans(0, `r := '\u00e4'`)
	one(t, spans, 6, 12, "ä", UnicodeCapture)
}

func TestUnicodeRejects(t *testing.T) {
	for name, line := range map[string]string{
		"outside quotes":       `\u0041 bare`,
		"truncated":            `"\u00e"`,
		"bad hex":              `"\u00zz"`,
		"escaped backslash":    `"\\u0041"`,
		"lone high surrogate":  `"\ud83d x"`,
		"lone low surrogate":   `"\ude00"`,
		"surrogate then bad":   `"\ud83dA"`,
		"non-graphic control":  `"\u0007"`,
		"non-graphic zwj":      `"\u200d"`,
		"U beyond max rune":    `"\U00110000"`,
		"U surrogate":          `"\U0000D800"`,
		"line ends mid-escape": `"\u00`,
	} {
		if spans := UnicodeLineSpans(0, line); len(spans) != 0 {
			t.Errorf("%s: %q produced %v, want none", name, line, spans)
		}
	}
}

func TestUnicodeMultiplePerLine(t *testing.T) {
	spans := UnicodeLineSpans(0, `"\u00e4\u00f6"`)
	if len(spans) != 2 || spans[0].Replace != "ä" || spans[1].Replace != "ö" {
		t.Fatalf("spans = %v, want ä and ö", spans)
	}
	if spans[0].EndCol != spans[1].StartCol {
		t.Errorf("adjacent escapes must abut: %v", spans)
	}
}

func TestUnicodeSpansCarriesLine(t *testing.T) {
	spans := UnicodeSpans([]string{`x`, `"\u0041"`})
	one(t, spans, 1, 7, "A", UnicodeCapture)
	if spans[0].Line != 1 {
		t.Errorf("Line = %d, want 1", spans[0].Line)
	}
}

// --- entities --------------------------------------------------------------

func TestEntityNamedHTML(t *testing.T) {
	spans := EntityLineSpans(0, `a &amp; b`, EntityHTML)
	one(t, spans, 2, 7, "&", EntityCapture)
}

func TestEntityUmlaut(t *testing.T) {
	spans := EntityLineSpans(0, `M&auml;rz`, EntityHTML)
	one(t, spans, 1, 7, "ä", EntityCapture)
}

func TestEntityNumericDecimal(t *testing.T) {
	spans := EntityLineSpans(0, `&#8230;`, EntityHTML)
	one(t, spans, 0, 7, "…", EntityCapture)
}

func TestEntityNumericHex(t *testing.T) {
	spans := EntityLineSpans(0, `&#x2026;`, EntityHTML)
	one(t, spans, 0, 8, "…", EntityCapture)
}

func TestEntityXMLPredefined(t *testing.T) {
	spans := EntityLineSpans(0, `&lt;tag&gt;`, EntityXML)
	if len(spans) != 2 || spans[0].Replace != "<" || spans[1].Replace != ">" {
		t.Fatalf("spans = %v, want < and >", spans)
	}
}

func TestEntityXMLRejectsHTMLNames(t *testing.T) {
	if spans := EntityLineSpans(0, `&auml; &nbsp;`, EntityXML); len(spans) != 0 {
		t.Errorf("XML must not decode HTML-only names, got %v", spans)
	}
}

func TestEntityRejects(t *testing.T) {
	for name, line := range map[string]string{
		"unknown name":      `&bogus;`,
		"no semicolon":      `&amp `,
		"empty":             `&;`,
		"bare ampersand":    `a & b`,
		"numeric empty":     `&#;`,
		"numeric hex empty": `&#x;`,
		"numeric surrogate": `&#xD800;`,
		"numeric too big":   `&#x110000;`,
		"non-graphic zwj":   `&#x200d;`,
		"prefix match only": `&notit;`, // HTML prefix rules would give "¬it;"
	} {
		if spans := EntityLineSpans(0, line, EntityHTML); len(spans) != 0 {
			t.Errorf("%s: %q produced %v, want none", name, line, spans)
		}
	}
}

func TestEntitySemi(t *testing.T) {
	// &semi; decodes to ";" itself — the one legitimate decoded ";".
	spans := EntityLineSpans(0, `&semi;`, EntityHTML)
	one(t, spans, 0, 6, ";", EntityCapture)
}

// --- base64 in YAML --------------------------------------------------------

const secretDoc = `apiVersion: v1
kind: Secret
metadata:
  name: creds
data:
  username: YWRtaW4=
  password: cGFzc3dvcmQK
stringData:
  note: plain
`

func TestBase64SecretData(t *testing.T) {
	spans := Base64YAMLSpans(strings.Split(secretDoc, "\n"))
	if len(spans) != 2 {
		t.Fatalf("spans = %v, want 2", spans)
	}
	if spans[0].Line != 5 || spans[0].Replace != "admin" {
		t.Errorf("first span = %+v, want line 5 decoding to admin", spans[0])
	}
	// The trailing newline `echo` leaves is forgiven.
	if spans[1].Line != 6 || spans[1].Replace != "password" {
		t.Errorf("second span = %+v, want line 6 decoding to password", spans[1])
	}
	if spans[0].StartCol != 12 || spans[0].EndCol != 20 {
		t.Errorf("first span range = [%d,%d), want [12,20)", spans[0].StartCol, spans[0].EndCol)
	}
}

func TestBase64NonSecretStaysRaw(t *testing.T) {
	doc := strings.ReplaceAll(secretDoc, "kind: Secret", "kind: ConfigMap")
	if spans := Base64YAMLSpans(strings.Split(doc, "\n")); len(spans) != 0 {
		t.Errorf("non-Secret document decoded: %v", spans)
	}
}

func TestBase64MultiDoc(t *testing.T) {
	doc := "kind: ConfigMap\ndata:\n  a: YWRtaW4=\n---\n" + secretDoc
	spans := Base64YAMLSpans(strings.Split(doc, "\n"))
	if len(spans) != 2 {
		t.Fatalf("spans = %v, want only the Secret document's 2", spans)
	}
	if spans[0].Line != 9 {
		t.Errorf("first span line = %d, want 9 (offset past the first doc)", spans[0].Line)
	}
}

func TestBase64Rejects(t *testing.T) {
	for name, val := range map[string]string{
		"binary payload":  "AAEC/w==", // decodes to non-printable bytes
		"invalid base64":  "not-base64!",
		"unpadded length": "YWRtaW4",  // len % 4 != 0
		"multiline text":  "YQpiCmMK", // "a\nb\nc\n" — inner newlines stay raw
		"empty payload":   "Cg==",     // just a newline
		"invalid utf8":    "/////w==",
	} {
		doc := "kind: Secret\ndata:\n  k: " + val + "\n"
		if spans := Base64YAMLSpans(strings.Split(doc, "\n")); len(spans) != 0 {
			t.Errorf("%s: %q produced %v, want none", name, val, spans)
		}
	}
}

func TestBase64QuotedValue(t *testing.T) {
	doc := "kind: Secret\ndata:\n  k: \"YWRtaW4=\"\n"
	spans := Base64YAMLSpans(strings.Split(doc, "\n"))
	if len(spans) != 1 || spans[0].Replace != "admin" {
		t.Fatalf("spans = %v, want quoted value decoded to admin", spans)
	}
	// The span covers the whole token, quotes included.
	if spans[0].StartCol != 5 || spans[0].EndCol != 15 {
		t.Errorf("range = [%d,%d), want [5,15)", spans[0].StartCol, spans[0].EndCol)
	}
}

func TestBase64TrailingCommentStripped(t *testing.T) {
	doc := "kind: Secret\ndata:\n  k: YWRtaW4= # the admin user\n"
	spans := Base64YAMLSpans(strings.Split(doc, "\n"))
	if len(spans) != 1 || spans[0].Replace != "admin" || spans[0].EndCol != 13 {
		t.Fatalf("spans = %v, want value before the comment decoded", spans)
	}
}

func TestBase64BlockEndsAtDedent(t *testing.T) {
	doc := "kind: Secret\ndata:\n  k: YWRtaW4=\ntype: Opaque\nother: YWRtaW4=\n"
	spans := Base64YAMLSpans(strings.Split(doc, "\n"))
	if len(spans) != 1 || spans[0].Line != 2 {
		t.Fatalf("spans = %v, want only the data: entry", spans)
	}
}

// --- dialects and the additional escape forms (#2334) ----------------------

// TestUnicodeBraceEscape: the ES6/PHP \u{X…} form decodes where the dialect
// has it (JS/TS, PHP) and stays raw where it does not (Go, JSON, Python, TOML).
func TestUnicodeBraceEscape(t *testing.T) {
	one(t, UnicodeLineSpansIn(0, `s = "\u{1F600}"`, UnicodeScript), 5, 14, "😀", UnicodeCapture)
	one(t, UnicodeLineSpansIn(0, `$s = "\u{e4}";`, UnicodePHP), 6, 12, "ä", UnicodeCapture)
	for name, d := range map[string]UnicodeDialect{
		"go": UnicodeGo, "json": UnicodeJSON, "python": UnicodePython, "toml": UnicodeTOML,
	} {
		if spans := UnicodeLineSpansIn(0, `"\u{1F600}"`, d); len(spans) != 0 {
			t.Errorf("%s: \\u{…} produced %v, want none", name, spans)
		}
	}
}

// TestUnicodeBraceRejects: an empty, over-long, unterminated, out-of-range,
// surrogate or non-graphic brace body stays raw.
func TestUnicodeBraceRejects(t *testing.T) {
	for name, line := range map[string]string{
		"empty":        `"\u{}"`,
		"seven digits": `"\u{00001F600}"`,
		"unterminated": `"\u{1F600"`,
		"bad hex":      `"\u{zz}"`,
		"beyond max":   `"\u{110000}"`,
		"surrogate":    `"\u{D800}"`,
		"non-graphic":  `"\u{7}"`,
	} {
		if spans := UnicodeLineSpansIn(0, line, UnicodeScript); len(spans) != 0 {
			t.Errorf("%s: %q produced %v, want none", name, line, spans)
		}
	}
}

// TestUnicodeHexEscape: \xNN decodes only where it names a code point
// (Python, JS/TS, YAML) — in Go and PHP it is a raw byte, so "\xc3\xbc" is one
// character in two escapes and decoding each alone would render "Ã¼".
func TestUnicodeHexEscape(t *testing.T) {
	one(t, UnicodeLineSpansIn(0, `x = "\xfc"`, UnicodePython), 5, 9, "ü", UnicodeCapture)
	one(t, UnicodeLineSpansIn(0, `k: "\xfc"`, UnicodeYAML), 4, 8, "ü", UnicodeCapture)
	for name, d := range map[string]UnicodeDialect{
		"go": UnicodeGo, "php": UnicodePHP, "json": UnicodeJSON, "toml": UnicodeTOML,
	} {
		if spans := UnicodeLineSpansIn(0, `"\xfc"`, d); len(spans) != 0 {
			t.Errorf("%s: \\xNN produced %v, want none", name, spans)
		}
	}
}

// TestUnicodeHexRejects: a truncated or non-graphic \xNN stays raw.
func TestUnicodeHexRejects(t *testing.T) {
	for name, line := range map[string]string{
		"one digit":  `"\xf"`,
		"bad hex":    `"\xzz"`,
		"escape seq": `"\x1b[0m"`,
		"line ends":  `"\x`,
	} {
		if spans := UnicodeLineSpansIn(0, line, UnicodePython); len(spans) != 0 {
			t.Errorf("%s: %q produced %v, want none", name, line, spans)
		}
	}
}

// TestUnicodeRawQuotes: a quote the dialect calls raw opens a literal that
// decodes nothing, and the literal is skipped whole so a `"` inside it cannot
// open a phantom escape-processing literal.
func TestUnicodeRawQuotes(t *testing.T) {
	for name, tc := range map[string]struct {
		line string
		d    UnicodeDialect
	}{
		"php single":        {`$s = '\u00fc';`, UnicodePHP},
		"php nested dquote": {`$s = 'say "\u00fc"';`, UnicodePHP},
		"php escaped quote": {`$s = 'it\'s \u00fc';`, UnicodePHP},
		"toml literal":      {`s = '\u00fc'`, UnicodeTOML},
		"yaml single":       {`k: '\u00fc'`, UnicodeYAML},
		"go raw string":     {"s := `\\u00fc`", UnicodeGo},
	} {
		if spans := UnicodeLineSpansIn(0, tc.line, tc.d); len(spans) != 0 {
			t.Errorf("%s: %q produced %v, want none", name, tc.line, spans)
		}
	}
}

// TestUnicodePythonRawPrefix: an r/b string prefix turns the literal raw — in
// r"…" a backslash is literal text, and in b"…" \u is not an escape at all.
func TestUnicodePythonRawPrefix(t *testing.T) {
	for name, line := range map[string]string{
		"r prefix":     `x = r"\u00fc"`,
		"R prefix":     `x = R"\u00fc"`,
		"b prefix":     `x = b"\u00fc"`,
		"rb prefix":    `x = rb"\u00fc"`,
		"br prefix":    `x = br"\u00fc"`,
		"rf prefix":    `x = rf"\u00fc"`,
		"single quote": `x = r'\u00fc'`,
	} {
		if spans := UnicodeLineSpansIn(0, line, UnicodePython); len(spans) != 0 {
			t.Errorf("%s: %q produced %v, want none", name, line, spans)
		}
	}
}

// TestUnicodePythonNonRawPrefix: a bare f or u prefix still processes escapes,
// and a prefix letter that only ends a longer identifier is not a prefix.
func TestUnicodePythonNonRawPrefix(t *testing.T) {
	for _, tc := range []struct {
		name       string
		line       string
		start, end int
	}{
		{"f prefix", `x = f"\u00fc"`, 6, 12},
		{"u prefix", `x = u"\u00fc"`, 6, 12},
		{"identifier tail", `for"\u00fc"`, 4, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			one(t, UnicodeLineSpansIn(0, tc.line, UnicodePython), tc.start, tc.end, "ü", UnicodeCapture)
		})
	}
}

// TestUnicodePythonRawPrefixResumes: text after a raw literal keeps decoding —
// the skip ends at the closing quote, it does not swallow the rest of the line.
func TestUnicodePythonRawPrefixResumes(t *testing.T) {
	one(t, UnicodeLineSpansIn(0, `x = r"\u00fc" + "\u00e4"`, UnicodePython), 17, 23, "ä", UnicodeCapture)
}

// TestUnicodeScriptTemplate: a JS template literal processes escapes like the
// other two quotes.
func TestUnicodeScriptTemplate(t *testing.T) {
	one(t, UnicodeLineSpansIn(0, "const s = `\\u00e4`", UnicodeScript), 11, 17, "ä", UnicodeCapture)
}

// TestUnicodeNamedEscapeStaysRaw: Python's \N{NAME} is not decoded — the Go
// standard library carries no Unicode name table (namedEscapeNote).
func TestUnicodeNamedEscapeStaysRaw(t *testing.T) {
	line := `x = "\N{LATIN SMALL LETTER U WITH DIAERESIS}"`
	if spans := UnicodeLineSpansIn(0, line, UnicodePython); len(spans) != 0 {
		t.Errorf("%q produced %v, want none (%s)", line, spans, namedEscapeNote)
	}
}

// TestUnicodeUnterminatedRawLiteral: a raw literal that never closes leaves
// the rest of the line unscanned rather than decoding inside it.
func TestUnicodeUnterminatedRawLiteral(t *testing.T) {
	if spans := UnicodeLineSpansIn(0, `$s = 'oops \u00fc`, UnicodePHP); len(spans) != 0 {
		t.Errorf("unterminated raw literal produced %v, want none", spans)
	}
}
