package lsp

import (
	"testing"

	"ike/internal/lsp/protocol"
)

// TestConvertDocumentSymbolsTree maps a nested reply into editor coordinates,
// keeping the hierarchy (#1025).
func TestConvertDocumentSymbolsTree(t *testing.T) {
	lines := []string{"package x", "", "type T struct {", "\tf int", "}", "func (t T) M() {}"}
	syms := []protocol.DocumentSymbol{{
		Name:           "T",
		Kind:           23,
		Range:          protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 4, Character: 1}},
		SelectionRange: protocol.Range{Start: protocol.Position{Line: 2, Character: 5}},
		Children: []protocol.DocumentSymbol{{
			Name:           "f",
			Kind:           8,
			Range:          protocol.Range{Start: protocol.Position{Line: 3}, End: protocol.Position{Line: 3, Character: 6}},
			SelectionRange: protocol.Range{Start: protocol.Position{Line: 3, Character: 1}},
		}},
	}}
	out := ConvertDocumentSymbols(syms, lines, protocol.EncodingUTF16)
	if len(out) != 1 {
		t.Fatalf("out = %+v", out)
	}
	root := out[0]
	if root.Name != "T" || root.Kind != 23 || root.Line != 2 || root.Col != 5 || root.EndLine != 4 {
		t.Fatalf("root = %+v", root)
	}
	if len(root.Children) != 1 || root.Children[0].Name != "f" || root.Children[0].Line != 3 {
		t.Fatalf("children = %+v", root.Children)
	}
}

// TestConvertDocumentSymbolsUTF16 converts a UTF-16 character offset behind a
// non-BMP rune into the right rune column.
func TestConvertDocumentSymbolsUTF16(t *testing.T) {
	lines := []string{"x = '😀'; name = 1"}
	// "x = '😀'; " is 10 runes but 11 UTF-16 units; "name" starts at unit 11.
	syms := []protocol.DocumentSymbol{{
		Name:           "name",
		Kind:           13,
		Range:          protocol.Range{Start: protocol.Position{Line: 0, Character: 11}, End: protocol.Position{Line: 0, Character: 19}},
		SelectionRange: protocol.Range{Start: protocol.Position{Line: 0, Character: 11}},
	}}
	out := ConvertDocumentSymbols(syms, lines, protocol.EncodingUTF16)
	if len(out) != 1 || out[0].Col != 10 {
		t.Fatalf("expected rune col 10 behind the surrogate pair, got %+v", out)
	}
}

// TestConvertDocumentSymbolsEmpty keeps nil in, nil out.
func TestConvertDocumentSymbolsEmpty(t *testing.T) {
	if out := ConvertDocumentSymbols(nil, []string{"x"}, ""); out != nil {
		t.Fatalf("expected nil, got %+v", out)
	}
}
