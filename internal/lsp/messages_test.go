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

// TestConvertRelatedInformation guards #2147: a diagnostic's linked locations
// survive the conversion to editor coordinates — a location in the published
// document converts through the negotiated encoding, one in another file
// keeps the server's position, and both carry a filesystem path.
func TestConvertRelatedInformation(t *testing.T) {
	lines := []string{"emoji 😀 here", "second"}
	p := protocol.PublishDiagnosticsParams{
		URI: protocol.PathToURI("/proj/main.go"),
		Diagnostics: []protocol.Diagnostic{{
			Range:   protocol.Range{Start: protocol.Position{Line: 1}, End: protocol.Position{Line: 1, Character: 6}},
			Message: "redeclared",
			RelatedInformation: []protocol.DiagnosticRelatedInformation{
				{
					// Past the surrogate pair: 10 UTF-16 units are 9 runes.
					Location: protocol.Location{
						URI:   protocol.PathToURI("/proj/main.go"),
						Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 10}},
					},
					Message: "declared here",
				},
				{
					Location: protocol.Location{
						URI:   protocol.PathToURI("/proj/other.go"),
						Range: protocol.Range{Start: protocol.Position{Line: 41, Character: 4}},
					},
					Message: "and here",
				},
			},
		}},
	}
	got := ConvertDiagnostics(p, lines, protocol.EncodingUTF16)
	if len(got) != 1 || len(got[0].Related) != 2 {
		t.Fatalf("converted = %+v, want one diagnostic with two related entries", got)
	}
	same, other := got[0].Related[0], got[0].Related[1]
	if same.Path != "/proj/main.go" || same.Line != 0 || same.Col != 9 {
		t.Errorf("same-file entry = %+v, want main.go 0:9 (rune column)", same)
	}
	if other.Path != "/proj/other.go" || other.Line != 41 || other.Col != 4 {
		t.Errorf("cross-file entry = %+v, want other.go 41:4", other)
	}
	if same.Label() != "declared here  main.go:1" {
		t.Errorf("Label = %q", same.Label())
	}
}

// TestConvertDiagnosticsWithoutRelatedInformation: a server that sends none
// leaves the slice nil, so nothing renders under the message.
func TestConvertDiagnosticsWithoutRelatedInformation(t *testing.T) {
	p := protocol.PublishDiagnosticsParams{
		URI:         protocol.PathToURI("/proj/main.go"),
		Diagnostics: []protocol.Diagnostic{{Message: "boom"}},
	}
	if got := ConvertDiagnostics(p, []string{"x"}, ""); len(got) != 1 || got[0].Related != nil {
		t.Errorf("converted = %+v, want no related entries", got)
	}
}
