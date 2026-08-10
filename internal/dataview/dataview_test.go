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

// newPane builds a sized, focused pane over the database at p.
func newPane(t *testing.T, p string) Model {
	t.Helper()
	m := New("data", p, theme.DefaultPalette())
	t.Cleanup(func() { m.Close() })
	m.SetSize(100, 24)
	m.SetFocused(true)
	return m
}

// key builds the key press for one chord name, matching the panel test
// convention elsewhere in the tree.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
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
	m.Update(key("j")) // sidebar: empty -> users
	m.Update(key("enter"))
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
	m.Update(key("j"))
	m.Update(key("enter"))
	m.Update(key("n")) // next page
	if m.PageOffset() != PageSize {
		t.Fatalf("offset = %d after n", m.PageOffset())
	}
	m.Update(key("n"))
	if m.PageOffset() != 2*PageSize || m.PageRows() != 1200-2*PageSize {
		t.Fatalf("last page = %d rows at %d", m.PageRows(), m.PageOffset())
	}
	m.Update(key("n")) // past the end: stays
	if m.PageOffset() != 2*PageSize {
		t.Fatalf("offset moved past the end: %d", m.PageOffset())
	}
	m.Update(key("p"))
	if m.PageOffset() != PageSize {
		t.Fatalf("offset = %d after p", m.PageOffset())
	}
	m.Update(key("g"))
	if m.PageOffset() != 0 || m.Cursor() != 0 {
		t.Fatalf("g must jump to the first page, offset=%d", m.PageOffset())
	}
	m.Update(key("G"))
	if m.PageOffset() != 1000 || m.Cursor() != m.PageRows()-1 {
		t.Fatalf("G must jump to the last page, offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
}

func TestRowStepCrossesPageEdges(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	m.Update(key("j"))
	m.Update(key("enter"))
	// k at the top of page 0: stays put.
	m.Update(key("k"))
	if m.PageOffset() != 0 || m.Cursor() != 0 {
		t.Fatalf("k at the very top moved: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
	m.Update(key("G")) // last row of last page
	m.Update(key("j")) // past the very end: stays
	if m.PageOffset() != 1000 || m.Cursor() != m.PageRows()-1 {
		t.Fatalf("j at the very end moved: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
	m.Update(key("k")) // step back within the page
	if m.Cursor() != m.PageRows()-2 {
		t.Fatalf("cursor = %d", m.Cursor())
	}
	// Walk the cursor to row 0 of the last page, then k crosses to the
	// previous page's last row.
	m.Update(key("g"))
	m.Update(key("n"))
	m.Update(key("n"))
	m.Update(key("k"))
	if m.PageOffset() != PageSize || m.Cursor() != PageSize-1 {
		t.Fatalf("k at a page top must load the previous page: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
	m.Update(key("j"))
	if m.PageOffset() != 2*PageSize || m.Cursor() != 0 {
		t.Fatalf("j at a page bottom must load the next page: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
}

func TestNullRendersDistinctly(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	m.Update(key("j"))
	m.Update(key("enter"))
	view := stripANSI(m.View())
	if !strings.Contains(view, nullCell) {
		t.Fatalf("the NULL note column must render %q, view:\n%s", nullCell, view)
	}
}

func TestSchemaKeyEmitsDDL(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	m.Update(key("j")) // sidebar cursor on users
	cmd := m.Update(key("s"))
	if cmd == nil {
		t.Fatal("s must emit the schema message")
	}
	msg, ok := cmd().(ShowSchemaMsg)
	if !ok {
		t.Fatalf("message = %T", cmd())
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

func TestHorizontalScrollMovesColumns(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	m.Update(key("j"))
	m.Update(key("enter"))
	before := stripANSI(m.View())
	if !strings.Contains(before, "id") {
		t.Fatalf("header missing:\n%s", before)
	}
	m.Update(key("l")) // scroll one column right
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
