package todoindex

// filter.go is the TODO index's dialect of the shared filter syntax (#2156):
// the schema naming what a term may say here, and the gate every indexed tag
// passes. The row itself is internal/filterbar, the syntax internal/filterexpr
// — the overlay only says what its entries are made of.
//
// The two pre-#2156 single-key filters survive as sugar over the same
// expression: ctrl+t cycles the tag: term through the configured patterns,
// ctrl+o toggles scope:file. The chips row above the input renders those two
// terms, so pressing the key and typing the term are visibly one thing.

import (
	"strings"

	"ike/internal/filterexpr"
)

// Scopes is the scope gate's vocabulary: the file open in the editor when the
// index was opened, or the whole project (the default).
var Scopes = []string{"file", "project"}

// schemaFor builds the filter language for one configured tag set: tag: takes
// the pattern words themselves, so completion offers exactly what the project
// indexes.
func schemaFor(patterns []string) filterexpr.Schema {
	tags := make([]string, 0, len(patterns))
	for _, p := range patterns {
		tags = append(tags, strings.ToUpper(p))
	}
	return filterexpr.Schema{Fields: []filterexpr.Field{
		{Name: "tag", Values: tags, Doc: "tag word, repeatable (OR) — ctrl+t cycles"},
		{Name: "file", ValueDoc: "a path or glob", Doc: "file path, glob or substring"},
		{Name: "scope", Values: Scopes, Doc: "current file only (ctrl+o)"},
	}}
}

// matches gates one entry through the filter. Terms of different fields are
// AND'd, repeats of one field OR'd, and the free text is the same fuzzy gate
// the Issues pane uses, run over the tag's source line.
func (m *Model) matches(e Entry) bool {
	q := m.filter.Query()
	if q.Empty() {
		return true
	}
	if q.Value("scope") == "file" && (m.curPath == "" || e.Item.Path != m.curPath) {
		return false
	}
	if tags := q.Values("tag"); len(tags) > 0 && !hasString(tags, e.Tag) {
		return false
	}
	if files := q.Values("file"); len(files) > 0 && !m.pathMatches(files, e.Item.Path) {
		return false
	}
	if _, ok := filterexpr.MatchText(q.Match, e.Item.Text); !ok {
		return false
	}
	return true
}

// pathMatches applies the file: values to a path, OR'd, against both the path
// as rendered (project-relative) and the raw one — a filter typed off the
// screen has to hit what the screen shows.
func (m *Model) pathMatches(pats []string, path string) bool {
	shown := path
	if m.displayPath != nil {
		shown = m.displayPath(path)
	}
	for _, p := range pats {
		if filterexpr.MatchPath(p, shown) || filterexpr.MatchPath(p, path) {
			return true
		}
	}
	return false
}

// hasString reports whether v is in list.
func hasString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// FileOnly reports whether the filter scopes the list to the current file —
// the state ctrl+o toggles (tests, the chips row).
func (m *Model) FileOnly() bool { return m.filter.HasTerm("scope", "file") }

// toggleFileScope is the ctrl+o shortcut: it writes (or removes) scope:file
// in the shared filter.
func (m *Model) toggleFileScope() {
	if m.FileOnly() {
		m.filter.SetTerm("scope", "")
		return
	}
	m.filter.SetTerm("scope", "file")
}

// cycleTag is the ctrl+t shortcut: it steps the tag: term through the
// configured patterns and back to "no tag term", writing the result into the
// same filter a typed tag: goes into.
func (m *Model) cycleTag() {
	next := ""
	if cur := m.filter.Query().Value("tag"); cur != "" {
		for i, p := range m.patterns {
			if strings.EqualFold(p, cur) && i+1 < len(m.patterns) {
				next = strings.ToUpper(m.patterns[i+1])
				break
			}
		}
	} else if len(m.patterns) > 0 {
		next = strings.ToUpper(m.patterns[0])
	}
	m.filter.SetTerm("tag", next)
}
