// Package client is one running language server seen through a typed
// request/notify API over a jsonrpc.Conn (Roadmap 0100). It owns the initialize
// handshake, caches the negotiated capabilities, and gates features so a missing
// capability is a graceful no-op rather than an error.
package client

import (
	"encoding/json"
	"strings"

	"ike/internal/lsp/protocol"
)

// Capabilities is the decoded, feature-gated view of a server's
// ServerCapabilities. Defaults are conservative: UTF-16 encoding, full-document
// sync, no optional features.
type Capabilities struct {
	Encoding           string
	SyncKind           int
	Completion         bool
	CompletionTriggers []string
	CompletionResolve  bool
	Hover              bool
	Definition         bool
	References         bool
	DocumentHighlight  bool
	InlayHint          bool
	Formatting         bool
	RangeFormatting    bool
	Rename             bool
	PrepareRename      bool
	CodeAction         bool
	CodeActionKinds    []string
	ExecuteCommand     bool
	SignatureHelp      bool
	SignatureTriggers  []string
	SemanticTokens     bool
	SemanticDelta      bool
	SemanticTypes      []string
	SemanticModifiers  []string
	WorkspaceSymbol    bool
	CallHierarchy      bool
	DocumentSymbol     bool
}

// OffersCodeActionKind reports whether the server declared kind — or one of
// its parent kinds, per the LSP's hierarchical kind scheme — in its
// codeActionKinds (#1148). An empty list means the server did not say (bare
// `true` provider) and counts as offered: the filtered request's empty answer
// is the graceful no-op then.
func (c Capabilities) OffersCodeActionKind(kind string) bool {
	if !c.CodeAction {
		return false
	}
	if len(c.CodeActionKinds) == 0 {
		return true
	}
	for _, k := range c.CodeActionKinds {
		if k == kind || (k != "" && strings.HasPrefix(kind, k+".")) {
			return true
		}
	}
	return false
}

// parseCapabilities decodes the raw ServerCapabilities into the gated view,
// honouring the negotiated position encoding and the (possibly object-or-number)
// sync kind.
func parseCapabilities(sc protocol.ServerCapabilities) Capabilities {
	caps := Capabilities{
		Encoding: protocol.EncodingUTF16,
		SyncKind: protocol.SyncFull,
	}
	if sc.PositionEncoding != "" {
		caps.Encoding = sc.PositionEncoding
	}
	caps.SyncKind = parseSyncKind(sc.TextDocumentSync)
	if sc.CompletionProvider != nil {
		caps.Completion = true
		caps.CompletionTriggers = sc.CompletionProvider.TriggerCharacters
		caps.CompletionResolve = sc.CompletionProvider.ResolveProvider
	}
	caps.Hover = truthyProvider(sc.HoverProvider)
	caps.Definition = truthyProvider(sc.DefinitionProvider)
	caps.References = truthyProvider(sc.ReferencesProvider)
	caps.DocumentHighlight = truthyProvider(sc.DocumentHighlightProvider)
	caps.InlayHint = truthyProvider(sc.InlayHintProvider)
	caps.WorkspaceSymbol = truthyProvider(sc.WorkspaceSymbolProvider)
	caps.CallHierarchy = truthyProvider(sc.CallHierarchyProvider)
	caps.DocumentSymbol = truthyProvider(sc.DocumentSymbolProvider)
	caps.Formatting = truthyProvider(sc.DocumentFormattingProvider)
	caps.RangeFormatting = truthyProvider(sc.DocumentRangeFormattingProvider)
	caps.Rename = truthyProvider(sc.RenameProvider)
	if caps.Rename {
		var opts struct {
			PrepareProvider bool `json:"prepareProvider"`
		}
		if json.Unmarshal(sc.RenameProvider, &opts) == nil {
			caps.PrepareRename = opts.PrepareProvider
		}
	}
	caps.CodeAction = truthyProvider(sc.CodeActionProvider)
	if caps.CodeAction {
		// A CodeActionOptions object may declare the offered kinds (#1148);
		// a bare `true` provider leaves the list empty (= unknown).
		var opts struct {
			CodeActionKinds []string `json:"codeActionKinds"`
		}
		if json.Unmarshal(sc.CodeActionProvider, &opts) == nil {
			caps.CodeActionKinds = opts.CodeActionKinds
		}
	}
	caps.ExecuteCommand = truthyProvider(sc.ExecuteCommandProvider)
	if sc.SignatureHelpProvider != nil {
		caps.SignatureHelp = true
		caps.SignatureTriggers = append(sc.SignatureHelpProvider.TriggerCharacters, sc.SignatureHelpProvider.RetriggerCharacters...)
	}
	if st := sc.SemanticTokensProvider; st != nil && truthyProvider(st.Full) {
		caps.SemanticTokens = true
		caps.SemanticTypes = st.Legend.TokenTypes
		caps.SemanticModifiers = st.Legend.TokenModifiers
		var full struct {
			Delta bool `json:"delta"`
		}
		if json.Unmarshal(st.Full, &full) == nil {
			caps.SemanticDelta = full.Delta
		}
	}
	return caps
}

// parseSyncKind extracts the change-sync kind from either a bare number or a
// `{ openClose, change }` object; absent/unknown falls back to full sync.
func parseSyncKind(raw json.RawMessage) int {
	if len(raw) == 0 {
		return protocol.SyncFull
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var obj struct {
		Change int `json:"change"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Change
	}
	return protocol.SyncFull
}

// truthyProvider reports whether a `boolean | options-object | null` provider
// field indicates support (true or any object).
func truthyProvider(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	// A non-bool (an options object) means supported; null already decodes to
	// false above only if it were bool, so guard explicitly.
	return string(raw) != "null"
}
