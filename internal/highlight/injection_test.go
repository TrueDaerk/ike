package highlight

import (
	"testing"

	"ike/internal/lang"
)

func TestOffsetSpans(t *testing.T) {
	f := Fragment{Lang: "sql", StartLine: 2, StartCol: 5, EndLine: 3, EndCol: 4}
	got := offsetSpans([]Span{
		{Line: 0, StartCol: 0, EndCol: 6, Capture: "keyword"}, // first fragment line: col shift
		{Line: 1, StartCol: 0, EndCol: 4, Capture: "keyword"}, // later line: line shift only
	}, f)
	want := []Span{
		{Line: 2, StartCol: 5, EndCol: 11, Capture: "keyword"},
		{Line: 3, StartCol: 0, EndCol: 4, Capture: "keyword"},
	}
	if len(got) != len(want) {
		t.Fatalf("offsetSpans returned %d spans, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Injected spans are placed before host spans, so CaptureAt prefers them over
// the host's enclosing capture inside the fragment range.
func TestInjectedSpanPrecedence(t *testing.T) {
	host := Span{Line: 0, StartCol: 4, EndCol: 20, Capture: "string"}
	injected := Span{Line: 0, StartCol: 5, EndCol: 11, Capture: "keyword"}
	ix := NewIndex(append([]Span{injected}, host))
	if got := ix.CaptureAt(0, 6); got != "keyword" {
		t.Errorf("inside fragment: got %q, want keyword", got)
	}
	if got := ix.CaptureAt(0, 4); got != "string" {
		t.Errorf("outside fragment: got %q, want string", got)
	}
}

// A "regex" fragment routes through the built-in mini-grammar (#1631) instead
// of a registered language: the injected spans land in host coordinates ahead
// of the host spans, so regex tokens win inside the string.
func TestOverlayRegexFragment(t *testing.T) {
	host := lang.Language{
		ID: "regexhost",
		Regions: func(lines []string) []lang.Region {
			// The regex body sits at cols 5..12 of `x = "[a-z]+$" // y`.
			return []lang.Region{{Lang: "regex", StartLine: 0, StartCol: 5, EndLine: 0, EndCol: 13}}
		},
	}
	lines := []string{`x = "[a-z]+$" // y`}
	hostSpans := []Span{{Line: 0, StartCol: 4, EndCol: 13, Capture: "string"}}
	spans, _ := overlayFragments(host, lines, hostSpans)
	ix := NewIndex(spans)
	if got := ix.CaptureAt(0, 5); got != "regex.class" {
		t.Errorf("class = %q, want regex.class", got)
	}
	if got := ix.CaptureAt(0, 10); got != "regex.quantifier" {
		t.Errorf("quantifier = %q, want regex.quantifier", got)
	}
	if got := ix.CaptureAt(0, 11); got != "regex.anchor" {
		t.Errorf("anchor = %q, want regex.anchor", got)
	}
	if got := ix.CaptureAt(0, 4); got != "string" {
		t.Errorf("opening quote = %q, want string", got)
	}
}

func TestOffsetFolds(t *testing.T) {
	f := Fragment{Lang: "json", StartLine: 4, StartCol: 0, EndLine: 9}
	got := offsetFolds([]Fold{{HeaderLine: 0, EndLine: 4}, {HeaderLine: 1, EndLine: 2}}, f)
	want := []Fold{{HeaderLine: 4, EndLine: 8}, {HeaderLine: 5, EndLine: 6}}
	if len(got) != len(want) {
		t.Fatalf("offsetFolds returned %d folds, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fold %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
