package lsp

import (
	"encoding/json"
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/lsp/protocol"
)

// messages.go defines the editor-facing tea.Msg types LSP results arrive as, plus
// the conversion helpers that map wire (protocol, UTF-16) data into editor
// (rune-column) coordinates. These types are the contract the editor's Update
// consumes; the bridge builds them and the host injects them via Send.

// DiagnosticsMsg replaces the full diagnostic set for one document. Version is the
// editor doc version is NOT used here (diagnostics carry the server's own version);
// the editor keys diagnostics by path only and always takes the latest set.
type DiagnosticsMsg struct {
	Path        string
	Diagnostics []Diagnostic
}

// Diagnostic is one diagnostic in editor coordinates.
type Diagnostic struct {
	Range    buffer.Range
	Severity int // protocol.Severity* (1=error … 4=hint)
	Message  string
	Source   string
}

// CompletionMsg delivers completion items for an in-flight request, anchored at
// the cursor position the request was issued from.
type CompletionMsg struct {
	Path  string
	Line  int
	Col   int
	Items []CompletionItem
}

// CompletionItem is the editor-facing completion entry.
type CompletionItem struct {
	Label      string
	Detail     string
	InsertText string
	Kind       int
}

// HoverMsg delivers hover content (already flattened to text) for a popup.
type HoverMsg struct {
	Path     string
	Contents string
}

// DefinitionMsg asks the host to navigate to a definition target. Line/Col are
// editor coordinates in the target file; the app handles navigation.
type DefinitionMsg struct {
	Path string
	Line int
	Col  int
}

// ServerStatusMsg drives the status line (server ready / crashed / disabled).
type ServerStatusMsg struct {
	Text string
}

// ConvertDiagnostics maps protocol diagnostics to editor coordinates using the
// document's current lines and the negotiated encoding.
func ConvertDiagnostics(p protocol.PublishDiagnosticsParams, lines []string, enc string) []Diagnostic {
	if enc == "" {
		enc = protocol.EncodingUTF16
	}
	out := make([]Diagnostic, 0, len(p.Diagnostics))
	for _, d := range p.Diagnostics {
		out = append(out, Diagnostic{
			Range:    protocol.FromLSPRange(lines, d.Range, enc),
			Severity: d.Severity,
			Message:  d.Message,
			Source:   d.Source,
		})
	}
	return out
}

// ConvertCompletion maps protocol items to editor items, falling back to the
// label when insertText/textEdit are absent.
func ConvertCompletion(items []protocol.CompletionItem) []CompletionItem {
	out := make([]CompletionItem, 0, len(items))
	for _, it := range items {
		insert := it.InsertText
		if it.TextEdit != nil && it.TextEdit.NewText != "" {
			insert = it.TextEdit.NewText
		}
		if insert == "" {
			insert = it.Label
		}
		out = append(out, CompletionItem{
			Label:      it.Label,
			Detail:     it.Detail,
			InsertText: insert,
			Kind:       it.Kind,
		})
	}
	return out
}

// HoverText flattens a protocol.Hover's contents (MarkupContent, a string, or an
// array of marked strings) into plain display text.
func HoverText(h *protocol.Hover) string {
	if h == nil || len(h.Contents) == 0 {
		return ""
	}
	// MarkupContent: { kind, value }.
	var mc protocol.MarkupContent
	if err := json.Unmarshal(h.Contents, &mc); err == nil && mc.Value != "" {
		return strings.TrimSpace(mc.Value)
	}
	// Bare string.
	var s string
	if err := json.Unmarshal(h.Contents, &s); err == nil && s != "" {
		return strings.TrimSpace(s)
	}
	// Array of strings or { language, value } objects.
	var arr []json.RawMessage
	if err := json.Unmarshal(h.Contents, &arr); err == nil {
		var parts []string
		for _, e := range arr {
			var es string
			if json.Unmarshal(e, &es) == nil && es != "" {
				parts = append(parts, es)
				continue
			}
			var ms struct {
				Value string `json:"value"`
			}
			if json.Unmarshal(e, &ms) == nil && ms.Value != "" {
				parts = append(parts, ms.Value)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}
