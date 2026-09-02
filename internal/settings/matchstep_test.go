package settings

// matchstep_test.go covers #2410 for the settings panel. It owns the keyboard
// ahead of the keymap layer, so it answers cmd+g / cmd+shift+g itself: the
// walk steps the search results while the filter input keeps the keyboard and
// the query stays editable.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// stepChord is the match-step chord as a key event; back flips the direction.
func stepChord(back bool) tea.KeyPressMsg {
	mod := tea.ModMeta
	if back {
		mod |= tea.ModShift
	}
	return tea.KeyPressMsg{Code: 'g', Mod: mod}
}

func TestPanelMatchStepWalksTheResults(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	typeFilter(m, "python")
	if !m.filtering {
		t.Fatal("the filter input must hold the keyboard")
	}
	rows := len(m.rows())
	if rows < 2 {
		t.Fatalf("the query needs several results, got %d", rows)
	}
	m.Update(stepChord(false))
	if m.sel != 1 {
		t.Fatalf("the chord must step the selection, sel = %d", m.sel)
	}
	if !m.filtering || m.filter != "python" {
		t.Fatalf("the input must survive the step: filtering=%v filter=%q",
			m.filtering, m.filter)
	}
}

func TestPanelMatchStepWrapsAndSaysSo(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	typeFilter(m, "python")
	rows := len(m.rows())
	m.sel = rows - 1
	m.Update(stepChord(false)) // off the end
	if m.sel != 0 {
		t.Fatalf("the walk must wrap to the first result, sel = %d", m.sel)
	}
	if !strings.Contains(m.stepStatus, "(wrapped)") {
		t.Fatalf("the counter must mark the wrap: %q", m.stepStatus)
	}
	if got := ansi.Strip(m.renderFilter()); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the filter row must render the wrap: %q", got)
	}
}

// TestPanelMatchStepIsNotTypedIntoTheFilter: the chord is not query text.
func TestPanelMatchStepIsNotTypedIntoTheFilter(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	typeFilter(m, "python")
	m.Update(stepChord(false))
	if m.filter != "python" {
		t.Fatalf("the chord must not type into the filter, got %q", m.filter)
	}
}

// TestPanelMatchStepWithoutAQuery: with nothing typed the chord is not the
// panel's, so its first-letter rail jump still gets its keys.
func TestPanelMatchStepWithoutAQuery(t *testing.T) {
	m, _ := searchModel(t)
	if st := m.stepMatch(1); st.Handled {
		t.Fatalf("stepMatch with no query = %+v, want not handled", st)
	}
}

func TestPanelMatchStepWithNoResults(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	typeFilter(m, "zzzznothinghere")
	if got := len(m.rows()); got != 0 {
		t.Fatalf("the query must match nothing, got %d rows", got)
	}
	if st := m.stepMatch(1); !st.Handled || st.Total != 0 {
		t.Fatalf("a step over nothing = %+v", st)
	}
	if m.stepStatus != "" {
		t.Fatalf("no counter is shown with no results, got %q", m.stepStatus)
	}
}

func TestPanelEditDropsTheWrapMarker(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	typeFilter(m, "python")
	m.sel = len(m.rows()) - 1
	m.Update(stepChord(false)) // wraps
	typeFilter(m, " ")
	if m.stepStatus != "" {
		t.Fatalf("an edited query must start a fresh walk, status = %q", m.stepStatus)
	}
}
