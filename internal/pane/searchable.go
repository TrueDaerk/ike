package pane

import "ike/internal/ui"

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

	// NextMatch / PrevMatch step through the matches of the pane's *open*
	// search (#2410), leaving the input focused and the query untouched:
	// cmd+g and cmd+shift+g do in every pane what they always did in the
	// editor, without enter having to apply and blur first.
	//
	// A pane whose search is closed returns ui.NoStep, and the chord keeps
	// its older meaning at the root model — repeating the editor's in-file
	// search, or walking the retained find-in-path results.
	NextMatch() ui.MatchStep
	PrevMatch() ui.MatchStep
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
	case KindHex:
		return &i.hv
	case KindNotebook:
		return &i.nv
	case KindIssues:
		return &i.gi
	case KindDOM:
		return &i.dm
	case KindDeps:
		return &i.dep
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

// StepMatch moves the focused pane's open search by delta (#2410): +1 to the
// next match, -1 to the previous one. It reports ui.NoStep for a pane kind
// without a search and for one whose search is closed, so the caller can fall
// back to what the chord meant before.
func (i *Instance) StepMatch(delta int) ui.MatchStep {
	s := i.Searchable()
	if s == nil {
		return ui.NoStep
	}
	if delta < 0 {
		return s.PrevMatch()
	}
	return s.NextMatch()
}
