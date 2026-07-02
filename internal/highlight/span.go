// Package highlight provides fast lexical syntax highlighting via Tree-sitter
// (Roadmap 0100). It is the base layer the editor renders under the cursor and
// selection; an optional LSP semantic-token overlay is a later increment.
//
// Parsing uses CGo (the Tree-sitter C library plus per-language grammars) and is
// therefore isolated behind a `cgo` build tag: parse_cgo.go implements the real
// parser, parse_stub.go is a no-op so `CGO_ENABLED=0` builds still compile (with
// highlighting simply disabled). Everything in this file (the span model, the
// per-line index and the theme) is pure Go and compiles in either mode.
package highlight

// Span is one highlighted run on a single line: the half-open rune-column range
// [StartCol, EndCol) carries the Tree-sitter capture name (e.g. "keyword",
// "string", "function") that the theme resolves to a colour. Multi-line grammar
// nodes are split into one Span per line so the editor can look them up per row.
type Span struct {
	Line     int
	StartCol int
	EndCol   int
	Capture  string
}

// SpansMsg delivers a freshly parsed span set for one document back into the
// editor as a tea.Msg. Version is the editor's document version the parse ran
// against, so stale results (a newer edit already landed) are dropped.
type SpansMsg struct {
	Path    string
	Version int
	Spans   []Span
}

// Index is a per-line lookup over a span set, built once when the editor caches
// a SpansMsg and queried per rune cell during rendering.
type Index struct {
	byLine map[int][]Span
}

// NewIndex groups spans by line for O(spans-on-line) column lookup.
func NewIndex(spans []Span) Index {
	byLine := make(map[int][]Span, len(spans))
	for _, s := range spans {
		byLine[s.Line] = append(byLine[s.Line], s)
	}
	return Index{byLine: byLine}
}

// CaptureAt returns the capture name covering (line, col), or "" if none. When
// spans overlap, the first one covering the cell wins — Tree-sitter's Captures
// iterator yields more specific patterns first, matching its highlight semantics.
func (ix Index) CaptureAt(line, col int) string {
	for _, s := range ix.byLine[line] {
		if col >= s.StartCol && col < s.EndCol {
			return s.Capture
		}
	}
	return ""
}

// Empty reports whether the index holds no spans.
func (ix Index) Empty() bool { return len(ix.byLine) == 0 }
