package highlight

import "testing"

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
