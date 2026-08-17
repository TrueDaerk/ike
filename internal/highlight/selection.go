package highlight

// selection.go holds the pure-Go side of the Tree-sitter extend-selection
// fallback (#1912): the range model SelectionRangesAt (parse_cgo.go) fills
// when no LSP server answers textDocument/selectionRange. Like the span and
// fold models it compiles with or without cgo; the parser lives behind the
// build tag.

// NodeRange is one syntax-node extent in editor rune coordinates: the span
// [Start, End) with End exclusive, matching the editor's buffer.Range
// convention. The highlight package deliberately does not depend on the
// editor's buffer package, so the editor converts these itself.
type NodeRange struct {
	StartLine, StartCol int
	EndLine, EndCol     int
}
