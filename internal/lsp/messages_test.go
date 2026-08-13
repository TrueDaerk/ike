package lsp

import (
	"testing"

	"ike/internal/lsp/protocol"
)

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

// TestClassLike guards the class category's kind filter (#1849): Go has no
// classes, so structs and interfaces count, as do enums; functions, methods
// and variables do not, and a missing kind is never class-like.
func TestClassLike(t *testing.T) {
	for _, c := range []struct {
		kind int
		want bool
	}{
		{protocol.SymKindClass, true},
		{protocol.SymKindStruct, true},
		{protocol.SymKindInterface, true},
		{protocol.SymKindEnum, true},
		{protocol.SymKindFunction, false},
		{protocol.SymKindMethod, false},
		{protocol.SymKindVariable, false},
		{protocol.SymKindEnumMember, false},
		{0, false},
	} {
		if got := ClassLike(c.kind); got != c.want {
			t.Errorf("ClassLike(%d) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// TestSymbolKindLabel: known kinds render a short badge, unknown ones none.
func TestSymbolKindLabel(t *testing.T) {
	for _, c := range []struct {
		kind int
		want string
	}{
		{protocol.SymKindClass, "class"},
		{protocol.SymKindStruct, "struct"},
		{protocol.SymKindInterface, "interface"},
		{protocol.SymKindFunction, "func"},
		{0, ""},
		{99, ""},
	} {
		if got := SymbolKindLabel(c.kind); got != c.want {
			t.Errorf("SymbolKindLabel(%d) = %q, want %q", c.kind, got, c.want)
		}
	}
}
