//go:build cgo

package highlight

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/lang"
	"ike/internal/structval"
)

// parse runs the grammar's Tree-sitter language + query over the joined lines and
// returns highlight spans in editor rune coordinates. It runs off the Update
// goroutine (inside a tea.Cmd), so the CGo work never blocks the event loop. g is
// the opaque token built by NewGrammar; a non-*grammarImpl or an uncompilable
// query yields no spans.
func parse(g lang.Grammar, lines []string) []Span {
	spans, _, _ := parseScoped(g, nil, nil, lines)
	return spans
}

// parseScoped is parse plus sticky-scroll scope collection (#168) and fold-range
// collection (#144): one Tree-sitter parse yields the highlight spans and, when
// scopeKinds / foldKinds are non-empty, the multi-line nodes of those kinds as
// Scopes / Folds in pre-order (outer before inner). Sharing the parse keeps both
// features free — no second CGo pass per edit.
func parseScoped(g lang.Grammar, scopeKinds, foldKinds []string, lines []string) ([]Span, []Scope, []Fold) {
	gi, ok := g.(*grammarImpl)
	if !ok {
		return nil, nil, nil
	}
	tsLang, query, ok := gi.compiled()
	if !ok {
		return nil, nil, nil
	}

	src := []byte(strings.Join(lines, "\n"))
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tsLang); err != nil {
		return nil, nil, nil
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, nil, nil
	}
	defer tree.Close()

	var scopes []Scope
	if len(scopeKinds) > 0 {
		kinds := make(map[string]bool, len(scopeKinds))
		for _, k := range scopeKinds {
			kinds[k] = true
		}
		collectScopes(tree.RootNode(), kinds, &scopes)
	}
	var folds []Fold
	if len(foldKinds) > 0 {
		kinds := make(map[string]bool, len(foldKinds))
		for _, k := range foldKinds {
			kinds[k] = true
		}
		collectFolds(tree.RootNode(), kinds, &folds)
	}

	// byteToRune[line] maps a byte offset within that line to a rune column.
	conv := newColMapper(lines)

	cursor := ts.NewQueryCursor()
	defer cursor.Close()
	names := query.CaptureNames()

	var spans []Span
	captures := cursor.Captures(query, tree.RootNode(), src)
	for {
		match, idx := captures.Next()
		if match == nil {
			break
		}
		cap := match.Captures[idx]
		name := names[cap.Index]
		start := cap.Node.StartPosition()
		end := cap.Node.EndPosition()
		appendSpans(&spans, conv, name, start, end)
	}
	// Rainbow brackets (#789) used to be a second walk over this tree. They
	// are computed from the spans instead now (#1628): brackets.go pairs them
	// in pure Go, using the string/comment captures above as its mask, so
	// bracket depth, indent-guide depth and unmatched-bracket detection all
	// come from one tracker.
	return spans, scopes, folds
}

// collectScopes walks the tree depth-first and appends every multi-line node
// whose kind is in kinds — pre-order, so outer scopes precede the scopes they
// contain, which is the order EnclosingScopes relies on. Single-line nodes are
// skipped: a header with no body below it can never be scrolled into.
func collectScopes(n *ts.Node, kinds map[string]bool, out *[]Scope) {
	start, end := n.StartPosition(), n.EndPosition()
	if kinds[n.Kind()] && end.Row > start.Row {
		*out = append(*out, Scope{HeaderLine: int(start.Row), EndLine: int(end.Row)})
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		collectScopes(n.NamedChild(i), kinds, out)
	}
}

// collectFolds walks the tree depth-first and appends every multi-line node
// whose kind is in kinds as a foldable region (#144) — pre-order, so outer
// folds precede the folds they contain, which is the order InnermostFold
// relies on. Single-line nodes are skipped (nothing to hide), and nodes
// starting on the header line of the previous fold are collapsed into it
// (e.g. a Go type_declaration and its type_spec fold as one region).
func collectFolds(n *ts.Node, kinds map[string]bool, out *[]Fold) {
	start, end := n.StartPosition(), n.EndPosition()
	endRow := int(end.Row)
	// Tree-sitter's end position is exclusive: a node ending at column 0 stops
	// *before* that row, so its last content line is the one above. Delimited
	// nodes hit this (a .http "section" ends where the next ### begins, #1329);
	// brace-closed bodies never do, since their closer occupies a column.
	if end.Column == 0 && endRow > int(start.Row) {
		endRow--
	}
	if kinds[n.Kind()] && endRow > int(start.Row) {
		if l := len(*out); l == 0 || (*out)[l-1].HeaderLine != int(start.Row) {
			*out = append(*out, Fold{HeaderLine: int(start.Row), EndLine: endRow})
		}
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		collectFolds(n.NamedChild(i), kinds, out)
	}
}

// appendSpans turns a (possibly multi-line) captured node into one Span per
// line, converting Tree-sitter byte columns to editor rune columns.
func appendSpans(out *[]Span, conv colMapper, capture string, start, end ts.Point) {
	for line := int(start.Row); line <= int(end.Row); line++ {
		sByte := 0
		if line == int(start.Row) {
			sByte = int(start.Column)
		}
		eByte := conv.lineBytes(line)
		if line == int(end.Row) {
			eByte = int(end.Column)
		}
		sCol := conv.runeCol(line, sByte)
		eCol := conv.runeCol(line, eByte)
		if eCol > sCol {
			*out = append(*out, Span{Line: line, StartCol: sCol, EndCol: eCol, Capture: capture})
		}
	}
}

// SelectionRangesAt returns the syntactic selection ladder at (line, col) —
// both editor rune coordinates — for the Tree-sitter extend-selection fallback
// (#1912): the extents of the smallest node containing the position and of
// every ancestor up to the root, innermost first, zero-width and duplicate
// extents dropped. nil when the path has no grammar or the position is outside
// the parsed text. Like parseScoped it parses fresh from a line snapshot and
// closes parser and tree before returning; it runs inside a tea.Cmd, never on
// the event loop.
func SelectionRangesAt(path string, lines []string, line, col int) []NodeRange {
	l, ok := lang.ByPath(path)
	if !ok || l.Grammar == nil {
		return nil
	}
	gi, ok := l.Grammar.(*grammarImpl)
	if !ok {
		return nil
	}
	if line < 0 || line >= len(lines) {
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
	pt := ts.Point{Row: uint(line), Column: uint(conv.byteCol(line, col))}
	node := tree.RootNode().NamedDescendantForPointRange(pt, pt)

	var out []NodeRange
	for ; node != nil; node = node.Parent() {
		start, end := node.StartPosition(), node.EndPosition()
		r := NodeRange{
			StartLine: int(start.Row),
			StartCol:  conv.runeCol(int(start.Row), int(start.Column)),
			EndLine:   int(end.Row),
			EndCol:    conv.runeCol(int(end.Row), int(end.Column)),
		}
		if r.StartLine == r.EndLine && r.StartCol >= r.EndCol {
			continue // zero-width node: nothing to select
		}
		if n := len(out); n > 0 && out[n-1] == r {
			continue // parent with the identical extent: one ladder step
		}
		out = append(out, r)
	}
	return out
}

// ExpressionEndingAt returns the extent of the widest syntax node whose kind is
// in kinds and which ends exactly at (line, col) — both editor rune coordinates
// — in a fresh parse of lines. It is the postfix-completion expression finder
// (#1913): with the caret just after a `.`, the node ending at that dot is the
// expression the template rewrites.
//
// The buffer is syntactically broken while the user types (`err.` is not a Go
// statement), but Tree-sitter's error recovery still parses everything up to
// the dot — the dot and the partial template word land in a trailing ERROR node
// — so the expression node itself is intact. The kind filter is what keeps the
// widest node honest: `x := foo(bar).` also has a short_var_declaration ending
// at the dot, and only the caller's expression kinds exclude it.
//
// Only nodes starting on line are considered: the accept path rewrites a
// single-line span. ok=false when the path has no grammar, the position is
// outside the parsed text, or no node qualifies.
func ExpressionEndingAt(path string, lines []string, line, col int, kinds []string) (NodeRange, bool) {
	if len(kinds) == 0 || line < 0 || line >= len(lines) {
		return NodeRange{}, false
	}
	l, ok := lang.ByPath(path)
	if !ok || l.Grammar == nil {
		return NodeRange{}, false
	}
	gi, ok := l.Grammar.(*grammarImpl)
	if !ok {
		return NodeRange{}, false
	}
	want := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}

	src := []byte(strings.Join(lines, "\n"))
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(gi.lang); err != nil {
		return NodeRange{}, false
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return NodeRange{}, false
	}
	defer tree.Close()

	conv := newColMapper(lines)
	// Byte offset of the position in the joined source: every preceding line
	// plus its "\n", then the rune→byte column conversion within the line.
	target := 0
	for i := 0; i < line; i++ {
		target += len(lines[i]) + 1
	}
	target += conv.byteCol(line, col)

	best, found := NodeRange{}, false
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		if int(n.StartByte()) > target {
			return // the whole subtree lies after the dot
		}
		if int(n.EndByte()) == target && want[n.Kind()] {
			start, end := n.StartPosition(), n.EndPosition()
			if int(start.Row) == line {
				r := NodeRange{
					StartLine: line,
					StartCol:  conv.runeCol(line, int(start.Column)),
					EndLine:   int(end.Row),
					EndCol:    conv.runeCol(int(end.Row), int(end.Column)),
				}
				// Widest wins: `foo(bar).if` wraps the whole call, not its
				// argument list.
				if !found || r.StartCol < best.StartCol {
					best, found = r, true
				}
			}
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return best, found
}

// SyntaxChainAt returns the syntax-node chain at (line, col) — both editor
// rune coordinates — for the structural-value copies gy / gY (#2499): the
// smallest named node containing the position and every ancestor up to the
// root, innermost first, each one carrying its direct children with the field
// names its grammar gives them.
//
// It is the sibling of SelectionRangesAt: same fresh parse from a line
// snapshot, same closing of parser and tree before returning, but the caller
// needs *shape* rather than extents — which child is the "value" half of a
// pair, where an element's tags end — so the snapshot keeps kinds and fields
// instead of rune ranges. Byte offsets are into strings.Join(lines, "\n"),
// which is the source internal/structval slices; children are one level deep,
// because no extraction rule looks further and a copy command must not
// snapshot a whole document.
//
// nil when the path has no grammar or the position is outside the parsed text.
func SyntaxChainAt(path string, lines []string, line, col int) []structval.Node {
	l, ok := lang.ByPath(path)
	if !ok || l.Grammar == nil {
		return nil
	}
	gi, ok := l.Grammar.(*grammarImpl)
	if !ok {
		return nil
	}
	if line < 0 || line >= len(lines) {
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
	pt := ts.Point{Row: uint(line), Column: uint(conv.byteCol(line, col))}

	var out []structval.Node
	for node := tree.RootNode().NamedDescendantForPointRange(pt, pt); node != nil; node = node.Parent() {
		out = append(out, snapshotNode(node))
	}
	return out
}

// snapshotNode copies one node plus its direct children into the pure-Go
// snapshot internal/structval reads. Anonymous children are kept: an element's
// `>` and a pair's `:` mark where the interesting text begins and ends.
func snapshotNode(n *ts.Node) structval.Node {
	out := structval.Node{
		Kind:  n.Kind(),
		Named: n.IsNamed(),
		Start: int(n.StartByte()),
		End:   int(n.EndByte()),
	}
	count := n.ChildCount()
	if count == 0 {
		return out
	}
	out.Children = make([]structval.Node, 0, count)
	for i := uint(0); i < count; i++ {
		c := n.Child(i)
		if c == nil {
			continue
		}
		out.Children = append(out.Children, structval.Node{
			Kind:  c.Kind(),
			Field: n.FieldNameForChild(uint32(i)),
			Named: c.IsNamed(),
			Start: int(c.StartByte()),
			End:   int(c.EndByte()),
		})
	}
	return out
}
