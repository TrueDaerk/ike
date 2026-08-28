package usages

// filter.go is the Usages pane's dialect of the shared filter syntax (#2156):
// which fields a term may name here, and the gate every listed reference
// passes. The row itself is internal/filterbar, the syntax internal/filterexpr
// — the pane only says what its rows are made of.
//
// A usage list is a flat set of hits, so the vocabulary is small: file: picks
// files (glob or substring), text: gates the source-line preview, and bare
// words are the same fuzzy match the Issues pane uses, run over the preview
// and the path together.

import (
	"strings"

	"ike/internal/filterbar"
	"ike/internal/filterexpr"
	ilsp "ike/internal/lsp"
)

// Schema is the pane's filter language.
var Schema = filterexpr.Schema{Fields: []filterexpr.Field{
	{Name: "file", ValueDoc: "a path or glob", Doc: "file path, glob or substring"},
	{Name: "text", ValueDoc: "a substring", Doc: "source-line substring"},
}}

// Filter exposes the filter row (tests, and the pane host's key routing).
func (m *Model) Filter() *filterbar.Model { return &m.filter }

// matches gates one reference through the filter. Terms of different fields
// are AND'd, repeats of one field OR'd.
func (m *Model) matches(ref ilsp.Reference) bool {
	q := m.filter.Query()
	if q.Empty() {
		return true
	}
	if files := q.Values("file"); len(files) > 0 && !m.pathMatches(files, ref.Path) {
		return false
	}
	if texts := q.Values("text"); len(texts) > 0 && !anySubstring(texts, ref.Preview) {
		return false
	}
	if _, ok := filterexpr.MatchText(q.Match, m.shorten(ref.Path)+" "+ref.Preview); !ok {
		return false
	}
	return true
}

// pathMatches applies the file: values to a path, OR'd, against both the path
// as rendered (project-relative) and the raw one — a filter typed off the
// screen has to hit what the screen shows.
func (m *Model) pathMatches(pats []string, path string) bool {
	shown := m.shorten(path)
	for _, p := range pats {
		if filterexpr.MatchPath(p, shown) || filterexpr.MatchPath(p, path) {
			return true
		}
	}
	return false
}

// anySubstring reports whether any needle is a case-insensitive substring of
// hay.
func anySubstring(needles []string, hay string) bool {
	low := strings.ToLower(hay)
	for _, n := range needles {
		if strings.Contains(low, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// filePaths lists the files behind the current result set as completion
// candidates for file:, project-relative like the headers.
func (m *Model) filePaths() []string {
	var out []string
	seen := map[string]bool{}
	for _, ref := range m.all {
		p := m.shorten(ref.Path)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
