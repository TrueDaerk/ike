package langyaml

import (
	"testing"

	"ike/internal/escapes"
	"ike/internal/lang"
)

// unicodeSpans keeps only the unicode-escape stand-ins of a Spans run — the
// producer also emits base64, cron, permission, epoch, number, network and
// anchor spans.
func unicodeSpans(t *testing.T, lines []string) []lang.Span {
	t.Helper()
	l, ok := lang.ByPath("/p/conf.yaml")
	if !ok || l.Spans == nil {
		t.Fatal("yaml: no Spans producer registered")
	}
	var out []lang.Span
	for _, s := range l.Spans(lines) {
		if s.Capture == escapes.UnicodeCapture {
			out = append(out, s)
		}
	}
	return out
}

// TestYAMLUnicodeEscapeSpans (#2334): a double-quoted YAML scalar decodes its
// \u, \U and \x escapes.
func TestYAMLUnicodeEscapeSpans(t *testing.T) {
	spans := unicodeSpans(t, []string{
		`title: "M\u00e4rz"`,
		`face: "\U0001F600"`,
		`byte: "\xfc"`,
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

// TestYAMLSingleQuotesStayRaw (#2334): the single-quoted YAML scalar escapes
// nothing (it doubles ” instead), and a plain scalar is not a literal at all.
func TestYAMLSingleQuotesStayRaw(t *testing.T) {
	for _, line := range []string{
		`title: '\u00fc'`,
		`title: \u00fc`,
		`title: "\\u00fc"`,
	} {
		if spans := unicodeSpans(t, []string{line}); len(spans) != 0 {
			t.Errorf("%q produced %+v, want none", line, spans)
		}
	}
}
