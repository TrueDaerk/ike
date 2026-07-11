package client

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

func TestDocumentHighlightDecodes(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/documentHighlight": func(json.RawMessage) any {
			return []protocol.DocumentHighlight{
				{Range: protocol.Range{Start: protocol.Position{Line: 1, Character: 2}, End: protocol.Position{Line: 1, Character: 7}}, Kind: protocol.HighlightWrite},
				{Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 0}, End: protocol.Position{Line: 4, Character: 5}}},
			}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	hs, err := c.DocumentHighlight(ctx, protocol.DocumentHighlightParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 || hs[0].Kind != protocol.HighlightWrite || hs[0].Range.Start.Line != 1 {
		t.Fatalf("hs = %+v", hs)
	}
	if hs[1].Kind != 0 {
		t.Errorf("absent kind must decode to 0, got %d", hs[1].Kind)
	}
}

func TestDocumentHighlightNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/documentHighlight": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	hs, err := c.DocumentHighlight(ctx, protocol.DocumentHighlightParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 0 {
		t.Fatalf("null result should yield no highlights, got %+v", hs)
	}
}

// TestParseCapabilitiesDocumentHighlight gates the feature on the
// boolean-or-object provider field (#172).
func TestParseCapabilitiesDocumentHighlight(t *testing.T) {
	for raw, want := range map[string]bool{`true`: true, `{}`: true, `false`: false, ``: false} {
		caps := parseCapabilities(protocol.ServerCapabilities{DocumentHighlightProvider: json.RawMessage(raw)})
		if caps.DocumentHighlight != want {
			t.Errorf("provider %q: DocumentHighlight = %v, want %v", raw, caps.DocumentHighlight, want)
		}
	}
}
