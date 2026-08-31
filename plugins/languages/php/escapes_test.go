package langphp

import (
	"testing"

	"ike/internal/escapes"
	"ike/internal/lang"
)

// unicodeSpans keeps only the unicode-escape stand-ins of a Spans run — the
// producer also emits the constant conceals.
func unicodeSpans(t *testing.T, lines []string) []lang.Span {
	t.Helper()
	l, ok := lang.ByPath("/p/app.php")
	if !ok || l.Spans == nil {
		t.Fatal("php: no Spans producer registered")
	}
	var out []lang.Span
	for _, s := range l.Spans(lines) {
		if s.Capture == escapes.UnicodeCapture {
			out = append(out, s)
		}
	}
	return out
}

// TestPHPUnicodeEscapeSpans (#2334): double-quoted PHP strings decode \uXXXX
// and the PHP 7 \u{X…} form.
func TestPHPUnicodeEscapeSpans(t *testing.T) {
	spans := unicodeSpans(t, []string{
		`$s = "M\u00e4rz";`,
		`$e = "\u{1F600}";`,
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

// TestPHPSingleQuotesStayRaw (#2334): PHP's '…' passes a backslash through
// unchanged, so nothing in it decodes — and a \xNN is a raw byte there and in
// "…" alike, so it never decodes on its own either.
func TestPHPSingleQuotesStayRaw(t *testing.T) {
	for _, line := range []string{
		`$s = '\u00fc';`,
		`$s = 'say "\u00fc"';`,
		`$s = "\\u00fc";`,
		`$s = "\xc3\xbc";`,
	} {
		if spans := unicodeSpans(t, []string{line}); len(spans) != 0 {
			t.Errorf("%q produced %+v, want none", line, spans)
		}
	}
}
