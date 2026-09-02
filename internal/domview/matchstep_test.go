package domview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// matchstep_test.go covers #2410 for the DOM inspector: cmd+g steps the
// selector's matches while the selector line keeps the keyboard, so a
// selector can be walked without enter closing the input first.

// typeSelector opens the selector line and types sel into it.
func typeSelector(m *Model, sel string) {
	press(m, "/")
	for _, r := range sel {
		press(m, string(r))
	}
}

func TestDOMNextMatchKeepsTheSelectorFocused(t *testing.T) {
	m := loaded(t)
	typeSelector(&m, "li.item")
	if !m.Editing() || len(m.Matches()) != 2 {
		t.Fatalf("editing=%v matches=%d", m.Editing(), len(m.Matches()))
	}
	st := m.NextMatch()
	if !st.Handled || st.Index != 2 || st.Total != 2 || st.Wrapped {
		t.Fatalf("NextMatch = %+v", st)
	}
	if !m.Editing() || m.Selector() != "li.item" {
		t.Fatalf("the selector line must survive the step: editing=%v selector=%q",
			m.Editing(), m.Selector())
	}
}

func TestDOMMatchStepWrapsAndSaysSo(t *testing.T) {
	m := loaded(t)
	typeSelector(&m, "li.item")
	m.NextMatch()       // 2/2
	st := m.NextMatch() // wraps to 1/2
	if !st.Wrapped || st.Index != 1 {
		t.Fatalf("the wrap = %+v", st)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "1/2 matches (wrapped)") {
		t.Fatalf("the selector line must mark the wrap:\n%s", view)
	}
}

// TestDOMMatchStepAfterEnter: the applied selector is still the pane's
// search, so the chord keeps stepping it once the line has closed.
func TestDOMMatchStepAfterEnter(t *testing.T) {
	m := loaded(t)
	typeSelector(&m, "li.item")
	press(&m, "enter")
	if m.Editing() {
		t.Fatal("enter must close the selector line")
	}
	if st := m.NextMatch(); !st.Handled || st.Total != 2 {
		t.Fatalf("NextMatch after enter = %+v", st)
	}
}

func TestDOMMatchStepWithoutASelector(t *testing.T) {
	m := loaded(t)
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with no selector = %+v, want not handled", st)
	}
}

func TestDOMMatchStepWithNoMatches(t *testing.T) {
	m := loaded(t)
	typeSelector(&m, "span")
	if st := m.NextMatch(); !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v", st)
	}
}

func TestDOMEditDropsTheWrapMarker(t *testing.T) {
	m := loaded(t)
	typeSelector(&m, "li.item")
	m.NextMatch()
	m.NextMatch() // wraps
	press(&m, "s")
	if m.matchWrapped {
		t.Fatal("an edited selector must start a fresh walk")
	}
}
