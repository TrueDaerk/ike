//go:build cgo

package highlight

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/lang"
)

// SyntaxTree parses lines with the registered grammar of language langID
// and returns the root of a pure-Go snapshot of the tree (#2667): every
// named node with its kind, field name, byte extent and editor rune-column
// range, children in source order. The parser and the Tree-sitter tree are
// closed before returning, so the snapshot is safe to keep and to read from
// any goroutine. nil when the language has no grammar. It is a full parse of
// the text: callers run it off the Update goroutine (a scan or worker).
func SyntaxTree(langID string, lines []string) *SyntaxNode {
	l, ok := lang.ByID(langID)
	if !ok || l.Grammar == nil {
		return nil
	}
	gi, ok := l.Grammar.(*grammarImpl)
	if !ok {
		return nil
	}
	src := []byte(strings.Join(lines, "\n"))
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(gi.lang); err != nil {
		return nil
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()
	conv := newColMapper(lines)
	return snapshotTree(tree.RootNode(), "", src, conv)
}

// snapshotTree copies n and its named descendants. The walk uses a cursor
// per level (Child(i) is O(1) on the C side; the cursor keeps field names).
// The operator token (#2914) is the one anonymous child kept: the text of
// the grammar's `operator` field, else the first anonymous token sitting
// between two named children (the infix of a field-less list such as
// shell's `a && b`); leading and trailing tokens (keywords, brackets) are
// never taken.
func snapshotTree(n *ts.Node, field string, src []byte, conv colMapper) *SyntaxNode {
	start, end := n.StartPosition(), n.EndPosition()
	out := &SyntaxNode{
		Kind:      n.Kind(),
		Field:     field,
		StartByte: int(n.StartByte()),
		EndByte:   int(n.EndByte()),
		StartLine: int(start.Row),
		StartCol:  conv.runeCol(int(start.Row), int(start.Column)),
		EndLine:   int(end.Row),
		EndCol:    conv.runeCol(int(end.Row), int(end.Column)),
	}
	if op := n.ChildByFieldName("operator"); op != nil {
		out.Operator = string(src[op.StartByte():op.EndByte()])
	}
	count := n.ChildCount()
	lastNamed := -1
	if out.Operator == "" {
		for i := int(count) - 1; i >= 0; i-- {
			if c := n.Child(uint(i)); c != nil && c.IsNamed() {
				lastNamed = i
				break
			}
		}
	}
	for i := uint(0); i < count; i++ {
		c := n.Child(i)
		if c == nil {
			continue
		}
		if !c.IsNamed() {
			if out.Operator == "" && len(out.Children) > 0 && int(i) < lastNamed {
				out.Operator = string(src[c.StartByte():c.EndByte()])
			}
			continue
		}
		out.Children = append(out.Children, snapshotTree(c, n.FieldNameForChild(uint32(i)), src, conv))
	}
	return out
}
