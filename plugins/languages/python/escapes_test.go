package langpython

import (
	"testing"

	"ike/internal/escapes"
	"ike/internal/lang"
)

// unicodeSpans keeps only the unicode-escape stand-ins of a Spans run — the
// producer also emits masks, network, permission and constant spans.
func unicodeSpans(t *testing.T, path string, lines []string) []lang.Span {
	t.Helper()
	l, ok := lang.ByPath(path)
	if !ok || l.Spans == nil {
		t.Fatalf("%s: no Spans producer registered", path)
	}
	var out []lang.Span
	for _, s := range l.Spans(lines) {
		if s.Capture == escapes.UnicodeCapture {
			out = append(out, s)
		}
	}
	return out
}

// TestPythonUnicodeEscapeSpans (#2334): Python string literals decode their
// \u, \U and \x escapes.
func TestPythonUnicodeEscapeSpans(t *testing.T) {
	spans := unicodeSpans(t, "/p/app.py", []string{
		`x = "M\u00e4rz"`,
		`y = '\U0001F600'`,
		`z = "\xfc"`,
	})
	want := []string{"ä", "😀", "ü"}
	if len(spans) != len(want) {
		t.Fatalf("spans = %+v, want %d", spans, len(want))
	}
	for i, w := range want {
		if spans[i].Replace != w {
			t.Errorf("span %d = %+v, want %q", i, spans[i], w)
		}
	}
}

// TestPythonRawStringsStayRaw (#2334): a raw or bytes prefix suppresses the
// decode — a backslash in r"…" is literal text, and \u is no escape in b"…".
func TestPythonRawStringsStayRaw(t *testing.T) {
	for _, line := range []string{
		`x = r"\u00fc"`,
		`x = rb"\u00fc"`,
		`x = b"\u00fc"`,
		`x = "\\u00fc"`,
		`x = "\N{LATIN SMALL LETTER U WITH DIAERESIS}"`,
	} {
		if spans := unicodeSpans(t, "/p/app.py", []string{line}); len(spans) != 0 {
			t.Errorf("%q produced %+v, want none", line, spans)
		}
	}
}
