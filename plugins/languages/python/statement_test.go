package langpython

import (
	"testing"

	"ike/internal/lang"
)

func TestCompleteStatement(t *testing.T) {
	for _, tc := range []struct {
		line string
		head string
		body bool
		ok   bool
	}{
		{"def abc()", "def abc():", true, true},
		{"def abc(", "def abc():", true, true},
		{"    def abc(self, x=[1", "    def abc(self, x=[1]):", true, true},
		{"def f(x: int) -> dict[str, int]", "def f(x: int) -> dict[str, int]:", true, true},
		{"if x", "if x:", true, true},
		{"if x:", "if x:", true, true},
		{"if x:   ", "if x:", true, true},
		{"elif y", "elif y:", true, true},
		{"else", "else:", true, true},
		{"for i in range(3", "for i in range(3):", true, true},
		{"while True", "while True:", true, true},
		{"try", "try:", true, true},
		{"except ValueError as e", "except ValueError as e:", true, true},
		{"finally", "finally:", true, true},
		{"with open(p) as f", "with open(p) as f:", true, true},
		{"class Foo(Base", "class Foo(Base):", true, true},
		{"async def run()", "async def run():", true, true},
		{"async with lock", "async with lock:", true, true},
		{"match command", "match command:", true, true},
		{"    case [x, y]", "    case [x, y]:", true, true},
		{"case {'a': 1}", "case {'a': 1}:", true, true},
		// One-line compound statements are complete; the caret moves on.
		{"if x: return", "if x: return", false, true},
		// Nothing to complete: expressions, assignments, calls, comments.
		{"x = 1", "", false, false},
		{"foo(bar)", "", false, false},
		{"match = re.match(p, s)", "", false, false},
		{"match(x)", "", false, false},
		{"iffy = 3", "", false, false},
		{"x = 1 if y else 2", "", false, false},
		{"# if x", "", false, false},
		{"", "", false, false},
		{"    ", "", false, false},
	} {
		got, ok := toolchain{}.CompleteStatement(tc.line)
		if ok != tc.ok {
			t.Errorf("%q: ok = %v, want %v (%+v)", tc.line, ok, tc.ok, got)
			continue
		}
		if !ok {
			continue
		}
		if got.Head != tc.head || got.Body != tc.body || len(got.Tail) != 0 {
			t.Errorf("%q: got %+v, want head %q body %v", tc.line, got, tc.head, tc.body)
		}
	}
}

func TestStatementCompleterRegistered(t *testing.T) {
	if !lang.SupportsStatementCompletion("python") {
		t.Fatal("python does not support statement completion")
	}
}
