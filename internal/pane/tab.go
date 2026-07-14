package pane

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/terminal"
	"ike/internal/theme"
)

// Tab is one slot in an editor pane's tab strip (0350, #573): either a
// document editor or an embedded terminal, so run output can live in a tab
// next to the files it belongs to. Exactly one of ed/term is non-nil.
type Tab struct {
	ed   *editor.Model
	term *terminal.Model
}

// newEditorTab wraps an editor model as a tab slot.
func newEditorTab(ed *editor.Model) *Tab { return &Tab{ed: ed} }

// newTerminalTab wraps a terminal model as a tab slot.
func newTerminalTab(t *terminal.Model) *Tab { return &Tab{term: t} }

// IsTerminal reports whether the tab hosts a terminal rather than a document.
func (t *Tab) IsTerminal() bool { return t.term != nil }

// Editor returns the tab's editor model, nil for a terminal tab.
func (t *Tab) Editor() *editor.Model { return t.ed }

// Terminal returns the tab's terminal model, nil for an editor tab.
func (t *Tab) Terminal() *terminal.Model { return t.term }

// Title returns a terminal tab's display label: the application-set OSC title
// when present, else the shell binary's base name. Editor tabs are labelled by
// the caller (basename + markers), which needs the whole tab list for
// disambiguation.
func (t *Tab) Title() string {
	if t.term == nil {
		return ""
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
	if t.term != nil {
		t.term.SetSize(w, h)
		return
	}
	t.ed.SetSize(w, h)
}

// setFocused marks the tab's component focused or blurred.
func (t *Tab) setFocused(on bool) {
	if t.term != nil {
		t.term.SetFocused(on)
		return
	}
	t.ed.SetFocused(on)
}

// setPalette re-threads the active theme palette into the tab's component.
func (t *Tab) setPalette(p *theme.Palette) {
	if t.term != nil {
		t.term.SetPalette(p)
		return
	}
	t.ed.SetPalette(p)
}

// configure re-applies configuration; terminals carry no live config.
func (t *Tab) configure(cfg host.Config) {
	if t.ed != nil {
		t.ed.Configure(cfg)
	}
}

// view renders the tab's component content.
func (t *Tab) view() string {
	if t.term != nil {
		return t.term.View()
	}
	return t.ed.View()
}

// update dispatches a message to the tab's component. Terminals only consume
// key presses (their output arrives via session messages, not Update).
func (t *Tab) update(msg tea.Msg) tea.Cmd {
	if t.term != nil {
		if k, ok := msg.(tea.KeyPressMsg); ok {
			return t.term.Update(k)
		}
		return nil
	}
	var cmd tea.Cmd
	*t.ed, cmd = t.ed.Update(msg)
	return cmd
}

// close ends a terminal tab's session; editor tabs have nothing to release.
func (t *Tab) close() {
	if t.term != nil {
		t.term.Close()
	}
}
