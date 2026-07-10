package client

import (
	"context"
	"encoding/json"

	"ike/internal/lsp/protocol"
)

// lifecycle.go drives the LSP handshake: initialize → initialized, and the
// reverse shutdown → exit. Initialize caches the negotiated capabilities so
// feature gating can consult them synchronously.

// InitParams bundles what the manager knows about a server instance.
type InitParams struct {
	RootURI               string
	ProcessID             int
	InitializationOptions json.RawMessage
}

// Initialize performs the initialize request and the initialized notification.
// It advertises a UTF-8 encoding preference (servers that honour it avoid
// surrogate-pair math) while still defaulting to UTF-16 when they decline.
func (c *Client) Initialize(ctx context.Context, p InitParams) (protocol.InitializeResult, error) {
	params := protocol.InitializeParams{
		ProcessID: p.ProcessID,
		RootURI:   p.RootURI,
		Capabilities: protocol.ClientCapabilities{
			General: &protocol.GeneralClientCapabilities{
				PositionEncodings: []string{protocol.EncodingUTF8, protocol.EncodingUTF16},
			},
			TextDocument: &protocol.TextDocumentClientCaps{
				Synchronization: &protocol.SyncClientCaps{DidSave: true},
				Completion:      &protocol.CompletionClientCaps{CompletionItem: &protocol.CompletionItemCaps{SnippetSupport: false}},
				Hover:           &protocol.HoverClientCaps{ContentFormat: []string{"markdown", "plaintext"}},
				Definition:      &protocol.LinkSupportCaps{LinkSupport: true},
				References:      &protocol.ReferencesClientCaps{},
				Formatting:      &protocol.ReferencesClientCaps{},
				RangeFormatting: &protocol.ReferencesClientCaps{},
				Rename:          &protocol.RenameClientCaps{PrepareSupport: true},
			},
		},
		InitializationOptions: p.InitializationOptions,
	}
	if p.RootURI != "" {
		params.WorkspaceFolders = []protocol.WorkspaceFolder{{URI: p.RootURI, Name: "root"}}
	}

	raw, err := c.conn.Call(ctx, "initialize", params)
	if err != nil {
		return protocol.InitializeResult{}, err
	}
	var res protocol.InitializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return protocol.InitializeResult{}, err
	}

	caps := parseCapabilities(res.Capabilities)
	c.mu.Lock()
	c.caps = caps
	c.ready = true
	if res.ServerInfo != nil {
		c.serverNme = res.ServerInfo.Name
	}
	c.mu.Unlock()

	if err := c.conn.Notify("initialized", struct{}{}); err != nil {
		return res, err
	}
	return res, nil
}

// Shutdown asks the server to shut down (request) then exit (notification). It
// is best-effort: errors are returned but the caller closes the conn regardless.
func (c *Client) Shutdown(ctx context.Context) error {
	if _, err := c.conn.Call(ctx, "shutdown", nil); err != nil {
		return err
	}
	return c.conn.Notify("exit", nil)
}
