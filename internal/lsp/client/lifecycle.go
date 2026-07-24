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
		ProcessID:             p.ProcessID,
		RootURI:               p.RootURI,
		Capabilities:          clientCapabilities(),
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

	// Open the handshake gate (#937): flush the traffic queued while the
	// handshake was in flight — still under mu, so a concurrent notify cannot
	// interleave ahead of the queue — then let requests through. Everything
	// lands behind initialized in the conn's FIFO write queue.
	c.initOnce.Do(func() {
		c.mu.Lock()
		queued := c.pending
		c.pending = nil
		for _, n := range queued {
			_ = c.conn.Notify(n.method, n.params)
		}
		c.handshook = true
		c.mu.Unlock()
		close(c.initDone)
	})
	return res, nil
}

// clientCapabilities is the full capability set IKE advertises in initialize.
// Kept as its own function so tests can assert the wire payload (#1060).
func clientCapabilities() protocol.ClientCapabilities {
	return protocol.ClientCapabilities{
		General: &protocol.GeneralClientCapabilities{
			PositionEncodings: []string{protocol.EncodingUTF8, protocol.EncodingUTF16},
		},
		// Without workspace.configuration a server never issues
		// workspace/configuration, so pyright never pulls the detected
		// Python interpreter (python.pythonPath) and falls back to the
		// system interpreter — venv imports then resolve as errors (#563).
		Workspace: &protocol.WorkspaceClientCaps{
			Configuration:          true,
			DidChangeConfiguration: &protocol.DidChangeConfigurationCaps{DynamicRegistration: true},
			// Watched files (#1144): servers that see this register their
			// globs via client/registerCapability and expect
			// workspace/didChangeWatchedFiles for external create/change/
			// delete — Intelephense re-indexes new files only through it.
			DidChangeWatchedFiles: &protocol.DidChangeWatchedFilesCaps{DynamicRegistration: true},
		},
		TextDocument: &protocol.TextDocumentClientCaps{
			Synchronization: &protocol.SyncClientCaps{DidSave: true},
			Completion:      &protocol.CompletionClientCaps{CompletionItem: &protocol.CompletionItemCaps{SnippetSupport: true}},
			Hover:           &protocol.HoverClientCaps{ContentFormat: []string{"markdown", "plaintext"}},
			Definition:      &protocol.LinkSupportCaps{LinkSupport: true},
			References:      &protocol.ReferencesClientCaps{},
			Formatting:      &protocol.ReferencesClientCaps{},
			RangeFormatting: &protocol.ReferencesClientCaps{},
			Rename:          &protocol.RenameClientCaps{PrepareSupport: true},
			CodeAction:      &protocol.ReferencesClientCaps{},
			SignatureHelp:   &protocol.ReferencesClientCaps{},
			CallHierarchy:   &protocol.ReferencesClientCaps{},
			InlayHint:       &protocol.ReferencesClientCaps{},
			DocumentSymbol:  &protocol.DocumentSymbolClientCaps{HierarchicalDocumentSymbolSupport: true},
			// #1060: vtsls gates push diagnostics on this capability — with
			// it absent it never sends textDocument/publishDiagnostics.
			PublishDiagnostics: &protocol.PublishDiagnosticsClientCaps{RelatedInformation: true},
			SemanticTokens: &protocol.SemanticTokensClientCaps{
				Requests:       protocol.SemanticTokensRequests{Full: &protocol.SemanticTokensFullRequest{Delta: true}},
				TokenTypes:     protocol.StandardTokenTypes,
				TokenModifiers: protocol.StandardTokenModifiers,
				Formats:        []string{"relative"},
			},
		},
	}
}

// Shutdown asks the server to shut down (request) then exit (notification). It
// is best-effort: errors are returned but the caller closes the conn regardless.
func (c *Client) Shutdown(ctx context.Context) error {
	if _, err := c.conn.Call(ctx, "shutdown", nil); err != nil {
		return err
	}
	return c.conn.Notify("exit", nil)
}
