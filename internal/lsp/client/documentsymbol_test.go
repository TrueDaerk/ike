package client

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

// TestDocumentSymbolsHierarchical decodes the DocumentSymbol[] shape, children
// included (#1025).
func TestDocumentSymbolsHierarchical(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/documentSymbol": func(json.RawMessage) any {
			return []protocol.DocumentSymbol{{
				Name:           "Server",
				Kind:           5,
				Range:          protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 20}},
				SelectionRange: protocol.Range{Start: protocol.Position{Line: 2, Character: 5}},
				Children: []protocol.DocumentSymbol{{
					Name:           "Start",
					Kind:           6,
					Range:          protocol.Range{Start: protocol.Position{Line: 4}, End: protocol.Position{Line: 9}},
					SelectionRange: protocol.Range{Start: protocol.Position{Line: 4, Character: 7}},
				}},
			}}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	syms, err := c.DocumentSymbols(ctx, protocol.DocumentSymbolParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 1 || syms[0].Name != "Server" || syms[0].Kind != 5 {
		t.Fatalf("symbols = %+v", syms)
	}
	if len(syms[0].Children) != 1 || syms[0].Children[0].Name != "Start" {
		t.Fatalf("children must survive the decode, got %+v", syms[0].Children)
	}
	if syms[0].SelectionRange.Start.Character != 5 {
		t.Errorf("selection range lost: %+v", syms[0].SelectionRange)
	}
}

// TestDocumentSymbolsFlat normalises the flat SymbolInformation[] shape into
// childless DocumentSymbol nodes whose ranges are the location range.
func TestDocumentSymbolsFlat(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/documentSymbol": func(json.RawMessage) any {
			return []protocol.SymbolInformation{{
				Name:          "handler",
				Kind:          12,
				ContainerName: "main",
				Location: protocol.Location{
					URI:   "file:///tmp/a.go",
					Range: protocol.Range{Start: protocol.Position{Line: 7, Character: 5}, End: protocol.Position{Line: 12}},
				},
			}}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	syms, err := c.DocumentSymbols(ctx, protocol.DocumentSymbolParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 1 || syms[0].Name != "handler" || syms[0].Kind != 12 {
		t.Fatalf("symbols = %+v", syms)
	}
	if syms[0].SelectionRange.Start.Line != 7 || syms[0].SelectionRange.Start.Character != 5 {
		t.Errorf("flat entry must map its location onto the selection range, got %+v", syms[0].SelectionRange)
	}
	if syms[0].Range.End.Line != 12 {
		t.Errorf("flat entry must map its location onto the full range, got %+v", syms[0].Range)
	}
	if syms[0].Detail != "main" {
		t.Errorf("container name should surface as the detail, got %q", syms[0].Detail)
	}
	if len(syms[0].Children) != 0 {
		t.Errorf("flat entries have no children, got %+v", syms[0].Children)
	}
}

// TestDocumentSymbolsNull treats a null (or unexpected) reply as no symbols.
func TestDocumentSymbolsNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/documentSymbol": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	syms, err := c.DocumentSymbols(ctx, protocol.DocumentSymbolParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 0 {
		t.Fatalf("null result should yield no symbols, got %+v", syms)
	}
}
