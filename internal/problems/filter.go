package problems

// filter.go is the Problems pane's dialect of the shared filter syntax
// (#2156): the schema naming what a term may say here, and the gate every
// listed diagnostic passes. The row itself is internal/filterbar, the syntax
// internal/filterexpr — this file is only the pane-specific part.
//
// The pre-#2156 single-key scope toggle survives as sugar: 'f' writes
// scope:file into the same filter, so the shortcut and a typed expression are
// one mechanism. scope: is resolved against the *current* active editor file
// rather than a path baked in when the key was pressed, which is what keeps
// the scope following the editor the way it always did.

import (
	"strings"

	"ike/internal/filterbar"
	"ike/internal/filterexpr"
	ilsp "ike/internal/lsp"
)

// Severities is the severity gate's vocabulary, in severity order.
var Severities = []string{"error", "warning", "info", "hint"}

// Scopes is the scope gate's vocabulary: the active editor's file, or the
// whole project (the default, so scope:project only ever spells out the
// absence of a scope).
var Scopes = []string{"file", "project"}

// Schema is the pane's filter language.
var Schema = filterexpr.Schema{Fields: []filterexpr.Field{
	{Name: "severity", Aliases: []string{"sev"}, Values: Severities,
		Doc: "severity gate, repeatable (OR)"},
	{Name: "file", ValueDoc: "a path or glob", Doc: "file path, glob or substring"},
	{Name: "code", ValueDoc: "a diagnostic code", Doc: "diagnostic code substring"},
	{Name: "source", Aliases: []string{"src"}, ValueDoc: "a source name",
		Doc: "reporting server, linter or task"},
	{Name: "scope", Values: Scopes, Doc: "current file only ('f')"},
}}

// sevName maps a severity number to its filter word.
func sevName(sev int) string {
	switch normSev(sev) {
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	}
	return "error"
}

// matches gates one diagnostic through the filter. Terms of different fields
// are AND'd, repeats of one field OR'd, and the free text is the same fuzzy
// gate the Issues pane uses, run over the message and code.
func (m *Model) matches(path string, d ilsp.Diagnostic) bool {
	q := m.filter.Query()
	if q.Empty() {
		return true
	}
	if q.Value("scope") == "file" && (m.activePath == "" || path != m.activePath) {
		return false
	}
	if sevs := q.Values("severity"); len(sevs) > 0 && !hasString(sevs, sevName(d.Severity)) {
		return false
	}
	if files := q.Values("file"); len(files) > 0 && !m.pathMatches(files, path) {
		return false
	}
	if codes := q.Values("code"); len(codes) > 0 && !anySubstring(codes, d.Code) {
		return false
	}
	if srcs := q.Values("source"); len(srcs) > 0 && !anySubstring(srcs, d.Source) {
		return false
	}
	if _, ok := filterexpr.MatchText(q.Match, matchHay(d)); !ok {
		return false
	}
	return true
}

// pathMatches applies the file: values to a path, OR'd, against both the
// path as rendered (project-relative) and the raw one — a filter typed off
// the screen has to hit what the screen shows.
func (m *Model) pathMatches(pats []string, path string) bool {
	shown := m.shorten(path)
	for _, p := range pats {
		if filterexpr.MatchPath(p, shown) || filterexpr.MatchPath(p, path) {
			return true
		}
	}
	return false
}

// matchHay is what the free match text is run against: the diagnostic's
// message and its code, the two things the row shows.
func matchHay(d ilsp.Diagnostic) string {
	if d.Code == "" {
		return d.Message
	}
	return d.Message + " " + d.Code
}

// anySubstring reports whether any needle is a case-insensitive substring of
// hay; an empty hay matches nothing, so code:x hides diagnostics with no code.
func anySubstring(needles []string, hay string) bool {
	if hay == "" {
		return false
	}
	low := strings.ToLower(hay)
	for _, n := range needles {
		if strings.Contains(low, strings.ToLower(n)) {
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

// FileOnly reports whether the filter scopes the list to the active editor's
// file — the state 'f' toggles (tests, persistence).
func (m *Model) FileOnly() bool { return m.filter.HasTerm("scope", "file") }

// Filter exposes the filter row (tests, and the pane host's key routing).
func (m *Model) Filter() *filterbar.Model { return &m.filter }

// toggleFileScope is the 'f' shortcut: it writes (or removes) scope:file in
// the shared filter, so the quick key and the typed expression are the same
// filter (#2156).
func (m *Model) toggleFileScope() bool {
	if m.FileOnly() {
		return m.filter.SetTerm("scope", "")
	}
	return m.filter.SetTerm("scope", "file")
}

// filePaths lists the currently listed files as completion candidates for
// file:, project-relative like the headers.
func (m *Model) filePaths() []string {
	if m.store == nil {
		return nil
	}
	paths := m.store.Paths()
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, m.shorten(p))
	}
	return out
}
