package client

import (
	"context"
	"encoding/json"

	"ike/internal/lsp/protocol"
)

// codeaction.go holds the lazy half of textDocument/codeAction (#2252):
// servers may offer actions without an edit and compute it only when the
// client asks, which is what the intention popup's diff preview needs before
// anything is applied.

// ResolveCodeAction requests codeAction/resolve for one lazily computed
// action. Null, a decode failure or an error returns the input action
// unchanged — the caller keeps the offer it had (title, kind, command)
// rather than losing the row over a missing edit.
func (c *Client) ResolveCodeAction(ctx context.Context, action protocol.CodeAction) (protocol.CodeAction, error) {
	raw, err := c.call(ctx, "codeAction/resolve", action)
	if err != nil {
		return action, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return action, nil
	}
	var out protocol.CodeAction
	if err := json.Unmarshal(raw, &out); err != nil || out.Title == "" {
		return action, nil
	}
	return out, nil
}
