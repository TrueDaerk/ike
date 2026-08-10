package dataview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// parquetFixture is the shape of the generated Parquet fixture: enough rows to
// need several pages, plus a wide-ish row so the grid has columns to scroll.
type parquetFixture struct {
	ID   int64    `parquet:"id"`
	Name string   `parquet:"name"`
	Note *string  `parquet:"note,optional"`
	Tags []string `parquet:"tags,list"`
}

// writeFixtureParquet generates a Parquet file with 1200 rows over several row
// groups. Generated rather than committed, so no binary blob ages in the repo.
func writeFixtureParquet(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "events.parquet")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]parquetFixture, 1200)
	for i := range rows {
		rows[i] = parquetFixture{ID: int64(i + 1), Name: fmt.Sprintf("user-%d", i+1), Tags: []string{"a"}}
	}
	w := parquet.NewGenericWriter[parquetFixture](f, parquet.MaxRowsPerRowGroup(250))
	if _, err := w.Write(rows); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestParquetFileOpensInTheGrid is the end-to-end acceptance of #1766: the
// pane opens a Parquet file, lists it as its single table, and pages it — the
// bytes never reach a text buffer.
func TestParquetFileOpensInTheGrid(t *testing.T) {
	m := newPane(t, writeFixtureParquet(t))
	if m.Err() != nil {
		t.Fatal(m.Err())
	}
	if m.Tables() != 1 {
		t.Fatalf("tables = %d, want the file itself as the only entry", m.Tables())
	}
	if m.SelectedTable() != "events.parquet" {
		t.Fatalf("selected = %q, want the file's base name", m.SelectedTable())
	}
	if m.PageRows() != PageSize || m.PageOffset() != 0 {
		t.Fatalf("first page = %d rows at %d", m.PageRows(), m.PageOffset())
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, fmt.Sprintf("rows 1–%d of 1200", PageSize)) {
		t.Fatalf("status line missing, view:\n%s", view)
	}
	for _, want := range []string{"id", "name", "note", "tags", "user-1", `["a"]`} {
		if !strings.Contains(view, want) {
			t.Errorf("grid lacks %q, view:\n%s", want, view)
		}
	}

	// Paging walks the file without a full load.
	m.Update(key("tab")) // sidebar -> grid
	m.Update(key("n"))
	if m.PageOffset() != PageSize {
		t.Fatalf("offset = %d after n, want %d", m.PageOffset(), PageSize)
	}
	m.Update(key("n"))
	if m.PageOffset() != 2*PageSize || m.PageRows() != 1200-2*PageSize {
		t.Fatalf("last page = %d rows at %d", m.PageRows(), m.PageOffset())
	}
}

// TestParquetSchemaKeyShowsTheSchemaView proves `s` carries the format's
// metadata — the interesting part of a Parquet file — to the read-only buffer.
func TestParquetSchemaKeyShowsTheSchemaView(t *testing.T) {
	m := newPane(t, writeFixtureParquet(t))
	cmd := m.Update(key("s"))
	if cmd == nil {
		t.Fatal("s produced no command")
	}
	msg, ok := cmd().(ShowSchemaMsg)
	if !ok {
		t.Fatalf("s produced %T, want ShowSchemaMsg", cmd())
	}
	for _, want := range []string{"1200 rows", "row groups", "REQUIRED", "OPTIONAL", "message "} {
		if !strings.Contains(msg.SQL, want) {
			t.Errorf("schema view lacks %q:\n%s", want, msg.SQL)
		}
	}
}

// TestCorruptParquetDegradesToANotice keeps a truncated file out of a text
// buffer and out of a crash: the pane explains itself instead.
func TestCorruptParquetDegradesToANotice(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.parquet")
	if err := os.WriteFile(p, []byte("PAR1 truncated before the footer"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newPane(t, p)
	if m.Err() == nil {
		t.Fatal("a truncated parquet file must not open cleanly")
	}
	if !strings.Contains(stripANSI(m.View()), "cannot open database") {
		t.Errorf("no notice rendered, view:\n%s", stripANSI(m.View()))
	}
}
