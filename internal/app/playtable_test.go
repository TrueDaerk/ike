package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// playtable_test.go covers the playground's table view (#2794): the shape
// check and its fallback, the grid model (columns, sort, cursor, search),
// cell copy, the drill-in, the focus round trip and the mouse mapping.

const playTableInput = `[{"id":3,"name":"carol","tags":["x"]},{"id":1,"name":"alice"},{"id":2,"name":"bob","extra":null}]`

// tableOn dispatches playground.tableView.
func tableOn(m Model) Model {
	tm, cmd := m.Update(TogglePlayTableMsg{})
	return drainCmd(tm.(Model), cmd)
}

// tablePlay opens the jq playground over playTableInput with the table view up.
func tablePlay(t *testing.T) Model {
	t.Helper()
	m := playNoOnboarding(openJQ(t, playApp(t, playTableInput)))
	m = setProgram(m, ".")
	m = tableOn(m)
	if m.play.table == nil {
		t.Fatalf("the table view must show an array of objects, status %q", m.play.status)
	}
	return m
}

// tableBody is the rendered result area of the playground, ANSI stripped.
func tableBody(m Model) string {
	return ansi.Strip(m.playInlineBody(playResultWidth(m)))
}

// tableColumn is column c of the table's display rows.
func tableColumn(m Model, c int) []string {
	t := m.play.table
	out := make([]string, len(t.order))
	for r := range t.order {
		out[r] = t.cell(r, c).Text
	}
	return out
}

// TestPlayTableShowsGrid is the acceptance case: an `[{…}]` result becomes a
// grid with one column per key, nested values as compact JSON and a missing
// key as ∅, in the result editor's rectangle — the editor keeps its size, and
// the grid fills exactly its rows below the unchanged header.
func TestPlayTableShowsGrid(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, playTableInput)))
	m = setProgram(m, ".")
	edW, edH := m.play.resultEd.Width(), m.play.resultEd.Height()
	header := len(strings.Split(tableBody(m), "\n")) - len(strings.Split(m.play.resultEd.View(), "\n"))
	m = tableOn(m)
	m.play.status = "" // the toggle's confirmation covers the result summary
	if got := strings.Join(m.play.table.labels, ","); got != "id,name,tags,extra" {
		t.Fatalf("columns = %q", got)
	}
	body := tableBody(m)
	for _, want := range []string{"id", "name", "tags", "extra", "carol", `["x"]`, "∅", "null", "row 1/3"} {
		if !strings.Contains(body, want) {
			t.Errorf("the table view lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `"name": "carol"`) {
		t.Errorf("the table view must replace the JSON text:\n%s", body)
	}
	if w, h := m.play.resultEd.Width(), m.play.resultEd.Height(); w != edW || h != edH {
		t.Errorf("the result rectangle moved: %dx%d, was %dx%d", w, h, edW, edH)
	}
	if rows := len(strings.Split(body, "\n")); rows != header+edH {
		t.Errorf("the table body has %d rows, want the %d header rows plus the result's %d", rows, header, edH)
	}
	for i, l := range strings.Split(body, "\n") {
		if w := ansi.StringWidth(l); w > playResultWidth(m) {
			t.Errorf("row %d is %d cells, wider than the pane's %d", i, w, playResultWidth(m))
		}
	}
	m = tableOn(m)
	if m.play.table != nil || m.play.tableOn {
		t.Fatal("the toggle must switch back to the text view")
	}
	if !strings.Contains(tableBody(m), `"name": "carol"`) {
		t.Error("the text view must be back")
	}
}

// TestPlayTableScalars: a list of scalars is one `value` column.
func TestPlayTableScalars(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, `[3,1,2]`)))
	m = setProgram(m, ".")
	m = tableOn(m)
	if m.play.table == nil || strings.Join(m.play.table.labels, ",") != "value" {
		t.Fatalf("scalar table = %+v", m.play.table)
	}
}

// TestPlayTableRejectsShape: a result that is no list notifies and stays in
// the text view.
func TestPlayTableRejectsShape(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, playTableInput)))
	m = setProgram(m, ".[0]")
	m = tableOn(m)
	if m.play.table != nil || m.play.tableOn {
		t.Fatal("an object result must stay in the text view")
	}
	if n := lastNotification(t, m); !strings.Contains(n, "an object, not a list") {
		t.Errorf("notification = %q", n)
	}
	if !strings.Contains(tableBody(m), `"name": "carol"`) {
		t.Error("the text view must stay")
	}
}

// TestPlayTableFallsBackOnNewResult: every new result re-checks the shape; one
// that does not fit falls back to the text with a notice, and the grid comes
// back with the next one that fits.
func TestPlayTableFallsBackOnNewResult(t *testing.T) {
	m := tablePlay(t)
	m = setProgram(m, ".[1]")
	if m.play.table != nil {
		t.Fatal("an object result must fall back to the text view")
	}
	if !strings.Contains(m.play.status, "showing text") || !m.play.statusWarn {
		t.Errorf("status = %q, want the fallback notice", m.play.status)
	}
	if !strings.Contains(tableBody(m), `"name": "alice"`) {
		t.Error("the fallback must show the text")
	}
	m = setProgram(m, "map(select(.id > 1))")
	if m.play.table == nil || len(m.play.table.order) != 2 {
		t.Fatalf("the grid must come back for a fitting result: %+v", m.play.table)
	}
}

// TestPlayTableSort: s cycles the cursor column ascending, descending, off,
// keeping the cursor on its row; a header click sorts the clicked column; the
// sort survives a new result that still has the column.
func TestPlayTableSort(t *testing.T) {
	m := tablePlay(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = playKeys(m, "s")
	if got := strings.Join(tableColumn(m, 0), ","); got != "1,2,3" {
		t.Fatalf("ascending = %s", got)
	}
	if m.play.table.order[m.play.table.cur] != 0 {
		t.Error("the cursor must stay on its row through a sort")
	}
	if !strings.Contains(tableBody(m), "id ▲") {
		t.Error("the header must mark the sorted column")
	}
	m = playKeys(m, "s")
	if got := strings.Join(tableColumn(m, 0), ","); got != "3,2,1" {
		t.Fatalf("descending = %s", got)
	}
	m = playKeys(m, "s")
	if got := strings.Join(tableColumn(m, 0), ","); got != "3,1,2" || m.play.table.sortCol != -1 {
		t.Fatalf("unsorted = %s", got)
	}
	// A header click on the name column sorts it.
	x := 1 + m.play.table.widths[0] + 2
	m = clickResult(m, x, 0)
	if got := strings.Join(tableColumn(m, 1), ","); got != "alice,bob,carol" {
		t.Fatalf("header click sort = %s", got)
	}
	m.play.program.Set(`map(.)`)
	m = drainCmd(m, m.runPlayNow())
	if m.play.table == nil || m.play.table.sortCol != 1 || strings.Join(tableColumn(m, 1), ",") != "alice,bob,carol" {
		t.Error("the sort must survive a new result with the same column")
	}
}

// TestPlayTableCopy: y (and the copy chord) copy the cursor cell — a string's
// own text, anything else as JSON — and Y the row as JSON.
func TestPlayTableCopy(t *testing.T) {
	copied := ""
	prev := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = prev })

	m := tablePlay(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = playKeys(m, "l")
	m = playKeys(m, "y")
	if copied != "carol" {
		t.Fatalf("cell copy = %q", copied)
	}
	m = playKeys(m, "l")
	m = drainKey(m, playCopyKey())
	if copied != `["x"]` {
		t.Fatalf("copy chord = %q", copied)
	}
	m = playKeys(m, "Y")
	if copied != `{"id":3,"name":"carol","tags":["x"]}` {
		t.Fatalf("row copy = %q", copied)
	}
	m = playKeys(m, "jl")
	copied = ""
	m = playKeys(m, "y")
	if copied != "" || !strings.Contains(m.play.status, "has no extra") {
		t.Errorf("a missing cell copies nothing and says so: %q / %q", copied, m.play.status)
	}
}

// TestPlayTableSearch: the find chord from the query line opens the cell
// search in the table, typing moves the cursor to the first hit, enter
// commits it, n steps on, and esc hands the keyboard back to the query line
// like the text view's round trip.
func TestPlayTableSearch(t *testing.T) {
	m := tablePlay(t)
	m = drainKey(m, playFindKey())
	tb := m.play.table
	if !tb.search.Open || !m.play.bufFocus || !m.play.findQuery {
		t.Fatal("the find chord must open the table's search with the keyboard in it")
	}
	m = playSearchFor(m, "b")
	if tb.search.Open || len(tb.search.Matches) != 1 {
		t.Fatalf("matches = %v", tb.search.Matches)
	}
	if tb.cur != 2 || tb.col != 1 {
		t.Errorf("cursor = (%d,%d), want bob's name cell (2,1)", tb.cur, tb.col)
	}
	if !strings.Contains(tableBody(m), "/b  1/1") {
		t.Errorf("the prompt row must show the counter:\n%s", tableBody(m))
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.play.bufFocus || !m.playOpen() {
		t.Fatal("esc must return a search opened from the query line to it")
	}
	// From the table: / searches, n walks the hits in row order.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = playKeys(m, "/")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = playSearchFor(m, "a")
	if len(tb.search.Matches) != 2 { // carol and alice — a header is no cell
		t.Fatalf("matches for a = %v", tb.search.Matches)
	}
	first := tb.cur
	m = playKeys(m, "n")
	if tb.cur == first {
		t.Error("n must step to the next hit")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if tb.search.Text != "" || !m.playOpen() {
		t.Fatal("the first esc drops a committed search")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.playOpen() {
		t.Error("esc from a table without a search closes the playground, as in the text view")
	}
}

// TestPlayTableFocusRoundTrip: tab moves the keyboard between the query line
// and the table, and the cursor keys walk the table only while it has it.
func TestPlayTableFocusRoundTrip(t *testing.T) {
	m := tablePlay(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.play.playTableFocused() {
		t.Fatal("tab must give the table the keyboard")
	}
	m = playKeys(m, "j")
	if m.play.table.cur != 1 {
		t.Fatalf("j moved the cursor to %d", m.play.table.cur)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.play.bufFocus {
		t.Fatal("tab must return to the query line")
	}
	m = playKeys(m, "j")
	if m.play.program.Text != ".j" || m.play.table.cur != 1 {
		t.Errorf("the query line must take typing: %q, cursor %d", m.play.program.Text, m.play.table.cur)
	}
}

// TestPlayTableDrillIn: enter on a row appends its index in the result — not
// its sorted position — like json.jqAppendPath, and hands the keyboard back.
func TestPlayTableDrillIn(t *testing.T) {
	m := tablePlay(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = playKeys(m, "sg") // by id: alice (index 1) on top, the cursor on her
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.play.program.Text != ".[1]" {
		t.Fatalf("program = %q, want .[1]", m.play.program.Text)
	}
	if m.play.bufFocus {
		t.Error("the drill-in hands the keyboard to the query line")
	}
	if m.play.table != nil {
		t.Error("the drilled-in object falls back to the text view")
	}
}

// TestPlayTableStreamRows: a stream of objects tabulates too, and its rows
// say why they cannot be drilled into.
func TestPlayTableStreamRows(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, playTableInput)))
	m = setProgram(m, ".[]")
	m = tableOn(m)
	if m.play.table == nil || !m.play.table.grid.Stream {
		t.Fatal("a stream of objects must tabulate")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.play.program.Text != ".[]" || !strings.Contains(m.play.status, "stream") {
		t.Errorf("program %q, status %q", m.play.program.Text, m.play.status)
	}
}

// TestPlayTableMouse: a click on a data cell moves the cursor there and
// focuses the table; the wheel scrolls the rows.
func TestPlayTableMouse(t *testing.T) {
	m := tablePlay(t)
	x := 1 + m.play.table.widths[0] + 2 // the name column
	m = clickResult(m, x, 3)            // header row 0, data rows from 1
	tb := m.play.table
	if tb.cur != 2 || tb.col != 1 || !m.play.playTableFocused() {
		t.Fatalf("click → cursor (%d,%d), focused %v", tb.cur, tb.col, m.play.playTableFocused())
	}
	m.wheelPlayTable(0, -5)
	if tb.cur != 0 && tb.top != 0 {
		t.Errorf("the wheel must stay in range: cur %d top %d", tb.cur, tb.top)
	}
}

// TestPlayTableChord: the default chord reaches the toggle from the query
// line without typing into it.
func TestPlayTableChord(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, playTableInput)))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl | tea.ModAlt})
	if m.play.table == nil {
		t.Fatalf("ctrl+alt+l must toggle the table view (status %q)", m.play.status)
	}
	if m.play.program.Text != "." {
		t.Errorf("the chord typed into the query line: %q", m.play.program.Text)
	}
}

// TestPlayTableYQ: the yq dialect shares the view and spells the drill-in in
// yq.
func TestPlayTableYQ(t *testing.T) {
	m := playNoOnboarding(openYQ(t, yqApp(t, "- name: a\n  n: 2\n- name: b\n  n: 1\n")))
	m = setProgram(m, ".")
	m = tableOn(m)
	if m.play.table == nil || strings.Join(m.play.table.labels, ",") != "n,name" {
		t.Fatalf("yq table = %+v", m.play.table)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = playKeys(m, "j")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.play.program.Text != ".[1]" {
		t.Errorf("program = %q", m.play.program.Text)
	}
}
