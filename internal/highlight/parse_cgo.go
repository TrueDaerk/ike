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
	spans, _ := parseScoped(g, nil, lines)
	return spans
}

// parseScoped is parse plus sticky-scroll scope collection (#168): one Tree-sitter
// parse yields both the highlight spans and, when scopeKinds is non-empty, the
// multi-line nodes of those kinds as Scopes in pre-order (outer before inner).
// Sharing the parse keeps sticky scroll free — no second CGo pass per edit.
func parseScoped(g lang.Grammar, scopeKinds []string, lines []string) ([]Span, []Scope) {
	gi, ok := g.(*grammarImpl)
	if !ok {
		return nil, nil
	}
	tsLang, query, ok := gi.compiled()
	if !ok {
		return nil, nil
	}

	src := []byte(strings.Join(lines, "\n"))
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tsLang); err != nil {
		return nil, nil
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, nil
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
	return spans, scopes
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
