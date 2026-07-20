package app

import (
	"strings"
	"testing"

	"ike/internal/pane"
)

// termadopt_test.go covers #708: a terminal pane dragged by its title bar and
// released on an editor pane's center zone moves its live shell session into
// the target's tab list as a terminal tab; edge drops keep the whole-pane
// relocate semantics.

// terminalDragApp opens a terminal pane below the editor and returns the model
// plus the editor and terminal pane keys.
func terminalDragApp(t *testing.T) (Model, string, string) {
	t.Helper()
	m, termKey := openTestTerminal(t)
	edKey := ""
	for _, k := range m.panes.Keys() {
		if inst := m.panes.Get(k); inst != nil && inst.Kind() == pane.KindEditor {
			edKey = k
			break
		}
	}
	if edKey == "" {
		t.Fatal("setup: no editor pane found")
	}
	return m, edKey, termKey
}

// TestTerminalPaneCenterDropBecomesTab: releasing a terminal pane's title drag
// in an editor's center adopts the running session as a terminal tab and
// closes the vacated terminal pane.
func TestTerminalPaneCenterDropBecomesTab(t *testing.T) {
	m, edKey, termKey := terminalDragApp(t)
	sess := m.panes.Get(termKey).Terminal().SessionKey()
	if sess == "" {
		t.Fatal("setup: terminal has no session")
	}

	tr := m.lay.Panes[termKey]
	er := m.lay.Panes[edKey]
	m = step(m, press(tr.X+2, tr.Y+1)) // grab the terminal pane title text row (top border is the resize band, #761)
	m = step(m, release(er.X+er.W/2, er.Y+er.H/2))

	if _, ok := m.lay.Panes[termKey]; ok {
		t.Fatal("terminal pane should close after the center drop")
	}
	einst := m.panes.Get(edKey)
	if einst == nil || einst.TabCount() != 2 {
		t.Fatalf("editor should hold its file tab plus the terminal tab, got %d tabs", einst.TabCount())
	}
	term := einst.TabTerminal(einst.ActiveTab())
	if term == nil {
		t.Fatal("the active tab should host the adopted terminal")
	}
	if got := term.SessionKey(); got != sess {
		t.Fatalf("the shell session must move, not restart: key %q want %q", got, sess)
	}
	if m.panes.Focused() != edKey {
		t.Fatalf("focus should land on the adopting pane, got %q", m.panes.Focused())
	}
	t.Cleanup(einst.CloseTerminalTabs)
}

// TestTerminalPaneEdgeDropStillRelocates: an edge-zone release keeps today's
// whole-pane relocate behavior and leaves the target's tab list untouched.
func TestTerminalPaneEdgeDropStillRelocates(t *testing.T) {
	m, edKey, termKey := terminalDragApp(t)
	tr := m.lay.Panes[termKey]
	er := m.lay.Panes[edKey]
	m = step(m, press(tr.X+2, tr.Y+1))
	m = step(m, release(er.X+er.W-1, er.Y+er.H/2)) // right edge zone

	if _, ok := m.lay.Panes[termKey]; !ok {
		t.Fatal("edge drop must relocate, not dissolve, the terminal pane")
	}
	if got := m.panes.Get(edKey).TabCount(); got != 1 {
		t.Fatalf("edge drop must not add tabs to the target, got %d", got)
	}
	if inst := m.panes.Get(termKey); inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("relocated pane should still be the terminal")
	}
}

// TestTerminalPaneCenterHoverShowsMerge: hovering an editor's center during a
// terminal pane drag shows the merge marker like a file-carrying drag (#318).
func TestTerminalPaneCenterHoverShowsMerge(t *testing.T) {
	m, edKey, termKey := terminalDragApp(t)
	tr := m.lay.Panes[termKey]
	er := m.lay.Panes[edKey]
	m = step(m, press(tr.X+2, tr.Y+1))
	m = step(m, motion(er.X+er.W/2, er.Y+er.H/2))

	if view := m.render(); !strings.Contains(view, "⧉ merge as tab") {
		t.Fatalf("center hover missing the merge marker:\n%s", view)
	}
}
