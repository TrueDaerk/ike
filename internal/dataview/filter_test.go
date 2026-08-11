package dataview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	// The clause is highlighted through the language registry, so the test
	// binary links the SQL plugin the app blank-imports in main.
	_ "ike/plugins/languages/sql"
)

// typeInto feeds a whole clause into the open filter line, one key per rune.
func typeInto(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// openGrid loads `users` and puts the cursor in the grid.
func openGrid(t *testing.T, m *Model) {
	t.Helper()
	m.Update(key("j")) // sidebar: empty -> users
	m.Update(key("enter"))
	if m.SelectedTable() != "users" {
		t.Fatalf("selected = %q, want users", m.SelectedTable())
	}
}

func TestFilterLineShowsPrefixAndClause(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	m.Update(key("/"))
	if !m.Filtering() {
		t.Fatal("'/' in the grid must open the filter line")
	}
	typeInto(&m, "WHERE id > 1100")
	if m.FilterInput() != "WHERE id > 1100" {
		t.Fatalf("input = %q", m.FilterInput())
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, `SELECT * FROM "users" WHERE id > 1100`) {
		t.Fatalf("the filter line must show the fixed prefix before the clause, view:\n%s", view)
	}
	// The grid's letter keys are text while the line is open: "n" types, it
	// does not page.
	typeInto(&m, "n")
	if m.PageOffset() != 0 || !strings.HasSuffix(m.FilterInput(), "n") {
		t.Fatalf("a letter must type into the line, not page: offset %d input %q", m.PageOffset(), m.FilterInput())
	}
}

func TestFilterLineKeepsThePaneHeight(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	closed := strings.Count(m.View(), "\n")
	m.Update(key("/"))
	if open := strings.Count(m.View(), "\n"); open != closed {
		t.Fatalf("the pane draws %d lines with the filter line open, %d without", open+1, closed+1)
	}
	// The grid gave up the row, not the pane: one body row less is visible.
	if got, want := m.bodyHeight(), 24-3; got != want {
		t.Fatalf("body height with the filter open = %d, want %d", got, want)
	}
}

func TestFilterClauseIsSQLHighlighted(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	m.Update(key("/"))
	typeInto(&m, "WHERE id > 1100")
	keyword := filterLineOf(m.View())
	if keyword == "" {
		t.Fatal("no filter line in the view")
	}
	// Highlighting rides the SQL grammar, which a CGo-free build does not
	// carry; there the clause renders plain and there is nothing to assert.
	if !sqlHighlightAvailable() {
		t.Skip("no SQL grammar in this build")
	}
	// The same clause with the keyword replaced by a plain word must render
	// differently: that difference *is* the syntax highlighting.
	m2 := newPane(t, writeFixtureDB(t))
	openGrid(t, &m2)
	m2.Update(key("/"))
	typeInto(&m2, "wheres id > 1100")
	plain := filterLineOf(m2.View())
	if stripANSI(keyword) == stripANSI(plain) {
		t.Fatal("the two clauses must differ in text")
	}
	if len(styleSet(keyword)) <= len(styleSet(plain)) {
		t.Fatalf("the keyword and the literal must take colours of their own, got %q vs %q", keyword, plain)
	}
}

// styleSet collects the distinct SGR sequences of a rendered line — a line
// whose SQL is highlighted carries more of them than a plain one.
func styleSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(s, "\x1b[")[1:] {
		if i := strings.IndexByte(part, 'm'); i >= 0 {
			out[part[:i]] = true
		}
	}
	return out
}

func TestFilterAppliesAndPagesFilteredRows(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	m.Update(key("/"))
	typeInto(&m, "WHERE id > 600 ORDER BY id")
	m.Update(key("enter"))
	if m.Filtering() {
		t.Fatal("enter must close the filter line")
	}
	if m.Filter() != "WHERE id > 600 ORDER BY id" {
		t.Fatalf("applied filter = %q", m.Filter())
	}
	if m.PageRows() != PageSize || m.PageOffset() != 0 {
		t.Fatalf("filtered page = %d rows at %d", m.PageRows(), m.PageOffset())
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "rows 1–500 of 600") {
		t.Fatalf("the status line must count the filtered result, view:\n%s", view)
	}
	if !strings.Contains(view, "filter: WHERE id > 600") {
		t.Fatalf("the active filter must be visible in the header, view:\n%s", view)
	}
	// Paging stays inside the filtered result: 600 rows, so page two is the
	// last one and holds the remaining 100.
	m.Update(key("n"))
	if m.PageOffset() != PageSize || m.PageRows() != 100 {
		t.Fatalf("second filtered page = %d rows at %d", m.PageRows(), m.PageOffset())
	}
	m.Update(key("n"))
	if m.PageOffset() != PageSize {
		t.Fatalf("past the end must stay put, offset = %d", m.PageOffset())
	}
}

func TestFilterEscRestoresTheWholeTable(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	m.Update(key("/"))
	typeInto(&m, "WHERE id > 1100")
	m.Update(key("enter"))
	if m.Filter() == "" {
		t.Fatal("the filter must be applied")
	}
	m.Update(key("/"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Filtering() || m.Filter() != "" {
		t.Fatalf("esc must close the line and drop the filter: editing=%v filter=%q", m.Filtering(), m.Filter())
	}
	if m.PageRows() != PageSize {
		t.Fatalf("the unfiltered page = %d rows, want %d", m.PageRows(), PageSize)
	}
	view := stripANSI(m.View())
	if strings.Contains(view, "filter:") {
		t.Fatalf("no filter marker may survive esc, view:\n%s", view)
	}
	if !strings.Contains(view, "rows 1–500 of 1200") {
		t.Fatalf("the whole table must be back, view:\n%s", view)
	}
}

func TestFilterEmptyClauseClearsIt(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	m.Update(key("/"))
	typeInto(&m, "WHERE id > 1100")
	m.Update(key("enter"))
	m.Update(key("/"))
	// Wipe the seeded clause and apply the empty line.
	for range m.FilterInput() {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m.Update(key("enter"))
	if m.Filtering() || m.Filter() != "" {
		t.Fatalf("an empty clause must clear the filter: editing=%v filter=%q", m.Filtering(), m.Filter())
	}
	if !strings.Contains(stripANSI(m.View()), "rows 1–500 of 1200") {
		t.Fatal("the unfiltered grid must be back")
	}
}

func TestFilterBrokenClauseKeepsTheLastGoodGrid(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	m.Update(key("/"))
	typeInto(&m, "WHERE id > 600 ORDER BY id")
	m.Update(key("enter"))
	before := m.PageRows()

	m.Update(key("/"))
	typeInto(&m, " AND nope >")
	m.Update(key("enter"))
	if !m.Filtering() {
		t.Fatal("a rejected clause keeps the line open")
	}
	if m.FilterErr() == nil {
		t.Fatal("a rejected clause must report the engine's error")
	}
	if m.Filter() != "WHERE id > 600 ORDER BY id" || m.PageRows() != before {
		t.Fatalf("the last good grid must stay: filter=%q rows=%d", m.Filter(), m.PageRows())
	}
	if !strings.Contains(stripANSI(m.View()), m.FilterErr().Error()) {
		t.Fatalf("the error must be visible, view:\n%s", stripANSI(m.View()))
	}
	// Editing the clause drops the stale error, and a valid clause applies.
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.FilterErr() != nil {
		t.Fatal("editing must clear the error of the text that is gone")
	}
}

func TestFilterRejectsSecondStatement(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	m.Update(key("/"))
	typeInto(&m, "WHERE id = 1; DROP TABLE users")
	m.Update(key("enter"))
	if m.Filter() != "" {
		t.Fatalf("a multi-statement clause must not apply, filter = %q", m.Filter())
	}
	if m.FilterErr() == nil || !strings.Contains(m.FilterErr().Error(), ";") {
		t.Fatalf("error = %v, want the multi-statement refusal", m.FilterErr())
	}
	// The table is untouched: the grid still pages all 1200 rows.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(stripANSI(m.View()), "rows 1–500 of 1200") {
		t.Fatalf("the table must be intact, view:\n%s", stripANSI(m.View()))
	}
}

func TestFilterDroppedWithTheTable(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	m.Update(key("/"))
	typeInto(&m, "WHERE id > 1100")
	m.Update(key("enter"))
	m.Update(key("tab")) // back to the sidebar
	m.Update(key("k"))   // empty
	m.Update(key("enter"))
	if m.Filter() != "" {
		t.Fatalf("switching tables must drop the filter, got %q", m.Filter())
	}
}

// filterLineOf returns the filter input line — the second-to-last view line
// while the field is open.
func filterLineOf(view string) string {
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		return ""
	}
	return lines[len(lines)-2]
}

// sqlHighlightAvailable reports whether this build carries the SQL grammar —
// a CGo-free build has none, and the clause then renders plain.
func sqlHighlightAvailable() bool { return highlight.FencedSupported("sql") }
