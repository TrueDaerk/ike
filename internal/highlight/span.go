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

import "ike/internal/lang"

// Span is one highlighted run on a single line: the half-open rune-column range
// [StartCol, EndCol) carries the Tree-sitter capture name (e.g. "keyword",
// "string", "function") that the theme resolves to a colour. Multi-line grammar
// nodes are split into one Span per line so the editor can look them up per row.
type Span struct {
	Line     int
	StartCol int
	EndCol   int
	Capture  string
	// Replace marks the span as a conceal-with-stand-in (#1585): on lines
	// the caret is not on the editor renders Replace instead of the source
	// runes (a "%20" range displays as " "). Empty — the normal case — means
	// the span is a plain style run; the Capture "conceal" alone (markdown
	// marker chrome, #881) hides the range without a stand-in.
	Replace string
}

// SpansMsg delivers a freshly parsed span set for one document back into the
// editor as a tea.Msg. Version is the editor's document version the parse ran
// against, so stale results (a newer edit already landed) are dropped.
type SpansMsg struct {
	Path    string
	Version int
	Spans   []Span
	// Scopes are the sticky-scroll scopes (#168) collected by the same parse,
	// in pre-order; nil when the language registers no ScopeNodes.
	Scopes []Scope
	// Folds are the foldable regions (#144) collected by the same parse, in
	// pre-order; nil when the language registers no FoldNodes (and no
	// ScopeNodes to fall back on).
	Folds []Fold
	// Notes are the Go-computed lint notes (#1623) produced by the same pass;
	// nil when the language registers no Lint.
	Notes []lang.Note
}

// Index is a per-line lookup over a span set, built once when the editor caches
// a SpansMsg and queried per rune cell during rendering.
type Index struct {
	byLine map[int][]Span
}

// NewIndex groups spans by line for O(spans-on-line) column lookup.
// "conceal.extent" spans (#1599) are dropped: they are a data channel for the
// editor's conceal layer (the enclosing inline spans whose markers reveal
// under the caret), never a style — indexed, an extent node would shadow the
// styles of everything inside it under first-covering-wins.
func NewIndex(spans []Span) Index {
	byLine := make(map[int][]Span, len(spans))
	for _, s := range spans {
		if s.Capture == "conceal.extent" {
			continue
		}
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

// LineSpans returns every indexed span of one line, in iterator order — for
// callers that render a narrow window of a very long line and want to filter
// once instead of paying CaptureAt's linear scan per rune cell (#2386).
func (ix Index) LineSpans(line int) []Span { return ix.byLine[line] }

// Empty reports whether the index holds no spans.
func (ix Index) Empty() bool { return len(ix.byLine) == 0 }
