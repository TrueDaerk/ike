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

// TestPythonDecoratorArguments guards #928: only the @ sigil and the dotted
// name carry the decorator color; the argument list highlights like a normal
// call — strings as strings, kwarg names as plain identifiers — for
// single-line and multi-line decorators alike.
func TestPythonDecoratorArguments(t *testing.T) {
	lines := []string{
		`@router.get("/users", summary="List")`,
		`def list_users():`,
		`    pass`,
		``,
		`@router.post(`,
		`    "/users",`,
		`    description="Create a user",`,
		`)`,
		`def create_user():`,
		`    pass`,
		``,
		`@staticmethod`,
		`def helper():`,
		`    pass`,
	}
	spans := highlight.Highlight("api.py", lines)
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
		{"sigil", 0, "@", "function"},
		{"dotted name", 0, "router.get", "function"},
		{"string argument", 0, `"/users"`, "string"},
		{"kwarg name", 0, "summary", "variable"},
		{"kwarg string value", 0, `"List"`, "string"},
		{"multi-line string arg", 5, `"/users"`, "string"},
		{"multi-line kwarg name", 6, "description", "variable"},
		{"multi-line kwarg value", 6, `"Create a user"`, "string"},
		{"bare decorator", 11, "staticmethod", "function"},
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
	// The closing paren of the argument list must not carry the decorator
	// color (the old whole-node capture painted it too).
	if got := ix.CaptureAt(0, len(lines[0])-1); got == "function" {
		t.Errorf("closing paren: still decorator-colored (%q)", got)
	}
}
