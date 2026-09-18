package app

// panenav_test.go covers #2634 at the root model: the caret chords a one-line
// input owns — cmd+left / cmd+right to the ends of the text, ctrl+left /
// ctrl+right by words — reach a *tool pane's* input and are not logged as
// unbound keybindings on the way. Before it the field answered them (it always
// has, ui.EditKey) but the keymap layer recorded its "no binding" verdict
// first, and the telemetry filled with keybinds that were never missing:
// ctrl+left in issues and problems, ctrl+right in the terminal.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// navChord builds one of the caret chords as a key event.
func navChord(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// TestProblemsFilterTakesTheCaretChords is the headline case: with the
// problems filter row focused, cmd+left puts the caret at the start of the
// expression (the next typed rune lands there), cmd+right brings it back to
// the end, and ctrl+left jumps a word.
func TestProblemsFilterTakesTheCaretChords(t *testing.T) {
	m := findApp(t)
	tm, cmd := m.Update(ProblemsToggleMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.activeWS().Panes.Get(pane.ProblemsKey) == nil {
		t.Skip("the problems pane did not open in this environment")
	}
	m.setFocus(pane.ProblemsKey)
	m = drainKey(m, findChord())
	m = typeInto(m, "sev err")

	filterText := func(m Model) string {
		return m.activeWS().Panes.Get(pane.ProblemsKey).Problems().Filter().Text()
	}
	if got := filterText(m); got != "sev err" {
		t.Fatalf("setup: filter = %q", got)
	}

	m = drainKey(m, navChord(tea.KeyLeft, tea.ModSuper))
	m = typeInto(m, "X")
	if got := filterText(m); got != "Xsev err" {
		t.Fatalf("cmd+left must move the caret to the start, got %q", got)
	}
	m = drainKey(m, navChord(tea.KeyRight, tea.ModSuper))
	m = typeInto(m, "Y")
	if got := filterText(m); got != "Xsev errY" {
		t.Fatalf("cmd+right must move the caret to the end, got %q", got)
	}
	// ctrl+left steps over the trailing word, so the next rune lands in front
	// of it rather than one cell to the left.
	m = drainKey(m, navChord(tea.KeyLeft, tea.ModCtrl))
	m = typeInto(m, "Z")
	if got := filterText(m); got != "Xsev ZerrY" {
		t.Fatalf("ctrl+left must jump a word, got %q", got)
	}
	if !m.activeWS().Panes.Get(pane.ProblemsKey).Problems().Filtering() {
		t.Fatal("the chords must leave the filter row focused")
	}
	if u := unboundChords(t, m); len(u) != 0 {
		t.Fatalf("a consumed caret chord must not be recorded unbound, got %v", u)
	}
}

// TestIssuesFilterTakesTheCaretChords: the same in the issues pane's filter
// overlay, the surface the telemetry recorded ctrl+left against.
func TestIssuesFilterTakesTheCaretChords(t *testing.T) {
	m := findApp(t)
	tm, cmd := m.Update(IssuesToggleMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.activeWS().Panes.Get(pane.IssuesKey) == nil {
		t.Skip("the issues pane did not open in this environment")
	}
	m.setFocus(pane.IssuesKey)
	m = drainKey(m, findChord())
	gi := func(m Model) string { return m.activeWS().Panes.Get(pane.IssuesKey).Issues().Filter() }
	if !m.activeWS().Panes.Get(pane.IssuesKey).Issues().Filtering() {
		t.Fatal("setup: the find chord must open the match row")
	}
	m = typeInto(m, "one two")
	if got := gi(m); got != "one two" {
		t.Fatalf("setup: match text = %q", got)
	}

	m = drainKey(m, navChord(tea.KeyLeft, tea.ModSuper))
	m = typeInto(m, "X")
	if got := gi(m); got != "Xone two" {
		t.Fatalf("cmd+left must move the caret to the start, got %q", got)
	}
	m = drainKey(m, navChord(tea.KeyRight, tea.ModCtrl))
	m = typeInto(m, "Y")
	if got := gi(m); got != "XoneY two" {
		t.Fatalf("ctrl+right must jump a word, got %q", got)
	}
	if u := unboundChords(t, m); len(u) != 0 {
		t.Fatalf("a consumed caret chord must not be recorded unbound, got %v", u)
	}
}

// TestTerminalSearchTakesTheCaretChords: the scrollback search field is the
// third host the telemetry named (ctrl+right there).
func TestTerminalSearchTakesTheCaretChords(t *testing.T) {
	m, key := openTestTerminal(t)
	// The first-start LSP dialog (#301) owns the keyboard ahead of everything
	// else, so a scripted key would never reach the pane behind it.
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	handled, out, _ := m.terminalReservedKey("cmd+f")
	if !handled {
		t.Fatal("setup: cmd+f must be reserved while a terminal is focused")
	}
	m = out.(Model)
	if !m.activeWS().Panes.Get(key).Terminal().Searching() {
		t.Fatal("setup: cmd+f must open the scrollback search")
	}
	m = typeInto(m, "one two")
	query := func() string { q, _ := m.activeWS().Panes.Get(key).Terminal().SearchQuery(); return q }
	if got := query(); got != "one two" {
		t.Fatalf("setup: query = %q", got)
	}

	m = drainKey(m, navChord(tea.KeyLeft, tea.ModSuper))
	m = typeInto(m, "X")
	if got := query(); got != "Xone two" {
		t.Fatalf("cmd+left must move the caret to the start, got %q", got)
	}
	m = drainKey(m, navChord(tea.KeyRight, tea.ModCtrl))
	m = typeInto(m, "Y")
	if got := query(); got != "XoneY two" {
		t.Fatalf("ctrl+right must jump a word, got %q", got)
	}
	if !m.activeWS().Panes.Get(key).Terminal().Searching() {
		t.Fatal("the chords must leave the search field open")
	}
	if u := unboundChords(t, m); len(u) != 0 {
		t.Fatalf("a consumed caret chord must not be recorded unbound, got %v", u)
	}
}

// TestTerminalFocusMovesSurviveTheSearchField: the claim is narrow. ctrl+up
// and ctrl+down are the spatial focus moves (#228) and stay them even with
// the scrollback search open, so the way out of a terminal is never lost —
// only the two chords a caret actually uses are taken.
func TestTerminalFocusMovesSurviveTheSearchField(t *testing.T) {
	m, key := openTestTerminal(t)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	_, out, _ := m.terminalReservedKey("cmd+f")
	m = out.(Model)
	if !m.activeWS().Panes.Get(key).Terminal().Searching() {
		t.Fatal("setup: cmd+f must open the scrollback search")
	}
	m = typeInto(m, "one")
	m = drainKey(m, navChord(tea.KeyUp, tea.ModCtrl))
	if q, _ := m.activeWS().Panes.Get(key).Terminal().SearchQuery(); q != "one" {
		t.Fatalf("ctrl+up must not edit the query, got %q", q)
	}
}

// TestCaretChordsStayCommandsWithoutAnInput: the claim is gated on an open
// input. With the problems list focused and the filter row blurred, cmd+left
// is not the pane's — it falls through to the keymap layer unchanged, so a
// future binding on it still resolves.
func TestCaretChordsStayCommandsWithoutAnInput(t *testing.T) {
	m := findApp(t)
	tm, cmd := m.Update(ProblemsToggleMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.activeWS().Panes.Get(pane.ProblemsKey) == nil {
		t.Skip("the problems pane did not open in this environment")
	}
	m.setFocus(pane.ProblemsKey)
	if m.lineInputFocused() {
		t.Fatal("an unfocused filter row is no open input")
	}
	m = drainKey(m, findChord())
	if !m.lineInputFocused() {
		t.Fatal("the focused filter row must count as an open input")
	}
}

// TestTabChordsSurviveAnOpenInput: the two-modifier arrows are commands, not
// caret chords — ctrl+alt+right walks the editor tabs and must keep doing so
// while a pane's filter row holds the keyboard.
func TestTabChordsSurviveAnOpenInput(t *testing.T) {
	m := findApp(t)
	tm, cmd := m.Update(ProblemsToggleMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.activeWS().Panes.Get(pane.ProblemsKey) == nil {
		t.Skip("the problems pane did not open in this environment")
	}
	m.setFocus(pane.ProblemsKey)
	m = drainKey(m, findChord())
	m = typeInto(m, "sev err")
	m = drainKey(m, navChord(tea.KeyRight, tea.ModCtrl|tea.ModAlt))
	if got := m.activeWS().Panes.Get(pane.ProblemsKey).Problems().Filter().Text(); got != "sev err" {
		t.Fatalf("ctrl+alt+right must not edit the filter, got %q", got)
	}
}
