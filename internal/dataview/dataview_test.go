package dataview

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	_ "modernc.org/sqlite"

	"ike/internal/datasrc"
	"ike/internal/theme"
)

// writeFixtureDB builds `users` (1200 rows with NULL notes), `empty`, and a
// view — enough shape for paging, NULL rendering and the sidebar.
func writeFixtureDB(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range []string{
		`CREATE TABLE empty (x TEXT)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, note TEXT)`,
		`CREATE VIEW named AS SELECT name FROM users`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ins, err := tx.Prepare(`INSERT INTO users (id, name, note) VALUES (?, ?, NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 1200; i++ {
		if _, err := ins.Exec(i, fmt.Sprintf("user-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return p
}

// newPane builds a sized, focused pane over the database at p and settles its
// background open (#1795) — New itself touches no file, so a test that wants
// rows has to let the runtime deliver them first.
func newPane(t *testing.T, p string) Model {
	t.Helper()
	m := New("data", p, theme.DefaultPalette())
	t.Cleanup(func() { m.Close() })
	m.SetSize(100, 24)
	m.SetFocused(true)
	pump(t, &m, m.Init())
	return m
}

// pump runs a command tree the way the bubbletea runtime would: every result
// the pane owns (the open, a row count) goes straight back into it, and the
// loop keeps going until the pane has no background work left. Messages meant
// for the root model — a ShowSchemaMsg — are handed back to the caller.
func pump(t *testing.T, m *Model, cmds ...tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	for len(cmds) > 0 {
		cmd := cmds[0]
		cmds = cmds[1:]
		if cmd == nil {
			continue
		}
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			cmds = append(cmds, msg...)
		case ResultMsg:
			cmds = append(cmds, m.Update(msg))
		default:
			out = append(out, msg)
		}
	}
	return out
}

// feed sends one message to the pane and settles whatever it started.
func feed(t *testing.T, m *Model, msg tea.Msg) []tea.Msg {
	t.Helper()
	return pump(t, m, m.Update(msg))
}

// key builds the key press for one chord name, matching the panel test
// convention elsewhere in the tree.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestOpenSelectsFirstTable(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	if m.Err() != nil {
		t.Fatal(m.Err())
	}
	if m.Tables() != 3 {
		t.Fatalf("tables = %d, want 3", m.Tables())
	}
	if m.SelectedTable() != "empty" {
		t.Fatalf("selected = %q, want the first table", m.SelectedTable())
	}
}

func TestSelectTableLoadsFirstPage(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j")) // sidebar: empty -> users
	feed(t, &m, key("enter"))
	if m.SelectedTable() != "users" {
		t.Fatalf("selected = %q", m.SelectedTable())
	}
	if m.PageRows() != PageSize || m.PageOffset() != 0 {
		t.Fatalf("page = %d rows at %d", m.PageRows(), m.PageOffset())
	}
	if !strings.Contains(stripANSI(m.View()), fmt.Sprintf("rows 1–%d of 1200", PageSize)) {
		t.Fatalf("status line missing, view:\n%s", stripANSI(m.View()))
	}
}

func TestPagingKeysWalkTheTable(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	feed(t, &m, key("n")) // next page
	if m.PageOffset() != PageSize {
		t.Fatalf("offset = %d after n", m.PageOffset())
	}
	feed(t, &m, key("n"))
	if m.PageOffset() != 2*PageSize || m.PageRows() != 1200-2*PageSize {
		t.Fatalf("last page = %d rows at %d", m.PageRows(), m.PageOffset())
	}
	feed(t, &m, key("n")) // past the end: stays
	if m.PageOffset() != 2*PageSize {
		t.Fatalf("offset moved past the end: %d", m.PageOffset())
	}
	feed(t, &m, key("p"))
	if m.PageOffset() != PageSize {
		t.Fatalf("offset = %d after p", m.PageOffset())
	}
	feed(t, &m, key("g"))
	if m.PageOffset() != 0 || m.Cursor() != 0 {
		t.Fatalf("g must jump to the first page, offset=%d", m.PageOffset())
	}
	feed(t, &m, key("G"))
	if m.PageOffset() != 1000 || m.Cursor() != m.PageRows()-1 {
		t.Fatalf("G must jump to the last page, offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
}

func TestRowStepCrossesPageEdges(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	// k at the top of page 0: stays put.
	feed(t, &m, key("k"))
	if m.PageOffset() != 0 || m.Cursor() != 0 {
		t.Fatalf("k at the very top moved: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
	feed(t, &m, key("G")) // last row of last page
	feed(t, &m, key("j")) // past the very end: stays
	if m.PageOffset() != 1000 || m.Cursor() != m.PageRows()-1 {
		t.Fatalf("j at the very end moved: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
	feed(t, &m, key("k")) // step back within the page
	if m.Cursor() != m.PageRows()-2 {
		t.Fatalf("cursor = %d", m.Cursor())
	}
	// Walk the cursor to row 0 of the last page, then k crosses to the
	// previous page's last row.
	feed(t, &m, key("g"))
	feed(t, &m, key("n"))
	feed(t, &m, key("n"))
	feed(t, &m, key("k"))
	if m.PageOffset() != PageSize || m.Cursor() != PageSize-1 {
		t.Fatalf("k at a page top must load the previous page: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
	feed(t, &m, key("j"))
	if m.PageOffset() != 2*PageSize || m.Cursor() != 0 {
		t.Fatalf("j at a page bottom must load the next page: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
}

// writeSmallDB builds a single table of n rows — a table well under PageSize,
// where the whole-page fetch has nothing to do and only a screenful jump can
// move the cursor.
func writeSmallDB(t *testing.T, n int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "small.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		if _, err := db.Exec(`INSERT INTO items (id, name) VALUES (?, ?)`, i, fmt.Sprintf("item-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// TestScreenKeysPageAShortTable: pgup/pgdown blätter a screenful inside the
// loaded page, so a table shorter than PageSize scrolls — while n/p, which
// fetch whole DB pages, have nothing to do there (#1788).
func TestScreenKeysPageAShortTable(t *testing.T) {
	m := newPane(t, writeSmallDB(t, 40))
	feed(t, &m, key("enter"))
	screen := m.gridHeight()
	if screen < 2 || screen >= 40 {
		t.Fatalf("test needs a screen smaller than the table: %d", screen)
	}
	feed(t, &m, key("pgdown"))
	if m.Cursor() != screen || m.PageOffset() != 0 {
		t.Fatalf("pgdown: cursor=%d offset=%d, want cursor=%d", m.Cursor(), m.PageOffset(), screen)
	}
	// The next jump runs past the end: it stops on the last row.
	feed(t, &m, key("pgdown"))
	feed(t, &m, key("pgdown"))
	if m.Cursor() != 39 {
		t.Fatalf("pgdown past the end: cursor = %d", m.Cursor())
	}
	feed(t, &m, key("pgup"))
	if m.Cursor() != 39-screen {
		t.Fatalf("pgup: cursor = %d, want %d", m.Cursor(), 39-screen)
	}
	feed(t, &m, key("pgup"))
	feed(t, &m, key("pgup"))
	if m.Cursor() != 0 {
		t.Fatalf("pgup past the top: cursor = %d", m.Cursor())
	}
	// The whole-page keys keep their fetch semantics: there is no second page.
	feed(t, &m, key("n"))
	if m.PageOffset() != 0 || m.Cursor() != 0 {
		t.Fatalf("n on a one-page table: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
}

// TestScreenKeysCrossPageEdges: a screenful jump off the loaded page's edge
// fetches the neighbour page, like j/k do.
func TestScreenKeysCrossPageEdges(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	for i := 0; i < 100 && m.PageOffset() == 0; i++ {
		feed(t, &m, key("pgdown"))
	}
	if m.PageOffset() != PageSize || m.Cursor() != 0 {
		t.Fatalf("pgdown at the page bottom must fetch the next page: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
	feed(t, &m, key("pgup"))
	if m.PageOffset() != 0 || m.Cursor() != PageSize-1 {
		t.Fatalf("pgup at the page top must fetch the previous page: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
}

func TestNullRendersDistinctly(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	view := stripANSI(m.View())
	if !strings.Contains(view, nullCell) {
		t.Fatalf("the NULL note column must render %q, view:\n%s", nullCell, view)
	}
}

func TestSchemaKeyEmitsDDL(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j")) // sidebar cursor on users
	out := feed(t, &m, key("s"))
	if len(out) != 1 {
		t.Fatalf("s must emit the schema message, got %v", out)
	}
	msg, ok := out[0].(ShowSchemaMsg)
	if !ok {
		t.Fatalf("message = %T", out[0])
	}
	if msg.Table != "users" || !strings.HasPrefix(msg.SQL, "CREATE TABLE users") {
		t.Fatalf("schema msg = %+v", msg)
	}
}

func TestCorruptFileDegradesToNotice(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.db")
	if err := writeFile(p, "not a database"); err != nil {
		t.Fatal(err)
	}
	m := newPane(t, p)
	if m.Err() == nil {
		t.Fatal("a corrupt file must keep its open error")
	}
	if !strings.Contains(stripANSI(m.View()), "cannot open database") {
		t.Fatalf("view must explain the failure:\n%s", stripANSI(m.View()))
	}
}

// TestMissingToolShowsActionableDialog: a backend whose external engine is
// not installed (the DuckDB CLI, #1765) is an actionable state — the pane
// draws a bordered dialog naming the tool and how to install it, not a bare
// notice line.
func TestMissingToolShowsActionableDialog(t *testing.T) {
	m := Model{key: "data", path: "/tmp/app.duckdb", pal: theme.DefaultPalette(), sel: -1}
	m.err = &datasrc.MissingToolError{
		Tool:  "duckdb",
		Why:   "DuckDB databases are read through the duckdb command line tool",
		Hints: []string{"brew install duckdb"},
	}
	m.SetSize(90, 24)
	view := stripANSI(m.View())
	for _, want := range []string{"duckdb not found", "app.duckdb", "brew install duckdb", "╭"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dialog must contain %q:\n%s", want, view)
		}
	}
	// A pane too small for the box still says what is missing, on one line.
	m.SetSize(20, 3)
	small := stripANSI(m.View())
	if strings.Contains(small, "╭") || !strings.Contains(small, "duckdb") {
		t.Fatalf("small pane must fall back to the notice:\n%s", small)
	}
}

func TestHorizontalScrollMovesColumns(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	before := stripANSI(m.View())
	if !strings.Contains(before, "id") {
		t.Fatalf("header missing:\n%s", before)
	}
	feed(t, &m, key("l")) // scroll one column right
	after := stripANSI(m.View())
	if !strings.Contains(after, "name") || strings.Contains(headerLineOf(after), " id ") {
		t.Fatalf("after l the id column must scroll away:\n%s", after)
	}
}

// stripANSI drops SGR sequences so a styled string can be compared by text.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// writeFile writes one small fixture file.
func writeFile(p, content string) error {
	return os.WriteFile(p, []byte(content), 0o644)
}

// headerLineOf extracts the grid header line (the second view line).
func headerLineOf(view string) string {
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		return ""
	}
	return lines[1]
}
