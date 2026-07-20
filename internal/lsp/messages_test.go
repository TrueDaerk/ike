package lsp

import "testing"

// TestDiagnosticCode: the protocol's string-or-number code renders as text
// for the diagnostic popup (#739); other shapes yield "".
func TestDiagnosticCode(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{
		{"reportUndefinedVariable", "reportUndefinedVariable"},
		{float64(2322), "2322"},
		{7, "7"},
		{nil, ""},
		{map[string]any{"x": 1}, ""},
	} {
		if got := diagnosticCode(c.in); got != c.want {
			t.Fatalf("diagnosticCode(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
