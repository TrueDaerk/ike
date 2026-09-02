package todoindex

// matchstep_test.go covers #2410 for the TODO index overlay. It owns the
// keyboard ahead of the keymap layer, so it answers cmd+g / cmd+shift+g
// itself: the walk steps the narrowed list while the filter row keeps the
// keyboard and the expression stays editable.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// stepChord is the match-step chord as a key event; back flips the direction.
func stepChord(back bool) tea.KeyPressMsg {
	mod := tea.ModMeta
	if back {
		mod |= tea.ModShift
	}
	return tea.KeyPressMsg{Code: 'g', Mod: mod}
}

func TestTodoIndexMatchStepChordWalksTheList(t *testing.T) {
	m, _ := filterModel(t)
	setFilter(t, m, "tag:TODO")
	m.filter.Focus()
	if m.list.Total() != 2 {
		t.Fatalf("the filter must leave two entries, got %d", m.list.Total())
	}
	m.Update(stepChord(false))
	if m.list.Cursor() != 1 {
		t.Fatalf("the chord must step the list, cursor = %d", m.list.Cursor())
	}
	if !m.filter.Active() || m.filter.Text() != "tag:TODO" {
		t.Fatalf("the filter row must survive the step: active=%v text=%q",
			m.filter.Active(), m.filter.Text())
	}
	if got := m.filter.Status(); got != "2/2" {
		t.Fatalf("the filter row counter = %q, want 2/2", got)
	}
}

func TestTodoIndexMatchStepWrapsAndSaysSo(t *testing.T) {
	m, _ := filterModel(t)
	setFilter(t, m, "tag:TODO")
	m.filter.Focus()
	m.Update(stepChord(false)) // 2/2
	m.Update(stepChord(false)) // wraps to 1/2
	if m.list.Cursor() != 0 {
		t.Fatalf("the walk must wrap, cursor = %d", m.list.Cursor())
	}
	if got := m.filter.Status(); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the counter must mark the wrap: %q", got)
	}
}

// TestTodoIndexMatchStepBeatsTheFilterKeys: "g" is filter text, the chord is
// not — the row must not eat it.
func TestTodoIndexMatchStepBeatsTheFilterKeys(t *testing.T) {
	m, _ := filterModel(t)
	m.filter.Focus()
	m.Update(stepChord(false))
	if m.filter.Text() != "" {
		t.Fatalf("the chord must not type into the filter, text = %q", m.filter.Text())
	}
}

func TestTodoIndexMatchStepWithNoMatches(t *testing.T) {
	m, _ := filterModel(t)
	setFilter(t, m, "tag:TODO file:*.nope")
	m.filter.Focus()
	st := m.stepMatch(1)
	if !st.Handled || st.Total != 0 {
		t.Fatalf("a step over nothing = %+v", st)
	}
	if m.filter.Status() != "" {
		t.Fatalf("no counter is shown with no matches, got %q", m.filter.Status())
	}
}
