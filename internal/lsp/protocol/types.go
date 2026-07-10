// Package protocol holds the Language Server Protocol data types the client and
// features exchange, plus the single position-encoding boundary (convert.go)
// between editor rune/byte coordinates and LSP's UTF-16 code-unit coordinates
// (Roadmap 0100). Only the subset the MVP needs is modelled; unknown fields on
// the wire are ignored by encoding/json, and capabilities gate everything else.
package protocol

import "encoding/json"

// Position is a zero-based line and character offset. Character is measured in
// the negotiated position encoding (UTF-16 by default) — never assume runes.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open span [Start, End).
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range within a document URI.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier names a document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier names a document plus its version.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentItem is a freshly opened document's full content.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// --- lifecycle ---

// InitializeParams is sent once per server to negotiate capabilities.
type InitializeParams struct {
	ProcessID             int                `json:"processId"`
	RootURI               string             `json:"rootUri"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	InitializationOptions json.RawMessage    `json:"initializationOptions,omitempty"`
	WorkspaceFolders      []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
}

// WorkspaceFolder is a named workspace root.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// ClientCapabilities advertises what the editor supports. The MVP keeps this
// small; general.positionEncodings lets a server pick UTF-8 when it can.
type ClientCapabilities struct {
	General      *GeneralClientCapabilities `json:"general,omitempty"`
	TextDocument *TextDocumentClientCaps    `json:"textDocument,omitempty"`
}

// GeneralClientCapabilities carries position-encoding preferences.
type GeneralClientCapabilities struct {
	PositionEncodings []string `json:"positionEncodings,omitempty"`
}

// TextDocumentClientCaps gates per-feature client support.
type TextDocumentClientCaps struct {
	Synchronization *SyncClientCaps       `json:"synchronization,omitempty"`
	Completion      *CompletionClientCaps `json:"completion,omitempty"`
	Hover           *HoverClientCaps      `json:"hover,omitempty"`
	Definition      *LinkSupportCaps      `json:"definition,omitempty"`
	References      *ReferencesClientCaps `json:"references,omitempty"`
	Formatting      *ReferencesClientCaps `json:"formatting,omitempty"`
	RangeFormatting *ReferencesClientCaps `json:"rangeFormatting,omitempty"`
	Rename          *RenameClientCaps     `json:"rename,omitempty"`
}

// RenameClientCaps announces rename support; prepareSupport asks servers to
// offer textDocument/prepareRename validation.
type RenameClientCaps struct {
	PrepareSupport bool `json:"prepareSupport,omitempty"`
}

type SyncClientCaps struct {
	DidSave bool `json:"didSave,omitempty"`
}
type CompletionClientCaps struct {
	CompletionItem *CompletionItemCaps `json:"completionItem,omitempty"`
}
type CompletionItemCaps struct {
	SnippetSupport bool `json:"snippetSupport,omitempty"`
}
type HoverClientCaps struct {
	ContentFormat []string `json:"contentFormat,omitempty"`
}
type LinkSupportCaps struct {
	LinkSupport bool `json:"linkSupport,omitempty"`
}

// ReferencesClientCaps announces plain request support (references,
// formatting, range formatting); the empty object is the whole announcement —
// no options are gated client-side.
type ReferencesClientCaps struct{}

// InitializeResult carries the server's negotiated capabilities.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerCapabilities reports what the server offers. TextDocumentSync may be a
// number (kind) or an object, so it is decoded leniently in capabilities.go.
type ServerCapabilities struct {
	PositionEncoding   string             `json:"positionEncoding,omitempty"`
	TextDocumentSync   json.RawMessage    `json:"textDocumentSync,omitempty"`
	CompletionProvider *CompletionOptions `json:"completionProvider,omitempty"`
	HoverProvider      json.RawMessage    `json:"hoverProvider,omitempty"`
	DefinitionProvider json.RawMessage    `json:"definitionProvider,omitempty"`
	ReferencesProvider json.RawMessage    `json:"referencesProvider,omitempty"`

	DocumentFormattingProvider      json.RawMessage `json:"documentFormattingProvider,omitempty"`
	DocumentRangeFormattingProvider json.RawMessage `json:"documentRangeFormattingProvider,omitempty"`
	RenameProvider                  json.RawMessage `json:"renameProvider,omitempty"`
}

// CompletionOptions describes completion support, notably trigger characters.
type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	ResolveProvider   bool     `json:"resolveProvider,omitempty"`
}

// --- sync ---

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}
type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         string                 `json:"text,omitempty"`
}
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// TextDocumentContentChangeEvent is one edit: incremental when Range is set,
// full-document replace when Range is nil (Text is the whole document).
type TextDocumentContentChangeEvent struct {
	Range *Range `json:"range,omitempty"`
	Text  string `json:"text"`
}

// --- diagnostics ---

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int          `json:"version"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Code     any    `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// --- completion ---

type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      *CompletionContext     `json:"context,omitempty"`
}

type CompletionContext struct {
	TriggerKind      int    `json:"triggerKind"`
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
}

// CompletionList wraps items; a server may also return a bare item array, which
// completion.go normalises.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type CompletionItem struct {
	Label         string    `json:"label"`
	Kind          int       `json:"kind,omitempty"`
	Detail        string    `json:"detail,omitempty"`
	Documentation any       `json:"documentation,omitempty"`
	InsertText    string    `json:"insertText,omitempty"`
	TextEdit      *TextEdit `json:"textEdit,omitempty"`
	SortText      string    `json:"sortText,omitempty"`
	FilterText    string    `json:"filterText,omitempty"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// --- rename ---

type PrepareRenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

// WorkspaceEdit carries rename (and code-action) rewrites. Servers send either
// the flat changes map or documentChanges; both decode.
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []TextDocumentEdit    `json:"documentChanges,omitempty"`
}

// TextDocumentEdit is one document's edits inside documentChanges. Entries
// that are not text edits (file operations) fail to decode into this shape
// and are skipped by consumers (TextDocument.URI empty).
type TextDocumentEdit struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Edits []TextEdit `json:"edits"`
}

// AllChanges flattens a WorkspaceEdit into one uri -> edits map, whichever
// shape the server chose.
func (w WorkspaceEdit) AllChanges() map[string][]TextEdit {
	out := map[string][]TextEdit{}
	for uri, edits := range w.Changes {
		out[uri] = append(out[uri], edits...)
	}
	for _, dc := range w.DocumentChanges {
		if dc.TextDocument.URI == "" || len(dc.Edits) == 0 {
			continue
		}
		out[dc.TextDocument.URI] = append(out[dc.TextDocument.URI], dc.Edits...)
	}
	return out
}

// --- hover ---

type HoverParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// Hover.Contents may be a MarkupContent object, a string, or an array; hover.go
// flattens it.
type Hover struct {
	Contents json.RawMessage `json:"contents"`
	Range    *Range          `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// --- definition ---

type DefinitionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// --- references ---

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

// ReferenceContext carries the one request option references defines.
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// --- formatting ---

// FormattingOptions carries the editor's indent settings into a formatting
// request; servers honour tabSize/insertSpaces, the rest is optional.
type FormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

type DocumentRangeFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Options      FormattingOptions      `json:"options"`
}
