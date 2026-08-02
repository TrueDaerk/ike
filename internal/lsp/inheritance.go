package lsp

// inheritance.go carries the editor-facing inheritance-mark types (#1450): the
// gutter's ↑/↓ decoration data. The manager computes marks from a document's
// symbols via textDocument/implementation; the bridge delivers them per file.

// InheritanceMark kinds: the arrow direction the gutter draws.
const (
	// InheritanceImplements marks a symbol that implements/overrides a super
	// declaration (↑).
	InheritanceImplements = 1
	// InheritanceImplemented marks a symbol that has implementations/overrides
	// below it (↓).
	InheritanceImplemented = 2
)

// InheritanceMark is one gutter mark in editor coordinates.
type InheritanceMark struct {
	Line int
	Kind int
}
