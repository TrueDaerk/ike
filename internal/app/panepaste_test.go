package app

// panepaste_test.go guards the #2002 paste routing for the tool-window panes
// that own a text input of their own. Before the sweep handlePaste stopped at
// the editor/terminal/HTTP panes, so cmd+v into the issues filter or the DOM
// selector did nothing at all.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// focusPane makes the pane under key the focused one.
func focusPane(t *testing.T, m Model, key string) Model {
	t.Helper()
	if !m.activeWS().Panes.Has(key) {
		t.Fatalf("pane %q was not created", key)
	}
	m.activeWS().Panes.SetFocused(key)
	return m
}

func slash() tea.KeyPressMsg { return tea.KeyPressMsg{Code: '/', Text: "/"} }

func TestPasteReachesIssuesFilter(t *testing.T) {
	m := newSized()
	panes := m.activeWS().Panes
	k := panes.AddIssues()
	m = focusPane(t, m, k)
	gi := panes.Get(k).Issues()
	gi.Update(slash())
	if !gi.Filtering() {
		t.Fatal("'/' must open the issues filter")
	}
	tm, _ := m.handlePaste("bug")
	m = tm.(Model)
	if !strings.Contains(gi.View(), "bug") {
		t.Fatalf("issues filter did not take the paste:\n%s", gi.View())
	}
}

func TestPasteReachesDOMSelector(t *testing.T) {
	m := newSized()
	panes := m.activeWS().Panes
	k := panes.AddDOM()
	m = focusPane(t, m, k)
	dm := panes.Get(k).DOM()
	dm.Update(slash())
	if !dm.Editing() {
		t.Fatal("'/' must open the DOM selector input")
	}
	tm, _ := m.handlePaste("div.main")
	m = tm.(Model)
	if !strings.Contains(dm.View(), "div.main") {
		t.Fatalf("DOM selector did not take the paste:\n%s", dm.View())
	}
}

// A closed input drops the paste instead of letting it leak elsewhere.
func TestPasteDroppedByClosedPaneInput(t *testing.T) {
	m := newSized()
	panes := m.activeWS().Panes
	k := panes.AddDOM()
	m = focusPane(t, m, k)
	dm := panes.Get(k).DOM()
	tm, cmd := m.handlePaste("div.main")
	m = tm.(Model)
	if cmd != nil {
		t.Fatal("a dropped paste must not produce a command")
	}
	if strings.Contains(dm.View(), "div.main") {
		t.Fatalf("a closed selector must not take the paste:\n%s", dm.View())
	}
}
