package pane

// keyreporter.go is the pane-side half of the deferred "unbound" verdict
// (#2303, #2889). The keymap layer resolves a chord before the focused pane
// sees it, so a chord the pane answers itself — the editor's alt+delete, a
// tool pane's search prompt taking alt+backspace — looks unbound from up
// there. The root model holds the verdict back, routes the key, and asks the
// instance afterwards whether the pane acted on it.

// KeyReporter is the capability of a pane component that can say whether its
// key handler acted on the last key press it was given. Tool panes get it by
// embedding ui.KeyVerdict; the editor has carried it since #2303.
type KeyReporter interface {
	HandledLastKey() bool
}

// keyReporter returns the focused component of the instance as a
// KeyReporter, or nil when this pane kind cannot report. An editor pane
// hosting a content tab (#1778) or a tab in Preview view (#2766) delegates to
// what the tab shows, exactly as Searchable does, so the answer comes from
// the component the key actually reached.
func (i *Instance) keyReporter() KeyReporter {
	switch i.kind {
	case KindEditor:
		t := i.activeTab()
		switch {
		case t == nil, t.IsTerminal():
			// A terminal tab owns its unbound chords outright (#2701): the
			// root model never holds a verdict for it.
			return nil
		case t.inst != nil:
			return t.inst.keyReporter()
		}
		if v := t.shownView(); v != nil {
			return v.keyReporter()
		}
		if ed := t.Editor(); ed != nil {
			return ed
		}
		return nil
	case KindDiff:
		if i.dfEdit != nil {
			return i.dfEdit // edit mode: keys belong to the embedded editor (#496)
		}
	case KindExplorer:
		return &i.exp
	case KindHTTP:
		return &i.hp
	case KindNotebook:
		return &i.nv
	case KindIssues:
		return &i.gi
	case KindAgentTrace:
		return &i.at
	}
	return nil
}

// HandledLastKey reports whether the focused component acted on the last key
// press routed to it. A pane kind that cannot report says false, so the
// held-back chord is logged as unbound exactly as before the deferral.
func (i *Instance) HandledLastKey() bool {
	r := i.keyReporter()
	return r != nil && r.HandledLastKey()
}
