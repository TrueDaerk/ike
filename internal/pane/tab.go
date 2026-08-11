package pane

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/terminal"
	"ike/internal/theme"
)

// Tab is one slot in a tab-hosting pane's tab strip (0350, #573, #1778): a
// document editor, an embedded terminal, or any other tabbable pane content
// wrapped as a nested Instance — so a preview, diff, data viewer or HTTP
// response can live in a tab next to the files it belongs to. Exactly one of
// ed/term/inst is non-nil.
type Tab struct {
	ed   *editor.Model
	term *terminal.Model
	// inst carries non-editor, non-terminal content (#1778): a nested
	// Instance of a viewer kind, reusing the per-kind dispatch the pane
	// itself uses. It never nests an editor or explorer kind.
	inst *Instance
	// lastUsed is the instance's use-sequence stamp of the last activation,
	// the recency the tab-limit eviction orders by (#742).
	lastUsed int
	// pinned protects the tab from the tab-limit LRU eviction and from
	// "Close Others" (#1172); manual closes stay allowed. It persists with
	// the layout identity.
	pinned bool
}

// newEditorTab wraps an editor model as a tab slot.
func newEditorTab(ed *editor.Model) *Tab { return &Tab{ed: ed} }

// newTerminalTab wraps a terminal model as a tab slot.
func newTerminalTab(t *terminal.Model) *Tab { return &Tab{term: t} }

// newContentTab wraps a nested content instance as a tab slot (#1778).
func newContentTab(inst *Instance) *Tab { return &Tab{inst: inst} }

// IsTerminal reports whether the tab hosts a terminal rather than a document.
func (t *Tab) IsTerminal() bool { return t.term != nil }

// Editor returns the tab's editor model, nil for other tab contents.
func (t *Tab) Editor() *editor.Model { return t.ed }

// Terminal returns the tab's terminal model, nil for other tab contents.
func (t *Tab) Terminal() *terminal.Model { return t.term }

// Content returns the tab's nested content instance (#1778), nil for editor
// and terminal tabs.
func (t *Tab) Content() *Instance { return t.inst }

// Kind reports the kind of content the tab carries: KindEditor for a document
// tab, KindTerminal for a terminal tab, the nested instance's kind otherwise.
func (t *Tab) Kind() Kind {
	switch {
	case t.term != nil:
		return KindTerminal
	case t.inst != nil:
		return t.inst.kind
	}
	return KindEditor
}

// Title returns a non-editor tab's display label. A terminal tab labels
// itself by the application-set OSC title when present, else the shell
// binary's base name; a content tab (#1778) by its kind's short title.
// Editor tabs are labelled by the caller (basename + markers), which needs
// the whole tab list for disambiguation.
func (t *Tab) Title() string {
	if t.inst != nil {
		return t.inst.ContentTitle()
	}
	if t.term == nil {
		return ""
	}
	if l := t.term.Label(); l != "" {
		return l
	}
	if osc := t.term.Title(); osc != "" {
		return osc
	}
	if s := t.term.ShellPath(); s != "" {
		return filepath.Base(s)
	}
	return "terminal"
}

// setSize pushes the pane's interior size into the tab's component.
func (t *Tab) setSize(w, h int) {
	switch {
	case t.term != nil:
		t.term.SetSize(w, h)
	case t.inst != nil:
		t.inst.SetSize(w, h)
	default:
		t.ed.SetSize(w, h)
	}
}

// setFocused marks the tab's component focused or blurred.
func (t *Tab) setFocused(on bool) {
	switch {
	case t.term != nil:
		t.term.SetFocused(on)
	case t.inst != nil:
		t.inst.SetFocused(on)
	default:
		t.ed.SetFocused(on)
	}
}

// setPalette re-threads the active theme palette into the tab's component.
func (t *Tab) setPalette(p *theme.Palette) {
	switch {
	case t.term != nil:
		t.term.SetPalette(p)
	case t.inst != nil:
		t.inst.setPalette(p)
	default:
		t.ed.SetPalette(p)
	}
}

// configure re-applies configuration; terminals carry no live config.
func (t *Tab) configure(cfg host.Config) {
	switch {
	case t.ed != nil:
		t.ed.Configure(cfg)
	case t.term != nil:
		t.term.SetAutoSuggest(autosuggestOn(cfg))
	case t.inst != nil:
		t.inst.configure(cfg)
	}
}

// view renders the tab's component content.
func (t *Tab) view() string {
	switch {
	case t.term != nil:
		return t.term.View()
	case t.inst != nil:
		return t.inst.View()
	}
	return t.ed.View()
}

// update dispatches a message to the tab's component. Terminals only consume
// key presses (their output arrives via session messages, not Update).
func (t *Tab) update(msg tea.Msg) tea.Cmd {
	switch {
	case t.term != nil:
		if k, ok := msg.(tea.KeyPressMsg); ok {
			return t.term.Update(k)
		}
		return nil
	case t.inst != nil:
		return t.inst.Update(msg)
	}
	var cmd tea.Cmd
	*t.ed, cmd = t.ed.Update(msg)
	return cmd
}

// close releases the tab content's background resources: a terminal tab's
// session, a content tab's backend (#1778); editor tabs have nothing to
// release.
func (t *Tab) close() {
	switch {
	case t.term != nil:
		t.term.Close()
	case t.inst != nil:
		t.inst.releaseContent()
	}
}
