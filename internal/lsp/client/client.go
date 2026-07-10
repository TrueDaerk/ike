package client

import (
	"context"
	"encoding/json"
	"sync"

	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
)

// Client wraps a jsonrpc.Conn with the typed LSP calls the MVP features use. It
// is created around an already-connected conn (real stdio in production, an
// in-memory pipe in tests), so the client logic is exercised without spawning a
// binary.
type Client struct {
	conn *jsonrpc.Conn

	mu        sync.RWMutex
	caps      Capabilities
	ready     bool
	serverNme string
}

// New builds a client over conn. Call Initialize before using any feature.
func New(conn *jsonrpc.Conn) *Client {
	return &Client{conn: conn, caps: Capabilities{Encoding: protocol.EncodingUTF16, SyncKind: protocol.SyncFull}}
}

// Caps returns the negotiated capabilities (valid after Initialize).
func (c *Client) Caps() Capabilities {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.caps
}

// Encoding returns the negotiated position encoding.
func (c *Client) Encoding() string { return c.Caps().Encoding }

// Ready reports whether the initialize handshake has completed.
func (c *Client) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

// Done exposes the underlying connection's termination channel.
func (c *Client) Done() <-chan struct{} { return c.conn.Done() }

// Close shuts the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Respond answers a server→client request (e.g. workspace/configuration). The
// manager's request handler uses it to keep servers from stalling.
func (c *Client) Respond(id jsonrpc.ID, res any, errObj *jsonrpc.Error) error {
	return c.conn.Respond(id, res, errObj)
}

// --- notifications (no reply) ---

func (c *Client) DidOpen(p protocol.DidOpenTextDocumentParams) error {
	return c.conn.Notify("textDocument/didOpen", p)
}
func (c *Client) DidChange(p protocol.DidChangeTextDocumentParams) error {
	return c.conn.Notify("textDocument/didChange", p)
}
func (c *Client) DidSave(p protocol.DidSaveTextDocumentParams) error {
	return c.conn.Notify("textDocument/didSave", p)
}
func (c *Client) DidClose(p protocol.DidCloseTextDocumentParams) error {
	return c.conn.Notify("textDocument/didClose", p)
}

// --- requests (await a result) ---

// Completion requests completion items; it normalises the `CompletionList | []`
// result shape into a slice.
func (c *Client) Completion(ctx context.Context, p protocol.CompletionParams) ([]protocol.CompletionItem, error) {
	raw, err := c.conn.Call(ctx, "textDocument/completion", p)
	if err != nil {
		return nil, err
	}
	return decodeCompletion(raw), nil
}

// Hover requests hover content.
func (c *Client) Hover(ctx context.Context, p protocol.HoverParams) (*protocol.Hover, error) {
	raw, err := c.conn.Call(ctx, "textDocument/hover", p)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var h protocol.Hover
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// Definition requests definition locations; it normalises `Location | []Location
// | LocationLink[]` into a slice of Locations.
func (c *Client) Definition(ctx context.Context, p protocol.DefinitionParams) ([]protocol.Location, error) {
	raw, err := c.conn.Call(ctx, "textDocument/definition", p)
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw), nil
}

// References requests every reference to the symbol at a position; the result
// is a plain Location array (normalised like Definition for safety).
func (c *Client) References(ctx context.Context, p protocol.ReferenceParams) ([]protocol.Location, error) {
	raw, err := c.conn.Call(ctx, "textDocument/references", p)
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw), nil
}

// Formatting requests whole-document formatting edits.
func (c *Client) Formatting(ctx context.Context, p protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	raw, err := c.conn.Call(ctx, "textDocument/formatting", p)
	if err != nil {
		return nil, err
	}
	return decodeTextEdits(raw), nil
}

// RangeFormatting requests formatting edits for one range.
func (c *Client) RangeFormatting(ctx context.Context, p protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	raw, err := c.conn.Call(ctx, "textDocument/rangeFormatting", p)
	if err != nil {
		return nil, err
	}
	return decodeTextEdits(raw), nil
}

// decodeTextEdits accepts a TextEdit array or null.
func decodeTextEdits(raw json.RawMessage) []protocol.TextEdit {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var edits []protocol.TextEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		return nil
	}
	return edits
}

// decodeCompletion accepts either a CompletionList or a bare item array.
func decodeCompletion(raw json.RawMessage) []protocol.CompletionItem {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list protocol.CompletionList
	if err := json.Unmarshal(raw, &list); err == nil && list.Items != nil {
		return list.Items
	}
	var items []protocol.CompletionItem
	if err := json.Unmarshal(raw, &items); err == nil {
		return items
	}
	return nil
}

// decodeLocations accepts a single Location, an array of Locations, or an array
// of LocationLinks (which expose targetUri/targetSelectionRange).
func decodeLocations(raw json.RawMessage) []protocol.Location {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var single protocol.Location
	if err := json.Unmarshal(raw, &single); err == nil && single.URI != "" {
		return []protocol.Location{single}
	}
	var locs []protocol.Location
	if err := json.Unmarshal(raw, &locs); err == nil && len(locs) > 0 && locs[0].URI != "" {
		return locs
	}
	var links []struct {
		TargetURI            string         `json:"targetUri"`
		TargetSelectionRange protocol.Range `json:"targetSelectionRange"`
	}
	if err := json.Unmarshal(raw, &links); err == nil {
		out := make([]protocol.Location, 0, len(links))
		for _, l := range links {
			if l.TargetURI != "" {
				out = append(out, protocol.Location{URI: l.TargetURI, Range: l.TargetSelectionRange})
			}
		}
		return out
	}
	return nil
}
