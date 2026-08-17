package client

import (
	"context"
	"encoding/json"

	"ike/internal/lsp/protocol"
)

// WillRenameFiles asks the server for the workspace edit a pending file/folder
// rename implies (#1912) — gopls rewrites the import paths of a moved package
// this way. A null result (nothing to rewrite) or an unexpected shape yields
// nil, not an error.
func (c *Client) WillRenameFiles(ctx context.Context, p protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) {
	raw, err := c.call(ctx, "workspace/willRenameFiles", p)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var we protocol.WorkspaceEdit
	if err := json.Unmarshal(raw, &we); err != nil {
		return nil, nil // unexpected shape: nothing to rewrite
	}
	return &we, nil
}
