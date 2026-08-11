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

// TestPythonSelfClsCaptures guards #1798: self/cls (and the metaclass
// spellings mcls/metacls) highlight as variable.builtin at both the
// parameter and every use site, while similarly-named identifiers that only
// partially match (selfish, classes) stay plain variables.
func TestPythonSelfClsCaptures(t *testing.T) {
	lines := []string{
		"class Wrapper:",
		"    def method(self, selfish):",
		"        return self.value + selfish",
		"    @classmethod",
		"    def create(cls, classes):",
		"        return cls, classes",
		"class Meta(type):",
		"    def __new__(mcls, name, bases, ns):",
		"        return super().__new__(mcls, name, bases, ns)",
		"    def __call__(metacls, *args):",
		"        return metacls",
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
		{"self param", 1, "self", "variable.builtin"},
		{"selfish param", 1, "selfish", "variable"},
		{"self use", 2, "self.value", "variable.builtin"},
		{"selfish use", 2, "selfish", "variable"},
		{"cls param", 4, "cls", "variable.builtin"},
		{"classes param", 4, "classes)", "variable"},
		{"cls use", 5, "cls,", "variable.builtin"},
		{"classes use", 5, "classes", "variable"},
		{"mcls param", 7, "mcls", "variable.builtin"},
		{"mcls use", 8, "mcls, name", "variable.builtin"},
		{"metacls param", 9, "metacls", "variable.builtin"},
		{"metacls use", 10, "metacls", "variable.builtin"},
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

// TestPythonFStringInterpolation guards #1466: the expression inside an
// f-string interpolation highlights as code (the old whole-node (string)
// capture started before the interpolation and won the first-covering
// lookup), the format spec / conversion get their own scope, and escaped
// braces stay string.
func TestPythonFStringInterpolation(t *testing.T) {
	highlight.SetRainbow(false)
	defer highlight.SetRainbow(true)
	lines := []string{
		`name = f"abc{x}def"`,
		`pct = f"abc{x:.0f}def"`,
		`call = f"{obj.method(arg, 1)!r} literal {{braces}}"`,
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
		{"string prefix+quote", 0, `f"`, "string"},
		{"literal before interpolation", 0, "abc", "string"},
		{"open brace", 0, "{", "punctuation.special"},
		{"interpolated identifier", 0, "x}", "variable"},
		{"close brace", 0, "}", "punctuation.special"},
		{"literal after interpolation", 0, "def", "string"},
		{"format spec", 1, ":.0f", "punctuation.special"},
		{"attribute object", 2, "obj", "variable"},
		{"method call", 2, "method", "function.method"},
		{"call argument", 2, "arg", "variable"},
		{"number argument", 2, "1)", "number"},
		{"conversion", 2, "!r", "punctuation.special"},
		{"literal between", 2, " literal ", "string"},
		{"escaped open braces", 2, "{{", "string"},
		{"escaped literal", 2, "braces", "string"},
		{"escaped close braces", 2, "}}", "string"},
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
