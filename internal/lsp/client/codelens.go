package client

import (
	"context"
	"encoding/json"

	"ike/internal/lsp/protocol"
)

// CodeLens requests every code lens of one document (#1912). A null result
// (nothing to show) or an unexpected shape yields an empty slice, not an error.
func (c *Client) CodeLens(ctx context.Context, p protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	raw, err := c.call(ctx, "textDocument/codeLens", p)
	if err != nil {
		return nil, err
	}
	var lenses []protocol.CodeLens
	if err := json.Unmarshal(raw, &lenses); err != nil {
		return nil, nil // null / unexpected shape: nothing to show
	}
	return lenses, nil
}

// ResolveCodeLens requests codeLens/resolve for one unresolved lens (#1912):
// servers ship lean lens lists and compute the command lazily. Null or a
// decode failure returns the input lens unchanged — the caller keeps what it
// had rather than losing the anchor.
func (c *Client) ResolveCodeLens(ctx context.Context, lens protocol.CodeLens) (protocol.CodeLens, error) {
	raw, err := c.call(ctx, "codeLens/resolve", lens)
	if err != nil {
		return lens, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return lens, nil
	}
	var out protocol.CodeLens
	if err := json.Unmarshal(raw, &out); err != nil {
		return lens, nil
	}
	return out, nil
}
