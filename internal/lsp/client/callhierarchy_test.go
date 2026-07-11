package client

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

func TestPrepareCallHierarchyDecodesItems(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/prepareCallHierarchy": func(json.RawMessage) any {
			return []protocol.CallHierarchyItem{{
				Name:           "Greet",
				Kind:           12,
				URI:            "file:///tmp/a.go",
				SelectionRange: protocol.Range{Start: protocol.Position{Line: 3, Character: 5}},
				Data:           json.RawMessage(`{"token":1}`),
			}}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	items, err := c.PrepareCallHierarchy(ctx, protocol.CallHierarchyPrepareParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "Greet" || items[0].SelectionRange.Start.Line != 3 {
		t.Fatalf("items = %+v", items)
	}
	if string(items[0].Data) != `{"token":1}` {
		t.Errorf("opaque data must survive the decode, got %q", items[0].Data)
	}
}

func TestPrepareCallHierarchyNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/prepareCallHierarchy": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	items, err := c.PrepareCallHierarchy(ctx, protocol.CallHierarchyPrepareParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("null result should yield no items, got %+v", items)
	}
}

func TestIncomingCallsRoundTripsItem(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"callHierarchy/incomingCalls": func(params json.RawMessage) any {
			var p protocol.CallHierarchyCallsParams
			if err := json.Unmarshal(params, &p); err != nil || p.Item.Name != "Greet" {
				return nil
			}
			return []protocol.CallHierarchyIncomingCall{{
				From:       protocol.CallHierarchyItem{Name: "main", URI: "file:///tmp/main.go"},
				FromRanges: []protocol.Range{{Start: protocol.Position{Line: 8, Character: 1}}},
			}}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	calls, err := c.IncomingCalls(ctx, protocol.CallHierarchyCallsParams{
		Item: protocol.CallHierarchyItem{Name: "Greet"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].From.Name != "main" || calls[0].FromRanges[0].Start.Line != 8 {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestOutgoingCallsNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"callHierarchy/outgoingCalls": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	calls, err := c.OutgoingCalls(ctx, protocol.CallHierarchyCallsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("null result should yield no calls, got %+v", calls)
	}
}
