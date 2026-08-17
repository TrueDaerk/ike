//go:build cgo

package highlight

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/lang"
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

// colMapper converts byte offsets to rune columns per line. ASCII-only lines
// (the common case) take a fast path where byte == rune column.
type colMapper struct{ lines []string }

func newColMapper(lines []string) colMapper { return colMapper{lines: lines} }

func (c colMapper) lineBytes(line int) int {
	if line < 0 || line >= len(c.lines) {
		return 0
	}
	return len(c.lines[line])
}

// byteCol is runeCol's inverse: it maps a rune column within line to a byte
// offset, clamping a column past the end to the line's byte length.
func (c colMapper) byteCol(line, runeOff int) int {
	if line < 0 || line >= len(c.lines) || runeOff <= 0 {
		return 0
	}
	s := c.lines[line]
	if isASCII(s) {
		if runeOff > len(s) {
			return len(s)
		}
		return runeOff
	}
	n := 0
	for i := range s { // ranging a string yields the byte index of each rune start
		if n == runeOff {
			return i
		}
		n++
	}
	return len(s)
}

func (c colMapper) runeCol(line, byteOff int) int {
	if line < 0 || line >= len(c.lines) {
		return 0
	}
	s := c.lines[line]
	if byteOff >= len(s) {
		return len([]rune(s))
	}
	if isASCII(s[:byteOff]) {
		return byteOff
	}
	return len([]rune(s[:byteOff]))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
