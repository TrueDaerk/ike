package archview

// filter.go gives the archive viewer the search every other list pane has
// (#2409): the shared filter row (#2156, internal/filterbar over
// internal/filterexpr), opened with "/" or the shared find chord, narrowing
// the entry tree live.
//
// The gate runs over the *tree* rather than the flat entry list, so the
// directories a tar never named explicitly — the common shape — filter like
// the ones it did, and a directory survives exactly when it or one of its
// members matches. Nothing about the pane's read-only contract changes: the
// filter only decides which of the listed headers are shown.

import (
	"ike/internal/filterexpr"
	"ike/internal/ui"
)

// Kinds is the type gate's vocabulary: the entries that carry bytes, and the
// directories that hold them.
var Kinds = []string{"file", "dir"}

// Schema is the pane's filter language.
var Schema = filterexpr.Schema{Fields: []filterexpr.Field{
	{Name: "name", Aliases: []string{"path"}, ValueDoc: "a path or glob",
		Doc: "entry path, glob or substring"},
	{Name: "type", Aliases: []string{"kind"}, Values: Kinds,
		Doc: "entries of one kind only"},
}}

// OpenSearch implements the pane's Searchable capability (#2409): the shared
// find chord focuses the filter row, exactly as "/" does.
func (m *Model) OpenSearch() bool {
	m.focusFilter()
	return true
}

// NextMatch implements the pane's match-step capability (#2410). The pane's
// search is a filter, so its "matches" are the entries that match it — not the
// parent directories the tree keeps around them for context: cmd+g walks the
// hits while the filter row keeps the keyboard and the expression stays
// editable.
func (m *Model) NextMatch() ui.MatchStep { return m.stepFiltered(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.stepFiltered(-1) }

func (m *Model) stepFiltered(delta int) ui.MatchStep {
	if !m.filter.Active() {
		return ui.NoStep
	}
	next, st := ui.StepOver(m.cursor, len(m.rows), delta, func(i int) bool {
		return m.matches(m.rows[i].full, m.rows[i].isDir)
	})
	m.cursor = next
	m.clampScroll()
	return m.filter.ShowStep(st)
}

// focusFilter puts the cursor in the filter row. The completion source is
// bound here rather than in New: the model is a value embedded in a
// pane.Instance, so only a pointer receiver sees the copy the pane renders.
func (m *Model) focusFilter() {
	m.filter.Candidates = func(field string) []string {
		if field == "name" {
			return m.entryNames()
		}
		return nil
	}
	m.filter.Focus()
}

// entryNames is the completion vocabulary of the name: field — every path the
// archive lists, in listing order.
func (m *Model) entryNames() []string {
	out := make([]string, 0, len(m.listing.Entries))
	for _, e := range m.listing.Entries {
		out = append(out, e.Name)
	}
	return out
}

// Filter returns the filter row's raw expression (tests).
func (m *Model) Filter() string { return m.filter.Text() }

// Filtering reports whether the filter row holds the keyboard (tests).
func (m *Model) Filtering() bool { return m.filter.Active() }

// matches gates one tree node through the filter. Terms of different fields
// are AND'd and repeats of one field OR'd, like every other pane's dialect;
// the free match text is the same fuzzy gate, run over the entry path.
func (m *Model) matches(full string, isDir bool) bool {
	q := m.filter.Query()
	if q.Empty() {
		return true
	}
	kind := "file"
	if isDir {
		kind = "dir"
	}
	if kinds := q.Values("type"); len(kinds) > 0 && !hasString(kinds, kind) {
		return false
	}
	if names := q.Values("name"); len(names) > 0 && !anyPath(names, full) {
		return false
	}
	if _, ok := filterexpr.MatchText(q.Match, full); !ok {
		return false
	}
	return true
}

// keeps reports whether n survives the filter: the node itself matches, or one
// of its descendants does — the tree would otherwise lose the rows that give a
// match its context.
func (m *Model) keeps(n *node) bool {
	if m.matches(n.full, n.isDir) {
		return true
	}
	for _, c := range n.children {
		if m.keeps(c) {
			return true
		}
	}
	return false
}

// anyPath applies the name: values to an entry path, OR'd.
func anyPath(pats []string, name string) bool {
	for _, p := range pats {
		if filterexpr.MatchPath(p, name) {
			return true
		}
	}
	return false
}

// hasString reports membership.
func hasString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
