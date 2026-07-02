//go:build cgo

package highlight

import (
	_ "embed"
	"strings"
	"sync"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsphp "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tspy "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

//go:embed queries/go.scm
var goQuery string

//go:embed queries/python.scm
var pythonQuery string

//go:embed queries/php.scm
var phpQuery string

// grammar bundles a compiled language and its highlight query. Languages and
// queries are read-only once built, so they are compiled once and shared across
// parse goroutines; only the Parser and QueryCursor (created per call) are
// stateful.
type grammar struct {
	lang  *ts.Language
	query *ts.Query
}

var (
	grammarOnce sync.Once
	grammars    map[string]*grammar
)

// initGrammars compiles each language + query exactly once. A query that fails
// to compile (grammar/query version skew) disables that language rather than
// crashing — the editor falls back to plain text for it.
func initGrammars() {
	grammars = make(map[string]*grammar)
	specs := []struct {
		id    string
		lang  *ts.Language
		query string
	}{
		{"go", ts.NewLanguage(tsgo.Language()), goQuery},
		{"python", ts.NewLanguage(tspy.Language()), pythonQuery},
		{"php", ts.NewLanguage(tsphp.LanguagePHP()), phpQuery},
	}
	for _, s := range specs {
		q, qerr := ts.NewQuery(s.lang, s.query)
		if qerr != nil {
			continue // skip a language whose query won't compile
		}
		grammars[s.id] = &grammar{lang: s.lang, query: q}
	}
}

// parse runs Tree-sitter over the joined lines and returns highlight spans in
// editor rune coordinates. It runs off the Update goroutine (inside a tea.Cmd),
// so the CGo work never blocks the event loop.
func parse(lang string, lines []string) []Span {
	grammarOnce.Do(initGrammars)
	g := grammars[lang]
	if g == nil {
		return nil
	}

	src := []byte(strings.Join(lines, "\n"))
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(g.lang); err != nil {
		return nil
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	// byteToRune[line] maps a byte offset within that line to a rune column.
	conv := newColMapper(lines)

	cursor := ts.NewQueryCursor()
	defer cursor.Close()
	names := g.query.CaptureNames()

	var spans []Span
	captures := cursor.Captures(g.query, tree.RootNode(), src)
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
	return spans
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
