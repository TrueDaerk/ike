package highlight

// tree.go is the pure-Go syntax-tree snapshot (#2667): a structural
// extractor (the PHP declaration index, internal/phpindex) needs the whole
// tree of a file — kinds, field names, child order and positions — not the
// highlight captures, and it must not depend on cgo itself. SyntaxTree
// (parse_cgo.go) parses once and copies the named nodes into SyntaxNode
// values with editor rune columns; the stub build returns nil, so a caller
// degrades to "nothing known" instead of failing to compile.

// SyntaxNode is one named node of a parsed tree. Positions are 0-based
// line / rune-column pairs with an exclusive end, the editor's own
// coordinates; StartByte/EndByte index the joined source
// (strings.Join(lines, "\n")) for Text. Children holds the named children
// in source order; anonymous tokens (keywords, punctuation) are not kept —
// a caller that needs them reads the parent's Text — except the operator
// token, which Operator carries (#2914).
type SyntaxNode struct {
	Kind      string
	Field     string // field name under the parent, "" when the grammar gives none
	StartByte int
	EndByte   int
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	// Operator is the node's operator token (#2914): the text of the
	// grammar's `operator` field when the node has one (`&&` on a Go
	// binary_expression, `and` on a Python boolean_operator), else the first
	// anonymous token sitting between two named children — the infix of a
	// field-less list such as shell's `a && b` — and "" for a node with
	// neither (a leading keyword or a closing bracket never counts). It is
	// the one anonymous token the snapshot keeps, so a consumer can tell
	// `a && b` from `a + b` without re-reading the source.
	Operator string
	Children []*SyntaxNode
}

// Text returns the node's source slice out of src, the joined lines the
// tree was parsed from; a node outside src (never for a tree SyntaxTree
// built) yields "".
func (n *SyntaxNode) Text(src string) string {
	if n == nil || n.StartByte < 0 || n.EndByte > len(src) || n.StartByte > n.EndByte {
		return ""
	}
	return src[n.StartByte:n.EndByte]
}

// Child returns the first child carrying field, or nil.
func (n *SyntaxNode) Child(field string) *SyntaxNode {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Field == field {
			return c
		}
	}
	return nil
}

// ChildOfKind returns the first child of the given kind, or nil.
func (n *SyntaxNode) ChildOfKind(kind string) *SyntaxNode {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Kind == kind {
			return c
		}
	}
	return nil
}

// ChildrenOfKind returns every child of the given kind, in order.
func (n *SyntaxNode) ChildrenOfKind(kind string) []*SyntaxNode {
	if n == nil {
		return nil
	}
	var out []*SyntaxNode
	for _, c := range n.Children {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// Contains reports whether the 0-based line / rune column lies inside the
// node's extent (start inclusive, end exclusive).
func (n *SyntaxNode) Contains(line, col int) bool {
	if n == nil {
		return false
	}
	if line < n.StartLine || line > n.EndLine {
		return false
	}
	if line == n.StartLine && col < n.StartCol {
		return false
	}
	if line == n.EndLine && col >= n.EndCol && !(n.StartLine == n.EndLine && n.StartCol == n.EndCol) {
		return false
	}
	return true
}
