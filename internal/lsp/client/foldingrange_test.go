package client

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

func TestFoldingRangesDecodes(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/foldingRange": func(json.RawMessage) any {
			// One kinded range with character refinements, one bare line range
			// (#1912).
			return json.RawMessage(`[
				{"startLine":0,"startCharacter":7,"endLine":3,"endCharacter":1,"kind":"imports"},
				{"startLine":5,"endLine":9}
			]`)
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	ranges, err := c.FoldingRanges(ctx, protocol.FoldingRangeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 2 {
		t.Fatalf("ranges = %+v, want 2", ranges)
	}
	if ranges[0].StartLine != 0 || ranges[0].EndLine != 3 || ranges[0].Kind != protocol.FoldingRangeImports {
		t.Errorf("first range = %+v, want imports fold 0..3", ranges[0])
	}
	if ranges[0].StartCharacter == nil || *ranges[0].StartCharacter != 7 {
		t.Errorf("startCharacter = %v, want 7", ranges[0].StartCharacter)
	}
	if ranges[1].StartLine != 5 || ranges[1].EndLine != 9 || ranges[1].Kind != "" || ranges[1].StartCharacter != nil {
		t.Errorf("second range = %+v, want bare 5..9", ranges[1])
	}
}

func TestFoldingRangesNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/foldingRange": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	ranges, err := c.FoldingRanges(ctx, protocol.FoldingRangeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 0 {
		t.Fatalf("null result should yield no ranges, got %+v", ranges)
	}
}

// TestParseCapabilitiesFoldingRange gates the feature on the boolean-or-object
// provider field (#1912).
func TestParseCapabilitiesFoldingRange(t *testing.T) {
	for raw, want := range map[string]bool{`true`: true, `{}`: true, `{"lineFoldingOnly":true}`: true, `false`: false, ``: false} {
		caps := parseCapabilities(protocol.ServerCapabilities{FoldingRangeProvider: json.RawMessage(raw)})
		if caps.FoldingRange != want {
			t.Errorf("provider %q: FoldingRange = %v, want %v", raw, caps.FoldingRange, want)
		}
	}
}
