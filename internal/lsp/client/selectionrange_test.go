package client

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

func TestSelectionRangesDecodesParentChain(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/selectionRange": func(json.RawMessage) any {
			// One ladder: word → statement → whole document (#1912).
			return json.RawMessage(`[
				{"range":{"start":{"line":1,"character":2},"end":{"line":1,"character":5}},
				 "parent":{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":9}},
				  "parent":{"range":{"start":{"line":0,"character":0},"end":{"line":4,"character":0}}}}}
			]`)
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	ranges, err := c.SelectionRanges(ctx, protocol.SelectionRangeParams{Positions: []protocol.Position{{Line: 1, Character: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 {
		t.Fatalf("ranges = %+v, want 1 ladder", ranges)
	}
	inner := ranges[0]
	if inner.Range.Start.Character != 2 || inner.Range.End.Character != 5 {
		t.Errorf("innermost = %+v, want 1:2..1:5", inner.Range)
	}
	if inner.Parent == nil || inner.Parent.Parent == nil {
		t.Fatalf("parent chain truncated: %+v", inner)
	}
	if outer := inner.Parent.Parent; outer.Range.End.Line != 4 || outer.Parent != nil {
		t.Errorf("outermost = %+v, want 0:0..4:0 without parent", outer)
	}
}

func TestSelectionRangesNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/selectionRange": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	ranges, err := c.SelectionRanges(ctx, protocol.SelectionRangeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 0 {
		t.Fatalf("null result should yield no ladders, got %+v", ranges)
	}
}

// TestParseCapabilitiesSelectionRange gates the feature on the
// boolean-or-object provider field (#1912).
func TestParseCapabilitiesSelectionRange(t *testing.T) {
	for raw, want := range map[string]bool{`true`: true, `{}`: true, `false`: false, ``: false} {
		caps := parseCapabilities(protocol.ServerCapabilities{SelectionRangeProvider: json.RawMessage(raw)})
		if caps.SelectionRange != want {
			t.Errorf("provider %q: SelectionRange = %v, want %v", raw, caps.SelectionRange, want)
		}
	}
}
