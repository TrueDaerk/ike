package explorer

// matchstep_test.go covers #2410 for the explorer's speed search: cmd+g and
// cmd+shift+g step the matches while the field keeps the keyboard and the
// query survives, and the step that comes back around says so.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// nextChord / prevChord are the match-step chords as key events.
func nextChord() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'g', Mod: tea.ModMeta} }
func prevChord() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'g', Mod: tea.ModMeta | tea.ModShift}
}

// TestSpeedSearchMatchStepChord: the chord steps like ctrl+n / ctrl+p, and
// the field never loses the keyboard or the query.
func TestSpeedSearchMatchStepChord(t *testing.T) {
	m := searchModel(t, "note1.txt", "note2.txt", "other.md")
	m = typeText(m, "/note")
	first := m.cursor
	m = pressKey(m, nextChord())
	if got := rowName(m, m.cursor); got != "note2.txt" {
		t.Fatalf("cmd+g must step to note2.txt, got %q", got)
	}
	if !m.Searching() {
		t.Fatal("the speed search must keep the keyboard across a step")
	}
	if m.search.Text != "note" {
		t.Fatalf("the query must survive the step, got %q", m.search.Text)
	}
	m = pressKey(m, prevChord())
	if m.cursor != first {
		t.Fatalf("cmd+shift+g must step back to the first match, cursor = %d", m.cursor)
	}
}

// TestSpeedSearchMatchStepWraps: past the last match the walk comes back
// around, and the footer counter says so.
func TestSpeedSearchMatchStepWraps(t *testing.T) {
	m := searchModel(t, "note1.txt", "note2.txt", "other.md")
	m = typeText(m, "/note")
	first := m.cursor
	m = pressKey(m, nextChord()) // note2
	m = pressKey(m, nextChord()) // wraps to note1
	if m.cursor != first {
		t.Fatalf("the walk must wrap to the first match, cursor = %d", m.cursor)
	}
	if !m.search.Wrapped {
		t.Fatal("the wrap must be recorded")
	}
	if got := m.searchLine(); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the search line must mark the wrap: %q", got)
	}
}

// TestSpeedSearchMatchStepWithNoMatches: the chord is a no-op the pane owns,
// and the field keeps saying "no matches".
func TestSpeedSearchMatchStepWithNoMatches(t *testing.T) {
	m := searchModel(t, "note1.txt")
	m = typeText(m, "/zzz")
	at := m.cursor
	st := m.NextMatch()
	if !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v", st)
	}
	if m.cursor != at {
		t.Fatal("a step with no matches must not move the cursor")
	}
}

// TestSpeedSearchMatchStepWithoutTheField: with the search closed the chord
// is not the explorer's.
func TestSpeedSearchMatchStepWithoutTheField(t *testing.T) {
	m := searchModel(t, "note1.txt")
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with no search open = %+v, want not handled", st)
	}
}

// TestSpeedSearchEditDropsTheWrapMarker: the marker describes one step.
func TestSpeedSearchEditDropsTheWrapMarker(t *testing.T) {
	m := searchModel(t, "note1.txt", "note2.txt")
	m = typeText(m, "/note")
	m = pressKey(m, nextChord())
	m = pressKey(m, nextChord()) // wraps
	m = typeText(m, "1")
	if m.search.Wrapped {
		t.Fatal("an edited query must start a fresh walk")
	}
}
