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
// are AND'd, repeats of one field OR'd; a negated term or word
// (-file:_test.go, -mock) drops what it matches (#2951).
func (m *Model) matches(ref ilsp.Reference) bool {
	q := m.filter.Query()
	if q.Empty() {
		return true
	}
	if !q.Keeps("file", func(v string) bool { return m.pathMatches(v, ref.Path) }) ||
		!q.Keeps("text", func(v string) bool {
			return strings.Contains(strings.ToLower(ref.Preview), strings.ToLower(v))
		}) {
		return false
	}
	_, ok := q.MatchFree(m.shorten(ref.Path) + " " + ref.Preview)
	return ok
}

// pathMatches applies one file: value to a path, against both the path as
// rendered (project-relative) and the raw one — a filter typed off the
// screen has to hit what the screen shows.
func (m *Model) pathMatches(pat, path string) bool {
	return filterexpr.MatchPath(pat, m.shorten(path)) || filterexpr.MatchPath(pat, path)
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
