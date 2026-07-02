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

// Position encodings a client may negotiate.
const (
	EncodingUTF16 = "utf-16"
	EncodingUTF8  = "utf-8"
	EncodingUTF32 = "utf-32"
)
