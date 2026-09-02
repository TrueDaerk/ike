package pane

// searchable.go is the pane-side half of the shared find chord (#2409). Every
// pane that owns a search or a filter of its own binds "/" to it, but the
// chord users reach for first is cmd+f — the one the editor and every other
// IDE trained them on. Rather than teaching each pane a second key, the app
// binds one Global command (search.open) and asks the focused pane for this
// capability; the editor keeps editor.find on the same chord in its own, more
// specific context.
//
// A pane kind with no search of its own simply returns nil below, and the
// root model says so instead of silently swallowing the chord.

// Searchable is the capability of a pane that owns a search or a filter.
type Searchable interface {
	// OpenSearch opens the pane's search or filter and reports whether it
	// did. false means "not available right now" — a terminal handing the
	// chord to an alt-screen child, a viewer with nothing loaded — and the
	// caller notifies rather than swallowing the key.
	OpenSearch() bool
}

// Searchable returns the focused component of the instance as a Searchable,
// or nil when this pane kind has no search. An editor pane hosting a terminal
// (#573) or a content tab (#1778) delegates to what the tab holds, exactly as
// ContextID does, so the chord acts on what the pane actually shows.
func (i *Instance) Searchable() Searchable {
	switch i.kind {
	case KindExplorer:
		return &i.exp
	case KindEditor:
		// A plain editor tab searches through editor.find, bound in the more
		// specific Editor context, so it never reaches this path.
		if t := i.activeTab(); t != nil {
			if t.IsTerminal() {
				return t.Terminal()
			}
			if t.inst != nil {
				return t.inst.Searchable()
			}
		}
		return nil
	case KindTerminal:
		return &i.term
	case KindDiff:
		return &i.df
	case KindProblems:
		return &i.pp
	case KindUsages:
		return &i.up
	case KindHTTP:
		return &i.hp
	case KindArchive:
		return &i.av
	case KindData:
		return &i.dv
	case KindIssues:
		return &i.gi
	case KindDOM:
		return &i.dm
	case KindMarkdown:
		// The image viewer shares the "preview" context but has no text to
		// search, so only the markdown half is Searchable.
		return &i.md
	}
	return nil
}

// OpenSearch opens the focused pane's own search, reporting whether the pane
// has one and it opened.
func (i *Instance) OpenSearch() bool {
	s := i.Searchable()
	return s != nil && s.OpenSearch()
}
