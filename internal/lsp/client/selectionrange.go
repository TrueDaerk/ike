package client

import (
	"context"
	"encoding/json"

	"ike/internal/lsp/protocol"
)

// SelectionRanges requests the syntactic expansion ladder at each position
// (#1912): one SelectionRange per requested position, each a linked list from
// the innermost range outwards via Parent. A null result (no ladder) or an
// unexpected shape yields an empty slice, not an error.
func (c *Client) SelectionRanges(ctx context.Context, p protocol.SelectionRangeParams) ([]protocol.SelectionRange, error) {
	raw, err := c.call(ctx, "textDocument/selectionRange", p)
	if err != nil {
		return nil, err
	}
	var ranges []protocol.SelectionRange
	if err := json.Unmarshal(raw, &ranges); err != nil {
		return nil, nil // null / unexpected shape: no ladder
	}
	return ranges, nil
}
