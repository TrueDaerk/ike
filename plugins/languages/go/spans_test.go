package langgo

import (
	"testing"

	"ike/internal/escapes"
	"ike/internal/lang"
)

// TestGoUnicodeEscapeSpans (#1620): the go language registers the
// unicode-escape span producer; escapes in string literals decode.
func TestGoUnicodeEscapeSpans(t *testing.T) {
	l, ok := lang.ByID("go")
	if !ok || l.Spans == nil {
		t.Fatal("go: no Spans producer registered")
	}
	spans := l.Spans([]string{`s := "M\u00e4rz \ud83d\ude00"`})
	if len(spans) != 2 {
		t.Fatalf("spans = %+v, want 2", spans)
	}
	if spans[0].Replace != "\u00e4" || spans[0].Capture != escapes.UnicodeCapture {
		t.Errorf("first span = %+v, want \u00e4 as %s", spans[0], escapes.UnicodeCapture)
	}
	if spans[1].Replace != "\U0001F600" {
		t.Errorf("second span = %+v, want the surrogate pair combined", spans[1])
	}
}
