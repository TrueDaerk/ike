package client

import (
	"context"
	"encoding/json"

	"ike/internal/lsp/protocol"
)

// DocumentSymbols requests every symbol of one document (#1025), the data
// source of the Structure tool pane. Servers answer with either hierarchical
// DocumentSymbol[] or flat SymbolInformation[]; both decode into the
// hierarchical shape — a flat entry becomes a childless node whose Range and
// SelectionRange are its location range. Null or an unexpected shape yields no
// symbols, not an error.
func (c *Client) DocumentSymbols(ctx context.Context, p protocol.DocumentSymbolParams) ([]protocol.DocumentSymbol, error) {
	raw, err := c.call(ctx, "textDocument/documentSymbol", p)
	if err != nil {
		return nil, err
	}
	return decodeDocumentSymbols(raw), nil
}

// decodeDocumentSymbols normalises the union reply. The two shapes are told
// apart per element by the "location" key: only SymbolInformation carries one,
// only DocumentSymbol carries "selectionRange"/"range" at the top level.
func decodeDocumentSymbols(raw json.RawMessage) []protocol.DocumentSymbol {
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil || len(elems) == 0 {
		return nil
	}
	var probe struct {
		Location *protocol.Location `json:"location"`
	}
	if json.Unmarshal(elems[0], &probe) == nil && probe.Location != nil {
		// Flat SymbolInformation[].
		var infos []protocol.SymbolInformation
		if err := json.Unmarshal(raw, &infos); err != nil {
			return nil
		}
		out := make([]protocol.DocumentSymbol, 0, len(infos))
		for _, si := range infos {
			out = append(out, protocol.DocumentSymbol{
				Name:           si.Name,
				Detail:         si.ContainerName,
				Kind:           si.Kind,
				Range:          si.Location.Range,
				SelectionRange: si.Location.Range,
			})
		}
		return out
	}
	var syms []protocol.DocumentSymbol
	if err := json.Unmarshal(raw, &syms); err != nil {
		return nil
	}
	return syms
}
