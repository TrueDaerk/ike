package langtoml

import (
	"testing"

	"ike/internal/escapes"
	"ike/internal/lang"
)

// unicodeSpans keeps only the unicode-escape stand-ins of a Spans run — the
// producer also emits cron, epoch, number and network spans.
func unicodeSpans(t *testing.T, lines []string) []lang.Span {
	t.Helper()
	l, ok := lang.ByPath("/p/conf.toml")
	if !ok || l.Spans == nil {
		t.Fatal("toml: no Spans producer registered")
	}
	var out []lang.Span
	for _, s := range l.Spans(lines) {
		if s.Capture == escapes.UnicodeCapture {
			out = append(out, s)
		}
	}
	return out
}

// TestTOMLUnicodeEscapeSpans (#2334): a TOML basic string decodes \uXXXX and
// \UXXXXXXXX.
func TestTOMLUnicodeEscapeSpans(t *testing.T) {
	spans := unicodeSpans(t, []string{
		`title = "M\u00e4rz"`,
		`face = "\U0001F600"`,
	})
	want := []string{"ä", "😀"}
	if len(spans) != len(want) {
		t.Fatalf("spans = %+v, want %d", spans, len(want))
	}
	for i, w := range want {
		if spans[i].Replace != w {
			t.Errorf("span %d = %+v, want %q", i, spans[i], w)
		}
	}
}

// TestTOMLLiteralStringsStayRaw (#2334): a literal string '…' has no escapes
// at all, and TOML 1.0 has no \xNN.
func TestTOMLLiteralStringsStayRaw(t *testing.T) {
	for _, line := range []string{
		`title = '\u00fc'`,
		`title = "\xfc"`,
		`title = "\\u00fc"`,
	} {
		if spans := unicodeSpans(t, []string{line}); len(spans) != 0 {
			t.Errorf("%q produced %+v, want none", line, spans)
		}
	}
}
