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

	// Handshake gate (#937): the LSP spec forbids any client traffic between
	// the initialize request and the initialized notification, and servers die
	// on violations (Intelephense crashes when didOpen races its async
	// initialize handler). Notifications sent before the handshake completes
	// are queued in pending and flushed — after initialized, in order — by
	// Initialize; requests block on initDone. handshook mirrors the closed
	// state of initDone so notify can test it under mu.
	initOnce  sync.Once
	initDone  chan struct{}
	handshook bool
	pending   []pendingNote
}

// pendingNote is a notification held back while the handshake is in flight.
type pendingNote struct {
	method string
	params any
}

// New builds a client over conn. Call Initialize before using any feature.
func New(conn *jsonrpc.Conn) *Client {
	return &Client{
		conn:     conn,
		caps:     Capabilities{Encoding: protocol.EncodingUTF16, SyncKind: protocol.SyncFull},
		initDone: make(chan struct{}),
	}
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

// notify sends a notification, or queues it while the initialize handshake is
// still in flight (#937); Initialize flushes the queue in order right after
// the initialized notification. A queued send reports success: if the
// handshake fails the connection is torn down anyway and the manager re-opens
// documents on the respawned server.
func (c *Client) notify(method string, params any) error {
	c.mu.Lock()
	if !c.handshook {
		c.pending = append(c.pending, pendingNote{method: method, params: params})
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	return c.conn.Notify(method, params)
}

// call issues a request once the handshake has completed (#937): no client
// request may reach the server before the initialize response. ctx bounds the
// wait exactly like the call itself.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	select {
	case <-c.initDone:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.conn.Done():
		return nil, jsonrpc.ErrClosed
	}
	return c.conn.Call(ctx, method, params)
}

func (c *Client) DidOpen(p protocol.DidOpenTextDocumentParams) error {
	return c.notify("textDocument/didOpen", p)
}
func (c *Client) DidChange(p protocol.DidChangeTextDocumentParams) error {
	return c.notify("textDocument/didChange", p)
}
func (c *Client) DidSave(p protocol.DidSaveTextDocumentParams) error {
	return c.notify("textDocument/didSave", p)
}
func (c *Client) DidClose(p protocol.DidCloseTextDocumentParams) error {
	return c.notify("textDocument/didClose", p)
}

// DidChangeWatchedFiles announces external file creations/changes/deletions
// (#1144) so workspace-indexing servers (Intelephense) refresh their view of
// files IKE never opened.
func (c *Client) DidChangeWatchedFiles(p protocol.DidChangeWatchedFilesParams) error {
	return c.notify("workspace/didChangeWatchedFiles", p)
}

// --- requests (await a result) ---

// Completion requests completion items; it normalises the `CompletionList | []`
// result shape into a slice. incomplete mirrors the list's isIncomplete flag
// (#849): the reply is a partial view and further typing must re-query rather
// than filter it client-side.
func (c *Client) Completion(ctx context.Context, p protocol.CompletionParams) (items []protocol.CompletionItem, incomplete bool, err error) {
	raw, err := c.call(ctx, "textDocument/completion", p)
	if err != nil {
		return nil, false, err
	}
	items, incomplete = decodeCompletion(raw)
	return items, incomplete, nil
}

// Resolve requests completionItem/resolve for one item (#847): servers ship
// lean completion lists and attach documentation / additionalTextEdits lazily.
func (c *Client) Resolve(ctx context.Context, item protocol.CompletionItem) (protocol.CompletionItem, error) {
	raw, err := c.call(ctx, "completionItem/resolve", item)
	if err != nil {
		return item, err
	}
	var out protocol.CompletionItem
	if err := json.Unmarshal(raw, &out); err != nil {
		return item, err
	}
	return out, nil
}

// Hover requests hover content.
func (c *Client) Hover(ctx context.Context, p protocol.HoverParams) (*protocol.Hover, error) {
	raw, err := c.call(ctx, "textDocument/hover", p)
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
	raw, err := c.call(ctx, "textDocument/definition", p)
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw), nil
}

// References requests every reference to the symbol at a position; the result
// is a plain Location array (normalised like Definition for safety).
func (c *Client) References(ctx context.Context, p protocol.ReferenceParams) ([]protocol.Location, error) {
	raw, err := c.call(ctx, "textDocument/references", p)
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw), nil
}

// DocumentHighlight requests the occurrences of the symbol at a position
// (#172). A null result (position not on a symbol) is an empty slice.
func (c *Client) DocumentHighlight(ctx context.Context, p protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	raw, err := c.call(ctx, "textDocument/documentHighlight", p)
	if err != nil {
		return nil, err
	}
	var hs []protocol.DocumentHighlight
	if err := json.Unmarshal(raw, &hs); err != nil {
		return nil, nil // null / unexpected shape: nothing to mark
	}
	return hs, nil
}

// InlayHints requests the inline hints within a document range (#171). A null
// result (nothing to hint) is an empty slice.
func (c *Client) InlayHints(ctx context.Context, p protocol.InlayHintParams) ([]protocol.InlayHint, error) {
	raw, err := c.call(ctx, "textDocument/inlayHint", p)
	if err != nil {
		return nil, err
	}
	var hints []protocol.InlayHint
	if err := json.Unmarshal(raw, &hints); err != nil {
		return nil, nil // null / unexpected shape: nothing to show
	}
	return hints, nil
}

// PrepareCallHierarchy resolves the symbol at a position into call-hierarchy
// items (#173). A null result (position not on a callable) is an empty slice.
func (c *Client) PrepareCallHierarchy(ctx context.Context, p protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error) {
	raw, err := c.call(ctx, "textDocument/prepareCallHierarchy", p)
	if err != nil {
		return nil, err
	}
	var items []protocol.CallHierarchyItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, nil // null / unexpected shape: nothing to show
	}
	return items, nil
}

// IncomingCalls requests the callers of a prepared item (#173).
func (c *Client) IncomingCalls(ctx context.Context, p protocol.CallHierarchyCallsParams) ([]protocol.CallHierarchyIncomingCall, error) {
	raw, err := c.call(ctx, "callHierarchy/incomingCalls", p)
	if err != nil {
		return nil, err
	}
	var calls []protocol.CallHierarchyIncomingCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, nil
	}
	return calls, nil
}

// OutgoingCalls requests the callees of a prepared item (#173).
func (c *Client) OutgoingCalls(ctx context.Context, p protocol.CallHierarchyCallsParams) ([]protocol.CallHierarchyOutgoingCall, error) {
	raw, err := c.call(ctx, "callHierarchy/outgoingCalls", p)
	if err != nil {
		return nil, err
	}
	var calls []protocol.CallHierarchyOutgoingCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, nil
	}
	return calls, nil
}

// WorkspaceSymbols requests project-wide symbols matching query (0250, #294).
// Servers may answer with SymbolInformation[] or the newer WorkspaceSymbol[]
// (whose location may lack a range); both decode into the classic shape, and
// entries without a usable URI are dropped.
func (c *Client) WorkspaceSymbols(ctx context.Context, p protocol.WorkspaceSymbolParams) ([]protocol.SymbolInformation, error) {
	raw, err := c.call(ctx, "workspace/symbol", p)
	if err != nil {
		return nil, err
	}
	var syms []protocol.SymbolInformation
	if err := json.Unmarshal(raw, &syms); err != nil {
		return nil, nil // null / unexpected shape: no symbols
	}
	out := syms[:0]
	for _, s := range syms {
		if s.Location.URI != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// Formatting requests whole-document formatting edits.
func (c *Client) Formatting(ctx context.Context, p protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	raw, err := c.call(ctx, "textDocument/formatting", p)
	if err != nil {
		return nil, err
	}
	return decodeTextEdits(raw), nil
}

// RangeFormatting requests formatting edits for one range.
func (c *Client) RangeFormatting(ctx context.Context, p protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	raw, err := c.call(ctx, "textDocument/rangeFormatting", p)
	if err != nil {
		return nil, err
	}
	return decodeTextEdits(raw), nil
}

// PrepareRename validates a rename position. ok is false when the server
// rejects the position (null result); the returned range is zero when the
// server answered with defaultBehavior only.
func (c *Client) PrepareRename(ctx context.Context, p protocol.PrepareRenameParams) (protocol.Range, bool, error) {
	raw, err := c.call(ctx, "textDocument/prepareRename", p)
	if err != nil {
		return protocol.Range{}, false, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return protocol.Range{}, false, nil
	}
	// Range | { range, placeholder } | { defaultBehavior: true }
	var withRange struct {
		Range           *protocol.Range    `json:"range"`
		Start           *protocol.Position `json:"start"`
		DefaultBehavior bool               `json:"defaultBehavior"`
	}
	if err := json.Unmarshal(raw, &withRange); err == nil {
		if withRange.Range != nil {
			return *withRange.Range, true, nil
		}
		if withRange.Start != nil { // bare Range shape
			var r protocol.Range
			if json.Unmarshal(raw, &r) == nil {
				return r, true, nil
			}
		}
		if withRange.DefaultBehavior {
			return protocol.Range{}, true, nil
		}
	}
	var r protocol.Range
	if err := json.Unmarshal(raw, &r); err == nil {
		return r, true, nil
	}
	return protocol.Range{}, false, nil
}

// Rename requests the workspace-wide edit for renaming the symbol at a
// position. A null result decodes to an empty edit.
func (c *Client) Rename(ctx context.Context, p protocol.RenameParams) (protocol.WorkspaceEdit, error) {
	raw, err := c.call(ctx, "textDocument/rename", p)
	if err != nil {
		return protocol.WorkspaceEdit{}, err
	}
	var we protocol.WorkspaceEdit
	if len(raw) == 0 || string(raw) == "null" {
		return we, nil
	}
	_ = json.Unmarshal(raw, &we)
	return we, nil
}

// CodeActions requests the actions available for a range. The result mixes
// CodeAction and bare Command entries; both decode into CodeAction (a bare
// command becomes a command-only action).
func (c *Client) CodeActions(ctx context.Context, p protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	raw, err := c.call(ctx, "textDocument/codeAction", p)
	if err != nil {
		return nil, err
	}
	return decodeCodeActions(raw), nil
}

// ExecuteCommand runs a server-defined command; effects come back as
// workspace/applyEdit requests, so the result payload is ignored.
func (c *Client) ExecuteCommand(ctx context.Context, p protocol.ExecuteCommandParams) error {
	_, err := c.call(ctx, "workspace/executeCommand", p)
	return err
}

// decodeCodeActions accepts (Command | CodeAction)[] or null. The two shapes
// share "title"; a bare Command carries "command" as a string, a CodeAction
// as an object — probed per element.
func decodeCodeActions(raw json.RawMessage) []protocol.CodeAction {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	out := make([]protocol.CodeAction, 0, len(items))
	for _, item := range items {
		var probe struct {
			Command json.RawMessage `json:"command"`
		}
		_ = json.Unmarshal(item, &probe)
		if len(probe.Command) > 0 && probe.Command[0] == '"' {
			var cmd protocol.Command
			if json.Unmarshal(item, &cmd) == nil && cmd.Title != "" {
				out = append(out, protocol.CodeAction{Title: cmd.Title, Command: &cmd})
			}
			continue
		}
		var act protocol.CodeAction
		if json.Unmarshal(item, &act) == nil && act.Title != "" {
			out = append(out, act)
		}
	}
	return out
}

// SignatureHelp requests call-signature info; null decodes to nil.
func (c *Client) SignatureHelp(ctx context.Context, p protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	raw, err := c.call(ctx, "textDocument/signatureHelp", p)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var sh protocol.SignatureHelp
	if err := json.Unmarshal(raw, &sh); err != nil || len(sh.Signatures) == 0 {
		return nil, nil
	}
	return &sh, nil
}

// SemanticTokensFull requests the whole document's packed semantic tokens.
func (c *Client) SemanticTokensFull(ctx context.Context, p protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	raw, err := c.call(ctx, "textDocument/semanticTokens/full", p)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var st protocol.SemanticTokens
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, nil
	}
	return &st, nil
}

// SemanticTokensDelta requests edits against a previous result. Servers may
// answer with either a delta (edits) or a fresh full result (data) — exactly
// one of the returns is non-nil on success.
func (c *Client) SemanticTokensDelta(ctx context.Context, p protocol.SemanticTokensDeltaParams) (*protocol.SemanticTokensDelta, *protocol.SemanticTokens, error) {
	raw, err := c.call(ctx, "textDocument/semanticTokens/full/delta", p)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, nil
	}
	var probe struct {
		Edits json.RawMessage `json:"edits"`
		Data  json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(raw, &probe)
	if len(probe.Edits) > 0 {
		var d protocol.SemanticTokensDelta
		if err := json.Unmarshal(raw, &d); err == nil {
			return &d, nil, nil
		}
	}
	if len(probe.Data) > 0 {
		var st protocol.SemanticTokens
		if err := json.Unmarshal(raw, &st); err == nil {
			return nil, &st, nil
		}
	}
	return nil, nil, nil
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

// decodeCompletion accepts either a CompletionList (carrying isIncomplete,
// #849) or a bare item array (always complete).
func decodeCompletion(raw json.RawMessage) ([]protocol.CompletionItem, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var list protocol.CompletionList
	if err := json.Unmarshal(raw, &list); err == nil && list.Items != nil {
		return list.Items, list.IsIncomplete
	}
	var items []protocol.CompletionItem
	if err := json.Unmarshal(raw, &items); err == nil {
		return items, false
	}
	return nil, false
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
