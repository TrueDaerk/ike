package app

import (
	"testing"

	"ike/internal/pane"
	"ike/internal/terminal"
)

// termtabdrag_test.go covers #707: a terminal tab dragged out of a tab bar
// behaves like a file tab — the center of another editor pane merges it into
// that tab list, edge zones split it off as its own terminal pane — with the
// shell session moving, never restarting.

// closeTerms ends every terminal session the model still holds.
func closeTerms(m Model) {
	for _, k := range m.panes.Keys() {
		inst := m.panes.Get(k)
		if inst == nil {
			continue
		}
		if inst.Kind() == pane.KindTerminal {
			inst.Terminal().Close()
		}
		inst.CloseTerminalTabs()
	}
}

// termTabDragApp builds two editor panes (src: a.txt+b.txt, dst: c.txt) and
// opens a terminal tab in src (third segment of its tab bar). It returns the
// model, both pane keys and the terminal's session key.
func termTabDragApp(t *testing.T) (Model, string, string, string) {
	t.Helper()
	m, _, src, dst := splitTabApp(t)
	m.setFocus(src)
	m = dispatch(t, m, TerminalNewTabMsg{})
	inst := m.panes.Get(src)
	if inst.TabCount() != 3 || inst.Tab(2) == nil || !inst.Tab(2).IsTerminal() {
		t.Fatalf("setup: src should hold a.txt, b.txt and a terminal tab, got %d tabs", inst.TabCount())
	}
	sess := inst.TabTerminal(2).SessionKey()
	if sess == "" {
		t.Fatal("setup: terminal tab has no session")
	}
	return m, src, dst, sess
}

// TestTerminalTabCenterDropMovesToOtherPane: releasing a terminal tab drag in
// another editor's center moves the live session into that pane's tab list.
func TestTerminalTabCenterDropMovesToOtherPane(t *testing.T) {
	m, src, dst, sess := termTabDragApp(t)
	defer func() { closeTerms(m) }()

	x, y := barCell(t, m, 17) // terminal segment of " a.txt │ b.txt │ <term> "
	m = step(m, press(x, y))
	dr := m.lay.Panes[dst]
	m = step(m, release(dr.X+dr.W/2, dr.Y+dr.H/2))

	if got := m.panes.Get(src).TabCount(); got != 2 {
		t.Fatalf("src should be down to its 2 file tabs, got %d", got)
	}
	dinst := m.panes.Get(dst)
	if dinst.TabCount() != 2 {
		t.Fatalf("dst should hold c.txt plus the terminal tab, got %d", dinst.TabCount())
	}
	term := dinst.TabTerminal(dinst.ActiveTab())
	if term == nil {
		t.Fatal("dst's active tab should host the moved terminal")
	}
	if got := term.SessionKey(); got != sess {
		t.Fatalf("the shell session must move, not restart: key %q want %q", got, sess)
	}
	if m.panes.Focused() != dst {
		t.Fatalf("focus should land on the adopting pane, got %q", m.panes.Focused())
	}
}

// TestTerminalTabEdgeDropSplitsOwnPane: an edge-zone release on another pane
// splits the terminal tab off as its own terminal pane next to it.
func TestTerminalTabEdgeDropSplitsOwnPane(t *testing.T) {
	m, src, dst, sess := termTabDragApp(t)
	defer func() { closeTerms(m) }()
	leavesBefore := len(m.lay.Panes)

	x, y := barCell(t, m, 17)
	m = step(m, press(x, y))
	dr := m.lay.Panes[dst]
	m = step(m, release(dr.X+dr.W-1, dr.Y+dr.H/2)) // right edge of dst

	if got := len(m.lay.Panes); got != leavesBefore+1 {
		t.Fatalf("edge drop should add a pane: %d want %d", got, leavesBefore+1)
	}
	if got := m.panes.Get(src).TabCount(); got != 2 {
		t.Fatalf("src should be down to its 2 file tabs, got %d", got)
	}
	if got := m.panes.Get(dst).TabCount(); got != 1 {
		t.Fatalf("dst's tab list must stay untouched, got %d", got)
	}
	finst := m.panes.FocusedInstance()
	if finst == nil || finst.Kind() != pane.KindTerminal {
		t.Fatal("the fresh split should be a focused terminal pane")
	}
	if got := finst.Terminal().SessionKey(); got != sess {
		t.Fatalf("the shell session must move, not restart: key %q want %q", got, sess)
	}
}

// TestTerminalTabSelfEdgeSplit: a drop on the source pane's own edge splits
// the terminal off right there, mirroring the file-tab self-edge rule.
func TestTerminalTabSelfEdgeSplit(t *testing.T) {
	m, src, _, sess := termTabDragApp(t)
	defer func() { closeTerms(m) }()

	x, y := barCell(t, m, 17)
	m = step(m, press(x, y))
	sr := m.lay.Panes[src]
	m = step(m, release(sr.X+sr.W/2, sr.Y+sr.H-1)) // own bottom edge

	if got := m.panes.Get(src).TabCount(); got != 2 {
		t.Fatalf("src should be down to its 2 file tabs, got %d", got)
	}
	finst := m.panes.FocusedInstance()
	if finst == nil || finst.Kind() != pane.KindTerminal {
		t.Fatal("the self-edge drop should split off a focused terminal pane")
	}
	if got := finst.Terminal().SessionKey(); got != sess {
		t.Fatalf("the shell session must move, not restart: key %q want %q", got, sess)
	}
}

// TestExitedSessionClosesSplitTerminalPane: a split-off terminal pane keeps
// its original session key under a fresh pane key; the shell's exit must
// still close the pane (terminalPaneForSession routing).
func TestExitedSessionClosesSplitTerminalPane(t *testing.T) {
	m, _, dst, sess := termTabDragApp(t)
	defer func() { closeTerms(m) }()

	x, y := barCell(t, m, 17)
	m = step(m, press(x, y))
	dr := m.lay.Panes[dst]
	m = step(m, release(dr.X+dr.W-1, dr.Y+dr.H/2))
	paneKey := m.panes.Focused()
	if paneKey == sess {
		t.Fatal("setup: the split pane should carry a fresh key, not the session key")
	}

	m = dispatch(t, m, terminal.ExitedMsg{Key: sess})
	if _, ok := m.lay.Panes[paneKey]; ok {
		t.Fatal("the split terminal pane should close when its shell exits")
	}
}
