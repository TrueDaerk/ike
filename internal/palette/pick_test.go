package palette

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pick_test.go covers the pick record the host pulls after an activation
// (#2551): the counterpart of the dismissal record.

func pickPalette(t *testing.T) *Palette {
	t.Helper()
	p := New(Config{DefaultPrefix: '&'}, stubMode{prefix: '&', items: []Item{
		{Title: "one", Msg: primaryMsg{"one"}},
		{Title: "two", Msg: primaryMsg{"two"}},
		{Title: "three", Msg: primaryMsg{"three"}},
	}})
	p.SetSize(100, 40)
	p.OpenLocked(Context{}, '&')
	return p
}

// A pick records the mode, the query length and the chosen row's 0-based rank
// out of the rows listed — and the record is taken exactly once.
func TestTakePickReportsRank(t *testing.T) {
	p := pickPalette(t)
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	rank := p.selected
	results := len(p.items)
	if results < 2 {
		t.Fatalf("the fixture needs at least two rows, got %d", results)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	got, ok := p.TakePick()
	if !ok {
		t.Fatal("an activation must leave a pick to take")
	}
	if got.Rank != rank || got.Results != results {
		t.Fatalf("pick = %+v, want rank %d of %d", got, rank, results)
	}
	if _, ok := p.TakePick(); ok {
		t.Fatal("the pick must be taken exactly once")
	}
}

// Esc is not a pick: the dismissal record and the pick record stay disjoint.
func TestDismissLeavesNoPick(t *testing.T) {
	p := pickPalette(t)
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := p.TakePick(); ok {
		t.Fatal("a dismissal recorded a pick")
	}
	if _, ok := p.TakeDismissal(); !ok {
		t.Fatal("a dismissal must still be taken")
	}
}
