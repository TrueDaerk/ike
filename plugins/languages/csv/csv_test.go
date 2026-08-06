package langcsv

import (
	"testing"

	"ike/internal/lang"
)

func captureAt(spans []lang.Span, line, col int) string {
	for _, s := range spans {
		if s.Line == line && col >= s.StartCol && col < s.EndCol {
			return s.Capture
		}
	}
	return ""
}

// TestColumnSpansRainbow: each column carries its cycling rainbow capture,
// separators style as punctuation (#1589).
func TestColumnSpansRainbow(t *testing.T) {
	spans := columnSpans("csv", []string{"a,b,c", "1,2,3"})
	for line := 0; line < 2; line++ {
		if got := captureAt(spans, line, 0); got != "rainbow.0" {
			t.Errorf("line %d col 0 = %q, want rainbow.0", line, got)
		}
		if got := captureAt(spans, line, 2); got != "rainbow.1" {
			t.Errorf("line %d col 2 = %q, want rainbow.1", line, got)
		}
		if got := captureAt(spans, line, 4); got != "rainbow.2" {
			t.Errorf("line %d col 4 = %q, want rainbow.2", line, got)
		}
		if got := captureAt(spans, line, 1); got != "punctuation" {
			t.Errorf("line %d separator = %q, want punctuation", line, got)
		}
	}
}

// TestColumnSpansCycle: column 6 wraps back to rainbow.0.
func TestColumnSpansCycle(t *testing.T) {
	spans := columnSpans("csv", []string{"a,b,c,d,e,f,g"})
	if got := captureAt(spans, 0, 12); got != "rainbow.0" {
		t.Errorf("column 6 capture = %q, want rainbow.0", got)
	}
}

// TestColumnSpansTSVAndQuotes: tsv splits on tabs; a quoted comma stays one
// csv field.
func TestColumnSpansTSVAndQuotes(t *testing.T) {
	spans := columnSpans("tsv", []string{"a\tb"})
	if got := captureAt(spans, 0, 2); got != "rainbow.1" {
		t.Errorf("tsv second column = %q, want rainbow.1", got)
	}
	spans = columnSpans("csv", []string{`"a,b",c`})
	if got := captureAt(spans, 0, 2); got != "rainbow.0" {
		t.Errorf("quoted comma split the field, capture = %q", got)
	}
}
