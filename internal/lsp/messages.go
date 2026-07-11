package lsp

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
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

// Reference is one find-references result in editor coordinates, with the
// target line's trimmed text as a preview for the results list.
type Reference struct {
	Path    string
	Line    int
	Col     int
	Preview string
}

// ReferencesMsg delivers the find-references results (lsp.references). The
// app renders them as a navigable list; an empty slice means the server found
// nothing (surfaced as a notification, not a list).
type ReferencesMsg struct {
	Refs []Reference
}

// DocumentHighlight is one occurrence of the symbol under the cursor in
// editor coordinates (#172). Kind is protocol.Highlight* (text/read/write).
type DocumentHighlight struct {
	Range buffer.Range
	Kind  int
}

// DocumentHighlightsMsg replaces the occurrence set for one document (#172).
// Line/Col anchor the request position so a reply that raced a cursor move is
// recognisably stale; an empty set clears the marks.
type DocumentHighlightsMsg struct {
	Path       string
	Line       int
	Col        int
	Highlights []DocumentHighlight
}

// InlayHint is one inline hint in editor coordinates (#171): Label is the
// flattened hint text, Kind is protocol.InlayHint* (type/parameter, 0 when the
// server left it unclassified), PadLeft/PadRight ask for a separating space.
type InlayHint struct {
	Line     int
	Col      int
	Label    string
	Kind     int
	PadLeft  bool
	PadRight bool
}

// InlayHintsMsg replaces the inlay-hint set for one document (#171); an empty
// set clears the hints.
type InlayHintsMsg struct {
	Path  string
	Hints []InlayHint
}

// CallHierarchyEntry is one call-hierarchy node payload (#173): the raw
// protocol item (kept verbatim for the incoming/outgoing follow-up requests)
// plus its presentation fields and the navigation target in editor
// coordinates — the call site for a caller row, the declaration for a callee.
type CallHierarchyEntry struct {
	Item   protocol.CallHierarchyItem
	Name   string
	Detail string
	Path   string
	Line   int
	Col    int
}

// CallHierarchyMsg opens the call-hierarchy overlay (lsp.callHierarchy, #173)
// on the prepared root items. Fetch is the bridge-built continuation the
// overlay runs to expand a node lazily; the result arrives as a
// CallHierarchyCallsMsg carrying the same ReqID — the manager stays
// unreachable from the app, as with every other LSP action.
type CallHierarchyMsg struct {
	Path  string
	Roots []CallHierarchyEntry
	Fetch func(reqID int, item protocol.CallHierarchyItem, incoming bool) tea.Cmd
}

// CallHierarchyCallsMsg delivers one node expansion: the callers (Incoming)
// or callees of the item requested under ReqID. An empty slice marks the node
// a leaf.
type CallHierarchyCallsMsg struct {
	ReqID    int
	Incoming bool
	Calls    []CallHierarchyEntry
}

// SymbolPromptMsg asks the app to prompt for a workspace-symbol query (0250,
// #294); Apply runs the workspace/symbol request with the typed query.
type SymbolPromptMsg struct {
	Apply func(query string) tea.Cmd
}

// SymbolHit is one workspace/symbol result: the symbol's name (what the
// palette row leads with, #295) plus its location as an editor-coordinate
// Reference (the preview doubles as the declaration line).
type SymbolHit struct {
	Name string
	Ref  Reference
}

// SymbolResultsMsg delivers the workspace/symbol hits, converted to editor
// coordinates like ReferencesMsg. NoProvider reports that no running server
// advertises the capability, so the app can hint instead of staying silent.
type SymbolResultsMsg struct {
	Query      string
	Hits       []SymbolHit
	NoProvider bool
}

// DefinitionCandidatesMsg delivers a go-to-definition result with several
// target locations (#279): instead of guessing the first, the app renders the
// candidates as a palette list; picking one navigates through the same
// DefinitionMsg path a single-target jump uses.
type DefinitionCandidatesMsg struct {
	Refs []Reference
}

// FormatEdit is one formatting rewrite in 0-based editor rune coordinates:
// the [Start, End) span becomes Text. Positions are already converted from the
// server's encoding (protocol/convert.go) by the manager.
type FormatEdit struct {
	StartLine, StartCol int
	EndLine, EndCol     int
	Text                string
}

// FormatEditsMsg delivers formatting edits for the app to route to the editor
// owning Path, which applies them as one undo unit.
type FormatEditsMsg struct {
	Path  string
	Edits []FormatEdit
}

// RenamePromptMsg asks the app to prompt for a symbol's new name
// (lsp.rename, #6): the server validated the position, Placeholder prefills
// the input (the current symbol text, possibly empty), and Apply is the
// bridge-built continuation the app runs with the typed name — keeping the
// manager unreachable from the app, as with every other LSP action.
type RenamePromptMsg struct {
	Path        string
	Placeholder string
	Apply       func(newName string) tea.Cmd
}

// CodeActionChoice is one offered action, presentation-ready.
type CodeActionChoice struct {
	Title     string
	Kind      string
	Preferred bool
}

// CodeActionsMsg lists the actions available at the request position
// (lsp.codeAction, #8). Apply is the bridge-built continuation for the chosen
// index — like RenamePromptMsg it keeps the manager unreachable from the app.
type CodeActionsMsg struct {
	Path    string
	Actions []CodeActionChoice
	Apply   func(index int) tea.Cmd
}

// SignatureHelpMsg delivers call-signature info for the cursor-anchored popup
// (lsp signature help, #4). An empty Label dismisses the popup — the server
// answering null means the cursor left the call context.
type SignatureHelpMsg struct {
	Path       string
	Label      string
	ParamStart int // rune index into Label where the active parameter starts
	ParamEnd   int // rune index (exclusive) where it ends; == ParamStart when unknown
	Doc        string
	More       int // additional overloads beyond the active signature
}

// SignatureContent flattens a SignatureHelp into the popup fields: the active
// signature's label, the active parameter's rune highlight range (parameter
// labels arrive as substrings or UTF-16 offset pairs), the first line of its
// documentation, and how many other overloads exist.
func SignatureContent(sh *protocol.SignatureHelp) (label string, start, end int, doc string, more int) {
	if sh == nil || len(sh.Signatures) == 0 {
		return "", 0, 0, "", 0
	}
	active := sh.ActiveSignature
	if active < 0 || active >= len(sh.Signatures) {
		active = 0
	}
	sig := sh.Signatures[active]
	label = sig.Label
	more = len(sh.Signatures) - 1
	doc = docFirstLine(sig.Documentation)

	if p := sh.ActiveParameter; p >= 0 && p < len(sig.Parameters) {
		start, end = paramRange(label, sig.Parameters[p].Label)
	}
	return label, start, end, doc, more
}

// paramRange resolves a parameter label (substring or UTF-16 [start,end)
// pair) to rune offsets within the signature label.
func paramRange(label string, raw json.RawMessage) (int, int) {
	var pair [2]int
	if err := json.Unmarshal(raw, &pair); err == nil {
		return utf16OffToRune(label, pair[0]), utf16OffToRune(label, pair[1])
	}
	var sub string
	if err := json.Unmarshal(raw, &sub); err == nil && sub != "" {
		if i := strings.Index(label, sub); i >= 0 {
			start := len([]rune(label[:i]))
			return start, start + len([]rune(sub))
		}
	}
	return 0, 0
}

// utf16OffToRune converts a UTF-16 unit offset within s to a rune index.
func utf16OffToRune(s string, off int) int {
	units := 0
	for i, r := range []rune(s) {
		if units >= off {
			return i
		}
		units++
		if r > 0xFFFF {
			units++
		}
	}
	return len([]rune(s))
}

// docFirstLine flattens a Documentation value (string | MarkupContent) to its
// first non-empty line.
func docFirstLine(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	text := ""
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		text = s
	} else {
		var mc protocol.MarkupContent
		if err := json.Unmarshal(raw, &mc); err == nil {
			text = mc.Value
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// SemanticSpansMsg delivers the decoded semantic-token overlay for a document
// (#9). The editor layers it over the Tree-sitter base index; an empty slice
// clears the overlay.
type SemanticSpansMsg struct {
	Path  string
	Spans []highlight.Span
}

// ServerStatusKind classifies a server status update (Roadmap 0130):
// persistent server state belongs on the status line, transient events surface
// as toast notifications of the matching severity.
type ServerStatusKind int

const (
	// ServerState is persistent server state ("ready", "disabled"), rendered as
	// a status-line segment.
	ServerState ServerStatusKind = iota
	// ServerEventInfo is a transient event ("restarted"), raised as an info toast.
	ServerEventInfo
	// ServerEventWarn is a transient recoverable failure ("crashed", recovery
	// follows automatically), raised as a warn toast.
	ServerEventWarn
	// ServerEventError is a transient unrecoverable failure (launch error,
	// disabled after repeated crashes), raised as a persistent error toast.
	ServerEventError
)

// ServerStatusMsg reports server state (ready / crashed / disabled). Kind
// decides whether it lands on the status line or as a toast. Lang names the
// language the update belongs to ("" for subsystem-wide events), so the
// language-server settings page (#130) can track per-server state.
type ServerStatusMsg struct {
	Lang string
	Text string
	Kind ServerStatusKind
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
