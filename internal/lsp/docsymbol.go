package lsp

import (
	"ike/internal/lsp/protocol"
)

// docsymbol.go carries the editor-facing document-symbol types (#1025): the
// Structure tool pane's data. The manager converts a server's documentSymbol
// reply into SymbolNode trees in editor (rune-column) coordinates; the bridge
// delivers them as one DocumentSymbolsMsg.

// SymbolNode is one symbol of a document in editor coordinates. Line/Col are
// the selection-range start (the name), the navigation target; EndLine is the
// full construct's last line, the enclosing-symbol test's lower bound.
type SymbolNode struct {
	Name     string
	Detail   string
	Kind     int // protocol symbol kind (1=file … 26=type parameter)
	Line     int
	Col      int
	EndLine  int
	Children []SymbolNode
}

// DocumentSymbolsMsg delivers a document's symbol tree to the Structure pane.
// NoProvider reports that no ready server advertises documentSymbolProvider
// for the path, so the pane can say so instead of staying empty.
type DocumentSymbolsMsg struct {
	Path       string
	Symbols    []SymbolNode
	NoProvider bool
}

// ConvertDocumentSymbols maps a documentSymbol reply (already normalised to
// the hierarchical shape by the client) into editor coordinates using the
// document's current lines and the negotiated encoding.
func ConvertDocumentSymbols(syms []protocol.DocumentSymbol, lines []string, enc string) []SymbolNode {
	if enc == "" {
		enc = protocol.EncodingUTF16
	}
	if len(syms) == 0 {
		return nil
	}
	out := make([]SymbolNode, 0, len(syms))
	for _, s := range syms {
		pos := protocol.FromLSPPosition(lines, s.SelectionRange.Start, enc)
		out = append(out, SymbolNode{
			Name:     s.Name,
			Detail:   s.Detail,
			Kind:     s.Kind,
			Line:     pos.Line,
			Col:      pos.Col,
			EndLine:  s.Range.End.Line,
			Children: ConvertDocumentSymbols(s.Children, lines, enc),
		})
	}
	return out
}
