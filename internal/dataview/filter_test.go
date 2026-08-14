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
func typeInto(t *testing.T, m *Model, s string) {
	t.Helper()
	for _, r := range s {
		feed(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// openGrid loads `users` and puts the cursor in the grid.
func openGrid(t *testing.T, m *Model) {
	t.Helper()
	feed(t, m, key("j")) // sidebar: empty -> users
	feed(t, m, key("enter"))
	if m.SelectedTable() != "users" {
		t.Fatalf("selected = %q, want users", m.SelectedTable())
	}
}

func TestFilterLineShowsPrefixAndClause(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	feed(t, &m, key("/"))
	if !m.Filtering() {
		t.Fatal("'/' in the grid must open the filter line")
	}
	typeInto(t, &m, "WHERE id > 1100")
	if m.FilterInput() != "WHERE id > 1100" {
		t.Fatalf("input = %q", m.FilterInput())
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, `SELECT * FROM "users" WHERE id > 1100`) {
		t.Fatalf("the filter line must show the fixed prefix before the clause, view:\n%s", view)
	}
	// The grid's letter keys are text while the line is open: "n" types, it
	// does not page.
	typeInto(t, &m, "n")
	if m.PageOffset() != 0 || !strings.HasSuffix(m.FilterInput(), "n") {
		t.Fatalf("a letter must type into the line, not page: offset %d input %q", m.PageOffset(), m.FilterInput())
	}
}

// TestFilterConditionGetsTheImplicitWhere covers #1885: `/` prefills the head
// down to `WHERE `, so a bare condition is a whole query.
func TestFilterConditionGetsTheImplicitWhere(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	feed(t, &m, key("/"))
	view := stripANSI(m.View())
	if !strings.Contains(view, `SELECT * FROM "users" WHERE `) {
		t.Fatalf("the empty line must already show the WHERE head, view:\n%s", view)
	}
	typeInto(t, &m, "id > 600 ORDER BY id")
	if !strings.Contains(stripANSI(m.View()), `SELECT * FROM "users" WHERE id > 600 ORDER BY id`) {
		t.Fatalf("the condition must render behind the WHERE head, view:\n%s", stripANSI(m.View()))
	}
	feed(t, &m, key("enter"))
	if m.Filter() != "WHERE id > 600 ORDER BY id" {
		t.Fatalf("applied filter = %q, want the WHERE-prefixed clause", m.Filter())
	}
	if m.PageRows() != PageSize || m.PageOffset() != 0 {
		t.Fatalf("filtered page = %d rows at %d", m.PageRows(), m.PageOffset())
	}
	// Reopening seeds the condition as typed, not the clause with its WHERE.
	feed(t, &m, key("/"))
	if m.FilterInput() != "id > 600 ORDER BY id" {
		t.Fatalf("seeded input = %q, want the bare condition", m.FilterInput())
	}
}

// TestFilterKeywordClauseKeepsItsOwnHead covers #1885: a line that opens a
// clause itself runs as typed and loses the implicit WHERE from the head.
func TestFilterKeywordClauseKeepsItsOwnHead(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	feed(t, &m, key("/"))
	typeInto(t, &m, "ORDER BY id DESC LIMIT 10")
	view := stripANSI(m.View())
	if strings.Contains(view, "WHERE") {
		t.Fatalf("a clause of its own must not get the implicit WHERE, view:\n%s", view)
	}
	feed(t, &m, key("enter"))
	if m.Filter() != "ORDER BY id DESC LIMIT 10" {
		t.Fatalf("applied filter = %q, want the clause as typed", m.Filter())
	}
	// An explicit WHERE is passed through too, and seeds back unchanged in the
	// condition slot.
	feed(t, &m, key("/"))
	for range m.FilterInput() {
		feed(t, &m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	typeInto(t, &m, "where id > 1100")
	feed(t, &m, key("enter"))
	if m.Filter() != "where id > 1100" {
		t.Fatalf("applied filter = %q, want the explicit WHERE untouched", m.Filter())
	}
	feed(t, &m, key("/"))
	if m.FilterInput() != "id > 1100" {
		t.Fatalf("seeded input = %q, want the condition without the WHERE", m.FilterInput())
	}
}

func TestClauseAndConditionRoundTrip(t *testing.T) {
	for _, tc := range []struct{ input, clause, seed string }{
		{"id = 5", "WHERE id = 5", "id = 5"},
		{"  name LIKE 'foo%'  ", "WHERE name LIKE 'foo%'", "name LIKE 'foo%'"},
		{"WHERE id = 5", "WHERE id = 5", "id = 5"},
		{"where id = 5", "where id = 5", "id = 5"},
		{"ORDER BY id", "ORDER BY id", "ORDER BY id"},
		{"order   by id", "order   by id", "order   by id"},
		{"LIMIT 10", "LIMIT 10", "LIMIT 10"},
		{"whereabouts = 1", "WHERE whereabouts = 1", "whereabouts = 1"},
		{"orders = 1", "WHERE orders = 1", "orders = 1"},
	} {
		if got := clauseOf(tc.input); got != tc.clause {
			t.Errorf("clauseOf(%q) = %q, want %q", tc.input, got, tc.clause)
		}
		if got := conditionOf(clauseOf(tc.input)); got != tc.seed {
			t.Errorf("conditionOf(clauseOf(%q)) = %q, want %q", tc.input, got, tc.seed)
		}
	}
	if got := conditionOf(""); got != "" {
		t.Errorf("conditionOf(%q) = %q, want empty", "", got)
	}
}

func TestFilterLineKeepsThePaneHeight(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	closed := strings.Count(m.View(), "\n")
	feed(t, &m, key("/"))
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
	feed(t, &m, key("/"))
	typeInto(t, &m, "WHERE id > 1100")
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
	feed(t, &m2, key("/"))
	typeInto(t, &m2, "wheres id > 1100")
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
	feed(t, &m, key("/"))
	typeInto(t, &m, "WHERE id > 600 ORDER BY id")
	feed(t, &m, key("enter"))
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
	feed(t, &m, key("n"))
	if m.PageOffset() != PageSize || m.PageRows() != 100 {
		t.Fatalf("second filtered page = %d rows at %d", m.PageRows(), m.PageOffset())
	}
	feed(t, &m, key("n"))
	if m.PageOffset() != PageSize {
		t.Fatalf("past the end must stay put, offset = %d", m.PageOffset())
	}
}

func TestFilterEscRestoresTheWholeTable(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	feed(t, &m, key("/"))
	typeInto(t, &m, "WHERE id > 1100")
	feed(t, &m, key("enter"))
	if m.Filter() == "" {
		t.Fatal("the filter must be applied")
	}
	feed(t, &m, key("/"))
	feed(t, &m, tea.KeyPressMsg{Code: tea.KeyEscape})
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
	feed(t, &m, key("/"))
	typeInto(t, &m, "WHERE id > 1100")
	feed(t, &m, key("enter"))
	feed(t, &m, key("/"))
	// Wipe the seeded clause and apply the empty line.
	for range m.FilterInput() {
		feed(t, &m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	feed(t, &m, key("enter"))
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
	feed(t, &m, key("/"))
	typeInto(t, &m, "WHERE id > 600 ORDER BY id")
	feed(t, &m, key("enter"))
	before := m.PageRows()

	feed(t, &m, key("/"))
	typeInto(t, &m, " AND nope >")
	feed(t, &m, key("enter"))
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
	feed(t, &m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.FilterErr() != nil {
		t.Fatal("editing must clear the error of the text that is gone")
	}
}

func TestFilterRejectsSecondStatement(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	feed(t, &m, key("/"))
	typeInto(t, &m, "WHERE id = 1; DROP TABLE users")
	feed(t, &m, key("enter"))
	if m.Filter() != "" {
		t.Fatalf("a multi-statement clause must not apply, filter = %q", m.Filter())
	}
	if m.FilterErr() == nil || !strings.Contains(m.FilterErr().Error(), ";") {
		t.Fatalf("error = %v, want the multi-statement refusal", m.FilterErr())
	}
	// The table is untouched: the grid still pages all 1200 rows.
	feed(t, &m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(stripANSI(m.View()), "rows 1–500 of 1200") {
		t.Fatalf("the table must be intact, view:\n%s", stripANSI(m.View()))
	}
}

func TestFilterDroppedWithTheTable(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	openGrid(t, &m)
	feed(t, &m, key("/"))
	typeInto(t, &m, "WHERE id > 1100")
	feed(t, &m, key("enter"))
	feed(t, &m, key("tab")) // back to the sidebar
	feed(t, &m, key("k"))   // empty
	feed(t, &m, key("enter"))
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
