package protocol

// enums.go gathers the LSP enumerations the MVP references. Values are the
// protocol's wire numbers; they are plain ints so unknown future values pass
// through without a decode error.

// DiagnosticSeverity values.
const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

// TextDocumentSyncKind: how a server wants document changes delivered.
const (
	SyncNone        = 0
	SyncFull        = 1
	SyncIncremental = 2
)

// CompletionTriggerKind: why completion fired.
const (
	CompletionTriggerInvoked          = 1
	CompletionTriggerCharacter        = 2
	CompletionTriggerIncompleteReopen = 3
)

// CompletionItemKind: the item's symbol kind (for popup glyphs/labels).
const (
	KindText          = 1
	KindMethod        = 2
	KindFunction      = 3
	KindConstructor   = 4
	KindField         = 5
	KindVariable      = 6
	KindClass         = 7
	KindInterface     = 8
	KindModule        = 9
	KindProperty      = 10
	KindUnit          = 11
	KindValue         = 12
	KindEnum          = 13
	KindKeyword       = 14
	KindSnippet       = 15
	KindColor         = 16
	KindFile          = 17
	KindReference     = 18
	KindFolder        = 19
	KindEnumMember    = 20
	KindConstant      = 21
	KindStruct        = 22
	KindEvent         = 23
	KindOperator      = 24
	KindTypeParameter = 25
)

// SymbolKind: the kind of a DocumentSymbol / SymbolInformation node. This is a
// DIFFERENT numbering than CompletionItemKind above (SymbolKind Class is 5,
// CompletionItemKind Class is 7) — never mix the two blocks. The inheritance
// features filter on a few of these (#1449); the palette's class category
// (#1849) labels every kind, so the full spec set is modelled.
const (
	SymKindFile          = 1
	SymKindModule        = 2
	SymKindNamespace     = 3
	SymKindPackage       = 4
	SymKindClass         = 5
	SymKindMethod        = 6
	SymKindProperty      = 7
	SymKindField         = 8
	SymKindConstructor   = 9
	SymKindEnum          = 10
	SymKindInterface     = 11
	SymKindFunction      = 12
	SymKindVariable      = 13
	SymKindConstant      = 14
	SymKindString        = 15
	SymKindNumber        = 16
	SymKindBoolean       = 17
	SymKindArray         = 18
	SymKindObject        = 19
	SymKindKey           = 20
	SymKindNull          = 21
	SymKindEnumMember    = 22
	SymKindStruct        = 23
	SymKindEvent         = 24
	SymKindOperator      = 25
	SymKindTypeParameter = 26
)

// Position encodings a client may negotiate.
const (
	EncodingUTF16 = "utf-16"
	EncodingUTF8  = "utf-8"
	EncodingUTF32 = "utf-32"
)

// StandardTokenTypes / StandardTokenModifiers are the LSP-predefined semantic
// token sets the client announces support for (3.17).
var StandardTokenTypes = []string{
	"namespace", "type", "class", "enum", "interface", "struct", "typeParameter",
	"parameter", "variable", "property", "enumMember", "event", "function",
	"method", "macro", "keyword", "modifier", "comment", "string", "number",
	"regexp", "operator", "decorator",
}

var StandardTokenModifiers = []string{
	"declaration", "definition", "readonly", "static", "deprecated", "abstract",
	"async", "modification", "documentation", "defaultLibrary",
}
