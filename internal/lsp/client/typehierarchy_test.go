package client

import (
	"encoding/json"
	"strings"
	"testing"

	"ike/internal/lsp/protocol"
)

func TestPrepareTypeHierarchyDecodesItems(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/prepareTypeHierarchy": func(json.RawMessage) any {
			return []protocol.TypeHierarchyItem{{
				Name:           "Shape",
				Kind:           protocol.SymKindInterface,
				URI:            "file:///tmp/shape.go",
				SelectionRange: protocol.Range{Start: protocol.Position{Line: 2, Character: 5}},
				Data:           json.RawMessage(`{"token":7}`),
			}}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	items, err := c.PrepareTypeHierarchy(ctx, protocol.TypeHierarchyPrepareParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "Shape" || items[0].SelectionRange.Start.Line != 2 {
		t.Fatalf("items = %+v", items)
	}
	if string(items[0].Data) != `{"token":7}` {
		t.Errorf("opaque data must survive the decode, got %q", items[0].Data)
	}
}

func TestPrepareTypeHierarchyNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/prepareTypeHierarchy": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	items, err := c.PrepareTypeHierarchy(ctx, protocol.TypeHierarchyPrepareParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("null result should yield no items, got %+v", items)
	}
}

func TestSupertypesRoundTripsItem(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"typeHierarchy/supertypes": func(params json.RawMessage) any {
			var p protocol.TypeHierarchyItemParams
			if err := json.Unmarshal(params, &p); err != nil || p.Item.Name != "Circle" || string(p.Item.Data) != `{"token":7}` {
				return nil
			}
			return []protocol.TypeHierarchyItem{{Name: "Shape", URI: "file:///tmp/shape.go"}}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	items, err := c.Supertypes(ctx, protocol.TypeHierarchyItemParams{
		Item: protocol.TypeHierarchyItem{Name: "Circle", Data: json.RawMessage(`{"token":7}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "Shape" {
		t.Fatalf("items = %+v", items)
	}
}

func TestSubtypesNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"typeHierarchy/subtypes": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	items, err := c.Subtypes(ctx, protocol.TypeHierarchyItemParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("null result should yield no items, got %+v", items)
	}
}

func TestImplementationDecodesShapes(t *testing.T) {
	cases := []struct {
		name   string
		result any
		want   int
	}{
		{"single location", protocol.Location{URI: "file:///tmp/a.go"}, 1},
		{"location array", []protocol.Location{{URI: "file:///tmp/a.go"}, {URI: "file:///tmp/b.go"}}, 2},
		{"location links", []map[string]any{{
			"targetUri":            "file:///tmp/a.go",
			"targetSelectionRange": protocol.Range{Start: protocol.Position{Line: 4}},
		}}, 1},
		{"null", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
				"textDocument/implementation": func(json.RawMessage) any { return tc.result },
			})
			ctx, cancel := ctx2s()
			defer cancel()
			locs, err := c.Implementation(ctx, protocol.ImplementationParams{})
			if err != nil {
				t.Fatal(err)
			}
			if len(locs) != tc.want {
				t.Fatalf("got %d locations, want %d: %+v", len(locs), tc.want, locs)
			}
		})
	}
}

// TestClientCapabilitiesAdvertiseInheritance guards #1449: without the
// implementation/typeHierarchy entries servers may withhold both providers.
func TestClientCapabilitiesAdvertiseInheritance(t *testing.T) {
	raw, err := json.Marshal(clientCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"implementation":{"linkSupport":true}`) {
		t.Fatalf("initialize capabilities missing implementation:\n%s", s)
	}
	if !strings.Contains(s, `"typeHierarchy":{}`) {
		t.Fatalf("initialize capabilities missing typeHierarchy:\n%s", s)
	}
}

// TestParseInheritanceProviders checks the bool/object/null provider forms for
// both new capabilities.
func TestParseInheritanceProviders(t *testing.T) {
	for raw, want := range map[string]bool{`true`: true, `{}`: true, `false`: false, `null`: false} {
		caps := parseCapabilities(protocol.ServerCapabilities{
			ImplementationProvider: json.RawMessage(raw),
			TypeHierarchyProvider:  json.RawMessage(raw),
		})
		if caps.Implementation != want || caps.TypeHierarchy != want {
			t.Errorf("provider %s: implementation=%v typeHierarchy=%v, want %v", raw, caps.Implementation, caps.TypeHierarchy, want)
		}
	}
}

// TestSymbolKindConstants guards the CompletionItemKind/SymbolKind numbering
// trap: DocumentSymbol.Kind uses SymbolKind, where Interface is 11 (not 8).
func TestSymbolKindConstants(t *testing.T) {
	if protocol.SymKindClass != 5 || protocol.SymKindMethod != 6 || protocol.SymKindInterface != 11 || protocol.SymKindStruct != 23 {
		t.Fatalf("SymKind constants drifted: class=%d method=%d interface=%d struct=%d",
			protocol.SymKindClass, protocol.SymKindMethod, protocol.SymKindInterface, protocol.SymKindStruct)
	}
}
