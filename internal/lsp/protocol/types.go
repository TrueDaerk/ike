// Package protocol holds the Language Server Protocol data types the client and
// features exchange, plus the single position-encoding boundary (convert.go)
// between editor rune/byte coordinates and LSP's UTF-16 code-unit coordinates
// (Roadmap 0100). Only the subset the MVP needs is modelled; unknown fields on
// the wire are ignored by encoding/json, and capabilities gate everything else.
package protocol

import (
	"encoding/json"
	"strings"
)

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
	Workspace    *WorkspaceClientCaps       `json:"workspace,omitempty"`
}

// WorkspaceClientCaps announces workspace-level support. Configuration lets a
// server pull settings via workspace/configuration (pyright reads the Python
// interpreter path this way); DidChangeConfiguration lets it react to updates.
type WorkspaceClientCaps struct {
	Configuration          bool                        `json:"configuration,omitempty"`
	DidChangeConfiguration *DidChangeConfigurationCaps `json:"didChangeConfiguration,omitempty"`
	// DidChangeWatchedFiles with dynamicRegistration lets a server register
	// the file globs it wants workspace/didChangeWatchedFiles for (#1144):
	// without it Intelephense never re-indexes externally created files.
	DidChangeWatchedFiles *DidChangeWatchedFilesCaps `json:"didChangeWatchedFiles,omitempty"`
}

// DidChangeWatchedFilesCaps announces workspace/didChangeWatchedFiles support
// (#1144); DynamicRegistration invites client/registerCapability watch globs.
type DidChangeWatchedFilesCaps struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type DidChangeConfigurationCaps struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// GeneralClientCapabilities carries position-encoding preferences.
type GeneralClientCapabilities struct {
	PositionEncodings []string `json:"positionEncodings,omitempty"`
}

// TextDocumentClientCaps gates per-feature client support.
type TextDocumentClientCaps struct {
	Synchronization *SyncClientCaps           `json:"synchronization,omitempty"`
	Completion      *CompletionClientCaps     `json:"completion,omitempty"`
	Hover           *HoverClientCaps          `json:"hover,omitempty"`
	Definition      *LinkSupportCaps          `json:"definition,omitempty"`
	References      *ReferencesClientCaps     `json:"references,omitempty"`
	Formatting      *ReferencesClientCaps     `json:"formatting,omitempty"`
	RangeFormatting *ReferencesClientCaps     `json:"rangeFormatting,omitempty"`
	Rename          *RenameClientCaps         `json:"rename,omitempty"`
	CodeAction      *ReferencesClientCaps     `json:"codeAction,omitempty"`
	SignatureHelp   *ReferencesClientCaps     `json:"signatureHelp,omitempty"`
	SemanticTokens  *SemanticTokensClientCaps `json:"semanticTokens,omitempty"`
	CallHierarchy   *ReferencesClientCaps     `json:"callHierarchy,omitempty"`
	InlayHint       *ReferencesClientCaps     `json:"inlayHint,omitempty"`
	DocumentSymbol  *DocumentSymbolClientCaps `json:"documentSymbol,omitempty"`
	// PublishDiagnostics must be advertised for servers that gate their push
	// diagnostics on it (#1060): vtsls sends none at all without the entry.
	PublishDiagnostics *PublishDiagnosticsClientCaps `json:"publishDiagnostics,omitempty"`
}

// PublishDiagnosticsClientCaps announces textDocument/publishDiagnostics
// support (#1060). relatedInformation asks servers to include linked
// locations on diagnostics that carry them.
type PublishDiagnosticsClientCaps struct {
	RelatedInformation bool `json:"relatedInformation,omitempty"`
}

// DocumentSymbolClientCaps announces documentSymbol support (#1025);
// hierarchicalDocumentSymbolSupport asks for the DocumentSymbol[] tree shape
// instead of the flat SymbolInformation[] fallback.
type DocumentSymbolClientCaps struct {
	HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport,omitempty"`
}

// SemanticTokensClientCaps announces semantic-token support: which request
// forms the client issues and which token types/modifiers it understands.
type SemanticTokensClientCaps struct {
	Requests       SemanticTokensRequests `json:"requests"`
	TokenTypes     []string               `json:"tokenTypes"`
	TokenModifiers []string               `json:"tokenModifiers"`
	Formats        []string               `json:"formats"`
}

type SemanticTokensRequests struct {
	Full *SemanticTokensFullRequest `json:"full,omitempty"`
}

type SemanticTokensFullRequest struct {
	Delta bool `json:"delta,omitempty"`
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

	DocumentHighlightProvider json.RawMessage `json:"documentHighlightProvider,omitempty"`
	InlayHintProvider         json.RawMessage `json:"inlayHintProvider,omitempty"`

	DocumentFormattingProvider      json.RawMessage        `json:"documentFormattingProvider,omitempty"`
	DocumentRangeFormattingProvider json.RawMessage        `json:"documentRangeFormattingProvider,omitempty"`
	RenameProvider                  json.RawMessage        `json:"renameProvider,omitempty"`
	CodeActionProvider              json.RawMessage        `json:"codeActionProvider,omitempty"`
	SignatureHelpProvider           *SignatureHelpOptions  `json:"signatureHelpProvider,omitempty"`
	SemanticTokensProvider          *SemanticTokensOptions `json:"semanticTokensProvider,omitempty"`
	ExecuteCommandProvider          json.RawMessage        `json:"executeCommandProvider,omitempty"`
	WorkspaceSymbolProvider         json.RawMessage        `json:"workspaceSymbolProvider,omitempty"`
	CallHierarchyProvider           json.RawMessage        `json:"callHierarchyProvider,omitempty"`
	DocumentSymbolProvider          json.RawMessage        `json:"documentSymbolProvider,omitempty"`
}

// DocumentSymbolParams is the textDocument/documentSymbol request (#1025):
// every symbol of one document, for the Structure tool pane.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DocumentSymbol is one node of the hierarchical documentSymbol reply. Range
// spans the whole construct, SelectionRange just the name — navigation targets
// the latter. Servers may answer with flat SymbolInformation[] instead; the
// client normalises that shape into this one (childless, both ranges equal).
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// WorkspaceSymbolParams is the workspace/symbol request (0250, #294): a plain
// query the server matches against every symbol it knows.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// SymbolInformation is one workspace/symbol hit. Servers may reply with the
// newer WorkspaceSymbol shape whose location can lack a range; the client
// normalises both into this classic form.
type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
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
	Label            string    `json:"label"`
	Kind             int       `json:"kind,omitempty"`
	Detail           string    `json:"detail,omitempty"`
	Documentation    any       `json:"documentation,omitempty"`
	InsertText       string    `json:"insertText,omitempty"`
	TextEdit         *TextEdit `json:"textEdit,omitempty"`
	SortText            string     `json:"sortText,omitempty"`
	FilterText          string     `json:"filterText,omitempty"`
	InsertTextFormat    int        `json:"insertTextFormat,omitempty"`
	AdditionalTextEdits []TextEdit `json:"additionalTextEdits,omitempty"`
}

// InsertTextFormat values (LSP): 1 = plain text, 2 = snippet syntax.
const (
	InsertPlainText = 1
	InsertSnippet   = 2
)

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
// shape the server chose. Per the spec the two fields are alternative
// encodings: when documentChanges is present it wins and changes is ignored —
// some servers (pylsp) send the same edits in both, and merging them applied
// every rename twice (#364).
func (w WorkspaceEdit) AllChanges() map[string][]TextEdit {
	out := map[string][]TextEdit{}
	for _, dc := range w.DocumentChanges {
		if dc.TextDocument.URI == "" || len(dc.Edits) == 0 {
			continue
		}
		out[dc.TextDocument.URI] = append(out[dc.TextDocument.URI], dc.Edits...)
	}
	if len(out) > 0 {
		return out
	}
	for uri, edits := range w.Changes {
		out[uri] = append(out[uri], edits...)
	}
	return out
}

// --- semantic tokens ---

// SemanticTokensOptions is the server capability: the legend the packed data
// decodes against, and whether full (and delta) requests are served. Full may
// be a bool or {delta: true} — decoded leniently.
type SemanticTokensOptions struct {
	Legend SemanticTokensLegend `json:"legend"`
	Full   json.RawMessage      `json:"full,omitempty"`
}

type SemanticTokensLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

type SemanticTokensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type SemanticTokensDeltaParams struct {
	TextDocument     TextDocumentIdentifier `json:"textDocument"`
	PreviousResultID string                 `json:"previousResultId"`
}

// SemanticTokens is the full result: packed 5-tuples relative-encoded.
type SemanticTokens struct {
	ResultID string   `json:"resultId,omitempty"`
	Data     []uint32 `json:"data"`
}

// SemanticTokensDelta carries edits against a previous result.
type SemanticTokensDelta struct {
	ResultID string               `json:"resultId,omitempty"`
	Edits    []SemanticTokensEdit `json:"edits"`
}

type SemanticTokensEdit struct {
	Start       int      `json:"start"`
	DeleteCount int      `json:"deleteCount"`
	Data        []uint32 `json:"data,omitempty"`
}

// --- signature help ---

// SignatureHelpOptions carries the trigger characters a server wants
// signature requests on (plus retrigger characters while help is showing).
type SignatureHelpOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	RetriggerCharacters []string `json:"retriggerCharacters,omitempty"`
}

type SignatureHelpParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// SignatureHelp is the server's answer: the overloads plus which signature
// and parameter are active at the cursor.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature,omitempty"`
	ActiveParameter int                    `json:"activeParameter,omitempty"`
}

type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation json.RawMessage        `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

// ParameterInformation's label is a substring of the signature label, or a
// [start, end) offset pair in UTF-16 units — decoded leniently by consumers.
type ParameterInformation struct {
	Label         json.RawMessage `json:"label"`
	Documentation json.RawMessage `json:"documentation,omitempty"`
}

// --- code actions ---

type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      CodeActionContext      `json:"context"`
}

// CodeActionContext carries the client-known diagnostics overlapping the
// range, so servers offer the matching quick-fixes. Only, when set, asks the
// server for actions of the listed kinds exclusively (#1148: the save chain
// requests just source.organizeImports).
type CodeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Only        []string     `json:"only,omitempty"`
}

// KindSourceOrganizeImports is the code-action kind the organize-imports save
// step requests (#1148).
const KindSourceOrganizeImports = "source.organizeImports"

// CodeAction is one offered fix/refactor: Edit, Command, or both (Edit is
// applied first per the spec).
type CodeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind,omitempty"`
	IsPreferred bool           `json:"isPreferred,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
	Command     *Command       `json:"command,omitempty"`
}

// Command is a server-defined command reference (the executeCommand shape).
type Command struct {
	Title     string            `json:"title"`
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

type ExecuteCommandParams struct {
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

// ApplyWorkspaceEditParams is the server→client workspace/applyEdit request.
type ApplyWorkspaceEditParams struct {
	Label string        `json:"label,omitempty"`
	Edit  WorkspaceEdit `json:"edit"`
}

// ApplyWorkspaceEditResult answers it.
type ApplyWorkspaceEditResult struct {
	Applied bool `json:"applied"`
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

// --- document highlight ---

// DocumentHighlightParams is the textDocument/documentHighlight request
// (#172): the occurrences of the symbol at a position within its document.
type DocumentHighlightParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// DocumentHighlightKind values (LSP): a plain textual occurrence, a read
// access, a write access.
const (
	HighlightText  = 1
	HighlightRead  = 2
	HighlightWrite = 3
)

// DocumentHighlight is one occurrence of the symbol under the cursor. Kind is
// optional; absent means HighlightText.
type DocumentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind,omitempty"`
}

// --- inlay hints ---

// InlayHintParams is the textDocument/inlayHint request (#171): the inline
// parameter-name / inferred-type hints within a document range.
type InlayHintParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

// InlayHintKind values (LSP): a type annotation, a parameter name.
const (
	InlayHintType      = 1
	InlayHintParameter = 2
)

// InlayHint is one inline hint anchored at a position. Kind is optional;
// absent means an unclassified hint. PaddingLeft/Right ask the client to
// separate the hint from the code with a space on that side.
type InlayHint struct {
	Position     Position       `json:"position"`
	Label        InlayHintLabel `json:"label"`
	Kind         int            `json:"kind,omitempty"`
	PaddingLeft  bool           `json:"paddingLeft,omitempty"`
	PaddingRight bool           `json:"paddingRight,omitempty"`
}

// InlayHintLabel is the hint text: the wire value is either a plain string or
// an array of label parts, flattened to one string on decode (the parts'
// tooltip/location extras are presentation we do not render).
type InlayHintLabel string

// UnmarshalJSON accepts both label shapes.
func (l *InlayHintLabel) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*l = InlayHintLabel(s)
		return nil
	}
	var parts []struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Value)
	}
	*l = InlayHintLabel(b.String())
	return nil
}

// --- call hierarchy ---

// CallHierarchyPrepareParams is the textDocument/prepareCallHierarchy request
// (#173): resolve the symbol at a position into hierarchy items.
type CallHierarchyPrepareParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// CallHierarchyItem is one node of a call hierarchy: a callable symbol with
// its declaration range and the selection range naming it. Data is the
// server's opaque resolve token and must round-trip verbatim into the
// incoming/outgoing follow-up requests.
type CallHierarchyItem struct {
	Name           string          `json:"name"`
	Kind           int             `json:"kind"`
	Tags           []int           `json:"tags,omitempty"`
	Detail         string          `json:"detail,omitempty"`
	URI            string          `json:"uri"`
	Range          Range           `json:"range"`
	SelectionRange Range           `json:"selectionRange"`
	Data           json.RawMessage `json:"data,omitempty"`
}

// CallHierarchyCallsParams is the shared parameter shape of the
// callHierarchy/incomingCalls and callHierarchy/outgoingCalls requests: the
// prepared item whose callers/callees are wanted.
type CallHierarchyCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyIncomingCall is one caller of an item; FromRanges are the call
// sites inside From's document.
type CallHierarchyIncomingCall struct {
	From       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

// CallHierarchyOutgoingCall is one callee of an item; FromRanges are the call
// sites inside the *queried* item's document.
type CallHierarchyOutgoingCall struct {
	To         CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
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

// --- watched files (#1144) ---

// FileChangeType values classify one workspace/didChangeWatchedFiles event.
const (
	// FileChangeCreated announces a newly created file.
	FileChangeCreated = 1
	// FileChangeChanged announces a content change of an existing file.
	FileChangeChanged = 2
	// FileChangeDeleted announces a deleted file.
	FileChangeDeleted = 3
)

// FileEvent is one file creation/change/deletion; Type is a FileChange* value.
type FileEvent struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

// DidChangeWatchedFilesParams carries the batched file events of one
// workspace/didChangeWatchedFiles notification.
type DidChangeWatchedFilesParams struct {
	Changes []FileEvent `json:"changes"`
}

// WatchKind bit flags of FileSystemWatcher.Kind: which event types a server
// wants for a glob. An absent Kind means all three (7).
const (
	WatchCreate = 1
	WatchChange = 2
	WatchDelete = 4
	// WatchAll is the spec default when a watcher carries no kind.
	WatchAll = WatchCreate | WatchChange | WatchDelete
)

// GlobPattern is the `string | RelativePattern` union of a watcher's glob: a
// bare pattern (relative to the workspace folder, or absolute), or a pattern
// resolved against an explicit base URI/folder.
type GlobPattern struct {
	// Pattern is the glob itself.
	Pattern string
	// BaseURI is the resolved base of a RelativePattern ("" for bare strings);
	// the wire shape allows a plain URI string or a WorkspaceFolder object.
	BaseURI string
}

// UnmarshalJSON accepts `"glob"` or `{"baseUri": string | {"uri": ...}, "pattern": ...}`.
func (g *GlobPattern) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		g.Pattern, g.BaseURI = s, ""
		return nil
	}
	var rel struct {
		BaseURI json.RawMessage `json:"baseUri"`
		Pattern string          `json:"pattern"`
	}
	if err := json.Unmarshal(raw, &rel); err != nil {
		return err
	}
	g.Pattern = rel.Pattern
	var base string
	if json.Unmarshal(rel.BaseURI, &base) != nil {
		var folder WorkspaceFolder
		if json.Unmarshal(rel.BaseURI, &folder) == nil {
			base = folder.URI
		}
	}
	g.BaseURI = base
	return nil
}

// MarshalJSON writes the bare-string form when no base is set, the
// RelativePattern object otherwise (round-trip support for tests).
func (g GlobPattern) MarshalJSON() ([]byte, error) {
	if g.BaseURI == "" {
		return json.Marshal(g.Pattern)
	}
	return json.Marshal(struct {
		BaseURI string `json:"baseUri"`
		Pattern string `json:"pattern"`
	}{g.BaseURI, g.Pattern})
}

// FileSystemWatcher is one glob a server registered interest in; Kind is a
// Watch* bit set (nil means WatchAll).
type FileSystemWatcher struct {
	GlobPattern GlobPattern `json:"globPattern"`
	Kind        *int        `json:"kind,omitempty"`
}

// DidChangeWatchedFilesRegistrationOptions is the registerOptions payload of a
// workspace/didChangeWatchedFiles registration.
type DidChangeWatchedFilesRegistrationOptions struct {
	Watchers []FileSystemWatcher `json:"watchers"`
}

// Registration is one dynamic capability registration (client/registerCapability).
type Registration struct {
	ID              string          `json:"id"`
	Method          string          `json:"method"`
	RegisterOptions json.RawMessage `json:"registerOptions,omitempty"`
}

// RegistrationParams carries a client/registerCapability request's payload.
type RegistrationParams struct {
	Registrations []Registration `json:"registrations"`
}

// Unregistration is one dynamic capability withdrawal (client/unregisterCapability).
type Unregistration struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

// UnregistrationParams carries a client/unregisterCapability request's payload.
// The wire property is "unregisterations" — a spec typo kept for compatibility.
type UnregistrationParams struct {
	Unregisterations []Unregistration `json:"unregisterations"`
}
