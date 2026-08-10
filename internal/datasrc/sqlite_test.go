package datasrc

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixtureDB builds a small database: `users` with 1200 rows (NULLs and a
// BLOB among them), an empty table, and a view.
func writeFixtureDB(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, note TEXT, pic BLOB)`,
		`CREATE TABLE empty (x TEXT)`,
		`CREATE VIEW named AS SELECT name FROM users WHERE name IS NOT NULL`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ins, err := tx.Prepare(`INSERT INTO users (id, name, note, pic) VALUES (?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 1200; i++ {
		name := any("user-" + itoa(i))
		var note, pic any
		if i%3 == 0 {
			note = "" // empty string, distinct from NULL
		}
		if i == 1 {
			pic = []byte{1, 2, 3, 4}
		}
		if _, err := ins.Exec(i, name, note, pic); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return p
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestIsSQLiteSniff(t *testing.T) {
	p := writeFixtureDB(t)
	if !IsSQLite(p, nil) {
		t.Fatal("the fixture database must sniff as SQLite")
	}
	head, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !IsSQLite(p, head[:32]) {
		t.Fatal("the head-based sniff must also claim it")
	}
	txt := filepath.Join(t.TempDir(), "notes.db")
	if err := os.WriteFile(txt, []byte("just text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsSQLite(txt, nil) {
		t.Fatal("a text file must not sniff as SQLite")
	}
	if IsSQLite(txt, []byte("just text\n")) {
		t.Fatal("a text head must not sniff as SQLite")
	}
}

func TestTablesListsObjectsWithCounts(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	tables, err := src.Tables()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Table{}
	for _, tb := range tables {
		got[tb.Name] = tb
	}
	if len(got) != 3 {
		t.Fatalf("objects = %d, want 3", len(got))
	}
	if u := got["users"]; u.Type != "table" || u.Rows != 1200 {
		t.Fatalf("users = %+v", u)
	}
	if e := got["empty"]; e.Rows != 0 {
		t.Fatalf("empty = %+v", e)
	}
	if v := got["named"]; !v.IsView() || v.Rows != 1200 {
		t.Fatalf("named = %+v", v)
	}
}

func TestPagePagingMath(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	p0, err := src.Page("users", 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(p0.Rows) != 500 || p0.Offset != 0 || p0.Total != 1200 {
		t.Fatalf("page 0 = %d rows at %d of %d", len(p0.Rows), p0.Offset, p0.Total)
	}
	if len(p0.Columns) != 4 || p0.Columns[0] != "id" {
		t.Fatalf("columns = %v", p0.Columns)
	}
	p2, err := src.Page("users", 1000, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Rows) != 200 {
		t.Fatalf("last page = %d rows, want 200", len(p2.Rows))
	}
	// Stable order: the last page's first row follows page 1's last row.
	if p2.Rows[0][0].Text != "1001" {
		t.Fatalf("last page starts at id %s, want 1001", p2.Rows[0][0].Text)
	}
}

func TestPageRendersNullBlobAndEmpty(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	p, err := src.Page("users", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	row1 := p.Rows[0] // id 1: note NULL, pic blob
	if !row1[2].Null {
		t.Fatal("a NULL column must scan as a Null cell")
	}
	if row1[3].Text != "<blob 4 bytes>" {
		t.Fatalf("blob cell = %q", row1[3].Text)
	}
	row3 := p.Rows[2] // id 3: note ""
	if row3[2].Null || row3[2].Text != "" {
		t.Fatalf("an empty string must stay a non-null empty cell, got %+v", row3[2])
	}
}

func TestSchemaReturnsDDL(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	ddl, err := src.Schema("users")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ddl, "CREATE TABLE users") {
		t.Fatalf("ddl = %q", ddl)
	}
	if _, err := src.Schema("nope"); err == nil {
		t.Fatal("an unknown table must error")
	}
}

// TestReadOnlyNeverBlocksWriters: with the pane's source open, a second
// connection still writes — mode=ro takes no lock a writer would wait on.
func TestReadOnlyNeverBlocksWriters(t *testing.T) {
	p := writeFixtureDB(t)
	src, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := src.Page("users", 0, 10); err != nil {
		t.Fatal(err)
	}
	w, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Exec(`INSERT INTO users (id, name) VALUES (9001, 'late')`); err != nil {
		t.Fatalf("a writer must not be blocked by the viewer: %v", err)
	}
	// And the fresh row is visible to the next page fetch.
	page, err := src.Page("users", 1200, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.Rows[0][1].Text != "late" {
		t.Fatalf("post-write page = %+v", page.Rows)
	}
}

func TestOpenRejectsGarbage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.sqlite")
	if err := os.WriteFile(p, []byte("this is not a database at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Fatal("a corrupt file must fail to open")
	}
	other := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(other, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(other); err == nil {
		t.Fatal("an unclaimed extension without magic must fail to open")
	}
}
