package jqplay

import "testing"

// TestCheckPlacesTheError covers the position extraction (#2781): each case
// names the rune span the query line must underline, or none.
func TestCheckPlacesTheError(t *testing.T) {
	cases := []struct {
		name       string
		d          Dialect
		program    string
		start, end int
	}{
		{"unknown function", DialectJQ, ".a | selct(.x)", 5, 10},
		{"unknown function in yq", DialectYQ, ".a | selct(.x)", 5, 10},
		{"unknown function after its def", DialectJQ, "def f: 1; f(2)", 10, 11},
		{"unknown variable", DialectJQ, ". as $a | $b", 10, 12},
		{"unknown label", DialectJQ, "label $f | break $g", 17, 19},
		{"unexpected end points past the program", DialectJQ, ".a | (", 6, 7},
		{"unexpected token", DialectJQ, ".a | )", 5, 6},
		{"offsets count the leading blanks", DialectJQ, "   .a | )", 8, 9},
		{"offsets are runes, not bytes", DialectJQ, `"é" | )`, 6, 7},
		{"invalid escape", DialectJQ, `.a | "\q"`, 6, 8},
		{"unterminated string", DialectJQ, `.a | "abc`, 5, 9},
		{"xmq unterminated quote", DialectXMQ, "select '//a", 7, 11},
		{"xmq trailing backslash", DialectXMQ, `select a\`, 8, 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Check(c.d, c.program)
			if d.Msg == "" {
				t.Fatalf("Check(%q) compiled, want an error", c.program)
			}
			if d.Start != c.start || d.End != c.end {
				t.Errorf("Check(%q) span = [%d,%d), want [%d,%d) (%s)", c.program, d.Start, d.End, c.start, c.end, d.Msg)
			}
			if got := Compile(c.d, c.program); got != d.Msg {
				t.Errorf("Compile = %q, Check.Msg = %q; the two must agree", got, d.Msg)
			}
		})
	}
}

// TestCheckWithoutPosition: a program that compiles has no diagnostic, and an
// error whose name cannot be found in the program falls back to the message
// alone rather than a guessed or out-of-range span.
func TestCheckWithoutPosition(t *testing.T) {
	for _, p := range []string{".a", "", "   ", ".a | map(.b)"} {
		if d := Check(DialectJQ, p); d != (Diagnostic{}) {
			t.Errorf("Check(%q) = %+v, want none", p, d)
		}
	}
	if d := compileDiagnostic(".a", errorString("function not defined: nope/0")); d.HasSpan() || d.Msg == "" {
		t.Errorf("an unplaceable name = %+v, want the message without a span", d)
	}
	if d := parseDiagnostic(".a", errorString("boom")); d.HasSpan() || d.Msg != "boom" {
		t.Errorf("a foreign parse error = %+v, want the message without a span", d)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
