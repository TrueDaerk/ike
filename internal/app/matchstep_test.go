package app

import (
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// matchstep_test.go covers #2410 at the root model: search.nextMatch /
// search.prevMatch reach the focused pane's open search first, and only fall
// back to the older readings (repeat the editor's find, walk the retained
// find-in-path results) when no pane search is open.

// stepChord is the app keymap's search.nextMatch chord as a key event: cmd+g
// on macOS, folded to ctrl+g everywhere else (keymap.NormalizeKey). back
// gives the search.prevMatch twin.
func stepChord(back bool) tea.KeyPressMsg {
	mod := tea.ModMeta
	if runtime.GOOS != "darwin" {
		mod = tea.ModCtrl
	}
	if back {
		mod |= tea.ModShift
	}
	return tea.KeyPressMsg{Code: 'g', Mod: mod}
}

// TestMatchStepChordStepsTheExplorerSpeedSearch is the issue's headline case:
// the chord that steps matches in the editor steps them in the explorer's
// speed search too, without the field losing focus.
func TestMatchStepChordStepsTheExplorerSpeedSearch(t *testing.T) {
	m := findApp(t)
	m.setFocus(pane.ExplorerKey)
	m = drainKey(m, findChord())
	if !m.explorer().Searching() {
		t.Fatal("the find chord must open the speed search first")
	}
	m = drainKey(m, stepChord(false))
	if !m.explorer().Searching() {
		t.Fatal("the match-step chord must leave the speed search open")
	}
}

// TestMatchStepChordStepsAToolWindowFilter: the registry panes get the chord
// through the Global search.nextMatch command and the pane.Searchable seam.
func TestMatchStepChordStepsAToolWindowFilter(t *testing.T) {
	m := findApp(t)
	tm, cmd := m.Update(ProblemsToggleMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.activeWS().Panes.Get(pane.ProblemsKey) == nil {
		t.Skip("the problems pane did not open in this environment")
	}
	m.setFocus(pane.ProblemsKey)
	m = drainKey(m, findChord())
	pp := m.activeWS().Panes.Get(pane.ProblemsKey).Problems()
	if !pp.Filtering() {
		t.Fatal("the find chord must focus the filter row first")
	}
	m = drainKey(m, stepChord(false))
	if !m.activeWS().Panes.Get(pane.ProblemsKey).Problems().Filtering() {
		t.Fatal("the match-step chord must leave the filter row focused")
	}
}

// TestMatchStepWithNoMatchesNotifies: the chord is a no-op with a short hint
// rather than a silent one, which reads as a broken binding.
func TestMatchStepWithNoMatchesNotifies(t *testing.T) {
	m := findApp(t)
	tm, cmd := m.Update(ProblemsToggleMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.activeWS().Panes.Get(pane.ProblemsKey) == nil {
		t.Skip("the problems pane did not open in this environment")
	}
	m.setFocus(pane.ProblemsKey)
	m.activeWS().Panes.Get(pane.ProblemsKey).Problems().OpenSearch()
	if _, handled := m.stepPaneMatch(1); !handled {
		t.Fatal("an open filter row must claim the chord")
	}
	// Feed the queued notification through one Update pass, which is where
	// the root model drains the host's queue into a toast.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if !hasNotice(m, noMatchesNotice) {
		t.Fatalf("expected the no-matches hint, toasts = %v", noticeTexts(m))
	}
}

// TestMatchStepWithoutAPaneSearchFallsThrough: with no pane search open the
// chord keeps its find-in-path meaning, so the pane layer must not claim it.
func TestMatchStepWithoutAPaneSearchFallsThrough(t *testing.T) {
	m := findApp(t)
	m.setFocus(pane.ExplorerKey)
	if _, handled := m.stepPaneMatch(1); handled {
		t.Fatal("a closed speed search must leave the chord to find-in-path")
	}
}
