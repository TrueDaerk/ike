// Package client is one running language server seen through a typed
// request/notify API over a jsonrpc.Conn (Roadmap 0100). It owns the initialize
// handshake, caches the negotiated capabilities, and gates features so a missing
// capability is a graceful no-op rather than an error.
package client

import (
	"encoding/json"

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
	Hover              bool
	Definition         bool
	References         bool
	Formatting         bool
	RangeFormatting    bool
	Rename             bool
	PrepareRename      bool
	CodeAction         bool
	ExecuteCommand     bool
	SignatureHelp      bool
	SignatureTriggers  []string
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
	}
	caps.Hover = truthyProvider(sc.HoverProvider)
	caps.Definition = truthyProvider(sc.DefinitionProvider)
	caps.References = truthyProvider(sc.ReferencesProvider)
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
	caps.ExecuteCommand = truthyProvider(sc.ExecuteCommandProvider)
	if sc.SignatureHelpProvider != nil {
		caps.SignatureHelp = true
		caps.SignatureTriggers = append(sc.SignatureHelpProvider.TriggerCharacters, sc.SignatureHelpProvider.RetriggerCharacters...)
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
