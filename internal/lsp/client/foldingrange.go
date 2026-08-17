package client

import (
	"context"
	"encoding/json"

	"ike/internal/lsp/protocol"
)

// FoldingRanges requests every foldable region of one document (#1912). A null
// result (nothing to fold) or an unexpected shape yields an empty slice, not
// an error.
func (c *Client) FoldingRanges(ctx context.Context, p protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	raw, err := c.call(ctx, "textDocument/foldingRange", p)
	if err != nil {
		return nil, err
	}
	var ranges []protocol.FoldingRange
	if err := json.Unmarshal(raw, &ranges); err != nil {
		return nil, nil // null / unexpected shape: nothing to fold
	}
	return ranges, nil
}
