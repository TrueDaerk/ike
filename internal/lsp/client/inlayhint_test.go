package client

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

func TestInlayHintsDecodes(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/inlayHint": func(json.RawMessage) any {
			// One plain-string label, one label-parts array, so both wire
			// shapes flatten (#171).
			return json.RawMessage(`[
				{"position":{"line":1,"character":4},"label":"int","kind":1,"paddingLeft":true},
				{"position":{"line":2,"character":0},"label":[{"value":"a:"},{"value":"b"}],"kind":2,"paddingRight":true}
			]`)
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	hints, err := c.InlayHints(ctx, protocol.InlayHintParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 2 {
		t.Fatalf("hints = %+v, want 2", hints)
	}
	if hints[0].Label != "int" || hints[0].Kind != protocol.InlayHintType || !hints[0].PaddingLeft {
		t.Errorf("first hint = %+v, want padded 'int' type hint", hints[0])
	}
	if hints[1].Label != "a:b" || hints[1].Kind != protocol.InlayHintParameter || !hints[1].PaddingRight {
		t.Errorf("second hint = %+v, want flattened 'a:b' parameter hint", hints[1])
	}
	if hints[1].Position.Line != 2 || hints[1].Position.Character != 0 {
		t.Errorf("second hint position = %+v", hints[1].Position)
	}
}

func TestInlayHintsNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/inlayHint": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	hints, err := c.InlayHints(ctx, protocol.InlayHintParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 0 {
		t.Fatalf("null result should yield no hints, got %+v", hints)
	}
}

// TestParseCapabilitiesInlayHint gates the feature on the boolean-or-object
// provider field (#171).
func TestParseCapabilitiesInlayHint(t *testing.T) {
	for raw, want := range map[string]bool{`true`: true, `{}`: true, `{"resolveProvider":false}`: true, `false`: false, ``: false} {
		caps := parseCapabilities(protocol.ServerCapabilities{InlayHintProvider: json.RawMessage(raw)})
		if caps.InlayHint != want {
			t.Errorf("provider %q: InlayHint = %v, want %v", raw, caps.InlayHint, want)
		}
	}
}
