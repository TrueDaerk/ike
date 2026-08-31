package escapes

import "testing"

// encode_test.go covers the writing direction of the unicode family (#2338):
// the per-dialect encoder, its inverse, and the literal lookup the editor's
// caret fallback uses.

// bs is one backslash. Escape forms are built from it instead of being written
// out, because a literal "ü" in an interpreted Go string is the character
// itself — the tables below would then test nothing.
const bs = "\x5c"

// The escape spellings under test, per form.
var (
	uUmlaut  = bs + "u00fc"     // \uXXXX
	uLong    = bs + "U0001f600" // \UXXXXXXXX
	uBrace   = bs + "u{1f600}"  // \u{X…}
	uSurr    = bs + "ud83d" + bs + "ude00"
	uHexE    = bs + "xe9" // \xNN
	grinning = "\U0001F600"
)

func TestEncodeUnicodePerDialect(t *testing.T) {
	tests := []struct {
		name    string
		dialect UnicodeDialect
		in, out string
	}{
		{"go bmp", UnicodeGo, "über", uUmlaut + "ber"},
		{"json bmp", UnicodeJSON, "über", uUmlaut + "ber"},
		{"script bmp", UnicodeScript, "über", uUmlaut + "ber"},
		{"python bmp", UnicodePython, "über", uUmlaut + "ber"},
		{"yaml bmp", UnicodeYAML, "über", uUmlaut + "ber"},
		{"toml bmp", UnicodeTOML, "über", uUmlaut + "ber"},
		{"php bmp", UnicodePHP, "über", uUmlaut + "ber"},
		// Above the BMP the dialects part ways: the long form where the
		// language has it, braces where it has those, a surrogate pair where
		// it has neither.
		{"go astral", UnicodeGo, "ok " + grinning, "ok " + uLong},
		{"python astral", UnicodePython, "ok " + grinning, "ok " + uLong},
		{"yaml astral", UnicodeYAML, "ok " + grinning, "ok " + uLong},
		{"toml astral", UnicodeTOML, "ok " + grinning, "ok " + uLong},
		{"script astral", UnicodeScript, "ok " + grinning, "ok " + uBrace},
		{"php astral", UnicodePHP, "ok " + grinning, "ok " + uBrace},
		{"json astral", UnicodeJSON, "ok " + grinning, "ok " + uSurr},
		{"fallback bmp", UnicodeFallback, "über", uUmlaut + "ber"},
		{"fallback astral", UnicodeFallback, grinning, uSurr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EncodeUnicode(tc.in, tc.dialect); got != tc.out {
				t.Fatalf("EncodeUnicode(%q) = %q, want %q", tc.in, got, tc.out)
			}
		})
	}
}

// TestEncodeLeavesASCIIAlone: an already-escaped sequence is plain ASCII, so
// escaping never escapes it a second time — the acceptance criterion that an
// escaped backslash and an existing escape both survive untouched. Newlines
// and tabs stay too: a selection spans lines, and rewriting its structure is
// not what was asked.
func TestEncodeLeavesASCIIAlone(t *testing.T) {
	for _, in := range []string{
		uUmlaut, bs + uUmlaut, "plain ascii", "tab\tand\nnewline", `"quoted"`, "",
	} {
		if got := EncodeUnicode(in, UnicodeGo); got != in {
			t.Errorf("EncodeUnicode(%q) = %q, want it unchanged", in, got)
		}
	}
}

// TestEncodeKeepsInvalidUTF8: a byte that is not valid UTF-8 names no code
// point, so it passes through rather than becoming a replacement character
// that would claim the file says something it does not.
func TestEncodeKeepsInvalidUTF8(t *testing.T) {
	in := "a\xffb"
	if got := EncodeUnicode(in, UnicodeGo); got != in {
		t.Fatalf("EncodeUnicode(%q) = %q, want it unchanged", in, got)
	}
}

func TestDecodeUnicodeForms(t *testing.T) {
	tests := []struct {
		name    string
		dialect UnicodeDialect
		in, out string
	}{
		{"bmp", UnicodeGo, uUmlaut + "ber", "über"},
		{"long", UnicodeGo, uLong, grinning},
		{"surrogate pair", UnicodeJSON, uSurr, grinning},
		{"brace where the dialect has it", UnicodeScript, uBrace, grinning},
		{"brace where it does not", UnicodeJSON, uBrace, uBrace},
		{"hex where the dialect has it", UnicodePython, "caf" + uHexE, "café"},
		{"hex where it is a byte", UnicodeGo, "caf" + uHexE, "caf" + uHexE},
		{"escaped backslash stays", UnicodeGo, bs + uUmlaut, bs + uUmlaut},
		{"truncated stays", UnicodeGo, bs + "u00f", bs + "u00f"},
		{"other escapes pass through", UnicodeGo, "a" + bs + "nb" + bs + `"c`, "a" + bs + "nb" + bs + `"c`},
		{"named python escape stays", UnicodePython, bs + "N{BULLET}", bs + "N{BULLET}"},
		{"nothing to do", UnicodeGo, "plain", "plain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecodeUnicode(tc.in, tc.dialect); got != tc.out {
				t.Fatalf("DecodeUnicode(%q) = %q, want %q", tc.in, got, tc.out)
			}
		})
	}
}

// TestRoundTripPerDialect is the acceptance round trip: for every language,
// decode(encode(text)) is the text again — and encode(decode(escaped)) is the
// escaped form again.
func TestRoundTripPerDialect(t *testing.T) {
	dialects := map[string]UnicodeDialect{
		"go": UnicodeGo, "json": UnicodeJSON, "script": UnicodeScript,
		"python": UnicodePython, "php": UnicodePHP, "yaml": UnicodeYAML,
		"toml": UnicodeTOML, "fallback": UnicodeFallback,
	}
	texts := []string{
		"über", "Grüße, Welt", "日本語",
		"emoji " + grinning + " tail", "mixed ü and " + grinning,
		"double " + bs + uUmlaut + " backslash",
		"plain ascii", "two\nlines with ü",
		// Text that already *contains* an escape is deliberately absent: an
		// escape is what unescaping resolves, so the round trip through the
		// decode direction turns it into its character — correctly. That
		// escaping leaves it alone is TestEncodeLeavesASCIIAlone's job.
	}
	for name, d := range dialects {
		for _, text := range texts {
			enc := EncodeUnicode(text, d)
			if got := DecodeUnicode(enc, d); got != text {
				t.Errorf("%s: decode(encode(%q)) = %q", name, text, got)
			}
			if got := EncodeUnicode(DecodeUnicode(enc, d), d); got != enc {
				t.Errorf("%s: encode(decode(%q)) = %q", name, enc, got)
			}
		}
	}
}

func TestDialectFor(t *testing.T) {
	tests := []struct {
		id   string
		want UnicodeDialect
		ok   bool
	}{
		{"go", UnicodeGo, true},
		{"json", UnicodeJSON, true},
		{"ndjson", UnicodeJSON, true},
		{"typescript", UnicodeScript, true},
		{"python", UnicodePython, true},
		{"php", UnicodePHP, true},
		{"yaml", UnicodeYAML, true},
		{"ansible", UnicodeYAML, true},
		{"toml", UnicodeTOML, true},
		{"markdown", UnicodeDialect{}, false},
		{"", UnicodeDialect{}, false},
	}
	for _, tc := range tests {
		got, ok := DialectFor(tc.id)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("DialectFor(%q) = %+v, %v; want %+v, %v", tc.id, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLiteralAt(t *testing.T) {
	tests := []struct {
		name       string
		dialect    UnicodeDialect
		line       string
		col        int
		start, end int
		ok         bool
	}{
		{name: "inside a go string", dialect: UnicodeGo, line: `s := "abc"`, col: 7, start: 6, end: 9, ok: true},
		{name: "on the opening quote", dialect: UnicodeGo, line: `s := "abc"`, col: 5, start: 6, end: 9, ok: true},
		{name: "on the closing quote", dialect: UnicodeGo, line: `s := "abc"`, col: 9, start: 6, end: 9, ok: true},
		{name: "outside any literal", dialect: UnicodeGo, line: `s := "abc"`, col: 0},
		{name: "escaped quote does not close", dialect: UnicodeGo, line: `s := "a\"b"`, col: 7, start: 6, end: 10, ok: true},
		{name: "second of two literals", dialect: UnicodeGo, line: `f("a", "b")`, col: 8, start: 8, end: 9, ok: true},
		{name: "go raw literal refuses", dialect: UnicodeGo, line: "s := `abc`", col: 7},
		{name: "python raw prefix refuses", dialect: UnicodePython, line: `s = r"abc"`, col: 8},
		{name: "python plain literal", dialect: UnicodePython, line: `s = "abc"`, col: 6, start: 5, end: 8, ok: true},
		{name: "yaml single quotes are raw", dialect: UnicodeYAML, line: `k: 'abc'`, col: 5},
		{name: "yaml double quotes", dialect: UnicodeYAML, line: `k: "abc"`, col: 5, start: 4, end: 7, ok: true},
		{name: "unterminated runs to the line end", dialect: UnicodeGo, line: `s := "abc`, col: 7, start: 6, end: 9, ok: true},
		{name: "empty line", dialect: UnicodeGo, line: "", col: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end, ok := LiteralAt(tc.line, tc.col, tc.dialect)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && (start != tc.start || end != tc.end) {
				t.Fatalf("range = [%d,%d), want [%d,%d)", start, end, tc.start, tc.end)
			}
		})
	}
}
