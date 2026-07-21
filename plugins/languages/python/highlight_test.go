//go:build cgo

package langpython

import (
	"strings"
	"testing"

	"ike/internal/highlight"
)

// TestPythonIdentifierCaptures guards #724: CaptureAt is first-span-wins, so
// the identifier catch-all must not shadow the specific patterns.
func TestPythonIdentifierCaptures(t *testing.T) {
	lines := []string{
		"MAX_SIZE = 10",
		"def compute(value):",
		"    print(value)",
		"    return Wrapper, value",
	}
	spans := highlight.Highlight("main.py", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for Python source, got none")
	}
	ix := highlight.NewIndex(spans)
	cases := []struct {
		name string
		line int
		word string
		want string
	}{
		{"constant heuristic", 0, "MAX_SIZE", "constant"},
		{"def name", 1, "compute", "function"},
		{"builtin call", 2, "print", "function.builtin"},
		{"constructor heuristic", 3, "Wrapper", "constructor"},
		{"plain identifier", 2, "value", "variable"},
	}
	for _, c := range cases {
		col := strings.Index(lines[c.line], c.word)
		if col < 0 {
			t.Fatalf("%s: %q not in line %d", c.name, c.word, c.line)
		}
		if got := ix.CaptureAt(c.line, col); got != c.want {
			t.Errorf("%s: CaptureAt(%d,%d) = %q, want %q", c.name, c.line, col, got, c.want)
		}
	}
}
