package dataview

import (
	"strings"
	"testing"

	_ "ike/plugins/languages/sql"
)

// matchstep_test.go covers #2410 for the data grid. The grid's search is a
// filter, so its "matches" are the rows it left standing: cmd+g walks the
// loaded page while the filter line keeps the keyboard and the clause stays
// editable, and enter still applies and closes the line as it always did.

func TestGridMatchStepWalksTheRowsWithTheLineOpen(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	feed(t, &m, key("/"))
	typeInto(t, &m, "id > 0")
	rows := m.PageRows()
	if rows < 2 {
		t.Fatalf("the fixture needs several rows, got %d", rows)
	}
	before := m.Cursor()
	st := m.NextMatch()
	if !st.Handled || st.Total != rows || st.Wrapped {
		t.Fatalf("NextMatch = %+v, rows = %d", st, rows)
	}
	if m.Cursor() == before {
		t.Fatal("NextMatch did not move the row cursor")
	}
	if !m.Filtering() || m.FilterInput() != "id > 0" {
		t.Fatalf("the filter line must survive the step: open=%v input=%q",
			m.Filtering(), m.FilterInput())
	}
}

func TestGridMatchStepWrapsAndSaysSo(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	feed(t, &m, key("/"))
	st := m.PrevMatch() // back off the first row
	if !st.Wrapped || st.Index != st.Total {
		t.Fatalf("the backward wrap = %+v", st)
	}
	if got := stripANSI(m.View()); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the filter footer must mark the wrap:\n%s", got)
	}
}

func TestGridMatchStepWithoutTheFilterLine(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with the line closed = %+v, want not handled", st)
	}
}

// TestGridEnterStillApplies guards the acceptance criterion that enter keeps
// its meaning next to the new chord.
func TestGridEnterStillApplies(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	feed(t, &m, key("/"))
	typeInto(t, &m, "id > 1100")
	m.NextMatch()
	feed(t, &m, key("enter"))
	if m.Filtering() {
		t.Fatal("enter must close the filter line")
	}
	if !strings.Contains(m.Filter(), "id > 1100") {
		t.Fatalf("enter must apply the clause, filter = %q", m.Filter())
	}
}
