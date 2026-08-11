package datasrc

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// duckBin locates the duckdb CLI or skips the test — the DuckDB backend is a
// soft dependency, so a machine (or CI job) without the binary must still run
// a green suite.
func duckBin(t *testing.T) string {
	t.Helper()
	bin, err := findDuckDB()
	if err != nil {
		t.Skipf("duckdb CLI not installed: %v", err)
	}
	return bin
}

// writeDuckFixture builds a DuckDB database with the shapes the grid has to
// render: 1200 rows, a NULL, an empty string, a BLOB, an empty table, a view,
// and a nested value.
func writeDuckFixture(t *testing.T) string {
	t.Helper()
	bin := duckBin(t)
	p := filepath.Join(t.TempDir(), "app.duckdb")
	sql := `CREATE TABLE users (id INTEGER, name VARCHAR, note VARCHAR, pic BLOB, tags VARCHAR[]);
		INSERT INTO users
		  SELECT i,
		         'user-' || i,
		         CASE WHEN i = 1 THEN NULL WHEN i % 3 = 0 THEN '' ELSE 'n' || i END,
		         CASE WHEN i = 1 THEN '\x01\x02\x03\x04'::BLOB ELSE NULL END,
		         ['a', 'b']
		  FROM range(1, 1201) t(i);
		CREATE TABLE empty (x VARCHAR);
		CREATE VIEW named AS SELECT name FROM users WHERE name IS NOT NULL;`
	out, err := exec.Command(bin, p, sql).CombinedOutput()
	if err != nil {
		t.Fatalf("building the fixture failed: %v\n%s", err, out)
	}
	return p
}

func TestIsDuckDBSniff(t *testing.T) {
	p := writeDuckFixture(t)
	if !IsDuckDB(p, nil) {
		t.Fatal("the fixture database must sniff as DuckDB")
	}
	head, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !IsDuckDB(p, head[:512]) {
		t.Fatal("the head-based sniff must also claim it")
	}
	if !IsDatabase(p, head[:512]) {
		t.Fatal("the handler sniff must claim a DuckDB file")
	}
	if IsSQLite(p, head[:512]) {
		t.Fatal("a DuckDB file must not sniff as SQLite")
	}
	// Too short to hold the magic, and plain text of the right length.
	if IsDuckDB(p, head[:4]) {
		t.Fatal("a head shorter than the magic must not match")
	}
	txt := filepath.Join(t.TempDir(), "notes.duckdb")
	if err := os.WriteFile(txt, []byte("just text, no magic at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsDuckDB(txt, nil) {
		t.Fatal("a text file must not sniff as DuckDB")
	}
}

// TestDuckTablesListsObjectsWithEstimates: the listing takes its sizes from
// DuckDB's catalogue (duckdb_tables().estimated_size) in the single invocation
// that lists the objects — no count(\*) over the database on open (#1795).
func TestDuckTablesListsObjectsWithEstimates(t *testing.T) {
	src, err := Open(writeDuckFixture(t))
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
		t.Fatalf("objects = %d, want 3 (%+v)", len(got), tables)
	}
	if u := got["users"]; u.Type != "table" || u.Rows != 1200 || !u.Estimated {
		t.Fatalf("users = %+v, want an estimated 1200", u)
	}
	if e := got["empty"]; e.Rows != 0 || !e.Estimated {
		t.Fatalf("empty = %+v, want an estimated 0", e)
	}
	// A view has no stored size: "?" until the background count lands.
	if v := got["named"]; !v.IsView() || v.Rows != -1 {
		t.Fatalf("named = %+v, want a view without a metadata size", v)
	}
	// Tables come out tables-first, then views — the sidebar's order.
	if tables[len(tables)-1].Name != "named" {
		t.Fatalf("views must sort last: %+v", tables)
	}
}

func TestDuckPagePagingMath(t *testing.T) {
	src, err := Open(writeDuckFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	p0, err := src.Page("users", 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	// A page never counts (#1795); Count is the exact number's own call.
	if len(p0.Rows) != 500 || p0.Offset != 0 || p0.Total != -1 {
		t.Fatalf("page 0 = %d rows at %d of %d", len(p0.Rows), p0.Offset, p0.Total)
	}
	if n, err := src.Count("users", ""); err != nil || n != 1200 {
		t.Fatalf("count = %d (%v), want 1200", n, err)
	}
	if len(p0.Columns) != 5 || p0.Columns[0] != "id" {
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

// TestDuckPageOnViewFallsBackToPlainScan: a view has no rowid, so the ORDER BY
// attempt fails and the plain scan has to carry the page.
func TestDuckPageOnView(t *testing.T) {
	src, err := Open(writeDuckFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	p, err := src.Page("named", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 10 || len(p.Columns) != 1 || p.Columns[0] != "name" {
		t.Fatalf("view page = %d rows, columns %v", len(p.Rows), p.Columns)
	}
	if p.Total != -1 {
		t.Fatalf("view page total = %d, want it uncounted", p.Total)
	}
	if n, err := src.Count("named", ""); err != nil || n != 1200 {
		t.Fatalf("view count = %d (%v), want 1200", n, err)
	}
}

func TestDuckPageRendersNullBlobEmptyAndNested(t *testing.T) {
	src, err := Open(writeDuckFixture(t))
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
		t.Fatalf("a NULL column must arrive as a Null cell, got %+v", row1[2])
	}
	if row1[3].Text != "<blob 4 bytes>" {
		t.Fatalf("blob cell = %q", row1[3].Text)
	}
	if row1[4].Text != `["a","b"]` {
		t.Fatalf("nested cell = %q", row1[4].Text)
	}
	row2 := p.Rows[1] // id 2: pic NULL
	if !row2[3].Null {
		t.Fatalf("a NULL blob must stay a Null cell, got %+v", row2[3])
	}
	row3 := p.Rows[2] // id 3: note ""
	if row3[2].Null || row3[2].Text != "" {
		t.Fatalf("an empty string must stay a non-null empty cell, got %+v", row3[2])
	}
}

func TestDuckPageOnEmptyTableKeepsColumns(t *testing.T) {
	src, err := Open(writeDuckFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	p, err := src.Page("empty", 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 0 || p.Total != -1 {
		t.Fatalf("empty page = %d rows of %d", len(p.Rows), p.Total)
	}
	// No row carries the column names, so DESCRIBE has to supply the header.
	if len(p.Columns) != 1 || p.Columns[0] != "x" {
		t.Fatalf("columns = %v", p.Columns)
	}
}

func TestDuckSchemaReturnsDDL(t *testing.T) {
	src, err := Open(writeDuckFixture(t))
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
	view, err := src.Schema("named")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view, "CREATE VIEW named") {
		t.Fatalf("view ddl = %q", view)
	}
	if _, err := src.Schema("nope"); err == nil {
		t.Fatal("an unknown table must error")
	}
}

// TestDuckOpenReadOnlyLeavesFileUntouched: the pane never writes, so the file
// must be byte-identical after a full browse.
func TestDuckOpenReadOnlyLeavesFileUntouched(t *testing.T) {
	p := writeDuckFixture(t)
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	src, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Page("users", 0, 10); err != nil {
		t.Fatal(err)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) || string(before) != string(after) {
		t.Fatal("a read-only browse must not modify the database file")
	}
}

// TestDuckOpenRejectsGarbage: a file claimed by extension but unreadable
// fails at open with DuckDB's own message — the pane's notice, not a hang.
func TestDuckOpenRejectsGarbage(t *testing.T) {
	duckBin(t)
	p := filepath.Join(t.TempDir(), "broken.duckdb")
	if err := os.WriteFile(p, []byte("this is not a database at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(p)
	if err == nil {
		t.Fatal("a corrupt file must fail to open")
	}
	if !strings.Contains(err.Error(), "not a valid DuckDB database") {
		t.Fatalf("the engine's own message must survive: %v", err)
	}
	if _, err := Open(filepath.Join(t.TempDir(), "gone.ddb")); err == nil {
		t.Fatal("a missing file must fail to open")
	}
}

// TestDuckMissingCLIIsActionable: without the binary the open fails with a
// MissingToolError carrying install hints — the pane renders it as a dialog
// instead of crashing or showing an empty grid.
func TestDuckMissingCLIIsActionable(t *testing.T) {
	prev := lookDuckDB
	lookDuckDB = func() (string, error) {
		return "", &MissingToolError{Tool: "duckdb", Why: "test", Hints: []string{"brew install duckdb"}}
	}
	defer func() { lookDuckDB = prev }()
	p := filepath.Join(t.TempDir(), "any.duckdb")
	if err := os.WriteFile(p, []byte("whatever"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(p)
	if err == nil {
		t.Fatal("a missing CLI must fail the open")
	}
	var missing *MissingToolError
	if !errors.As(err, &missing) {
		t.Fatalf("error must be a MissingToolError, got %T: %v", err, err)
	}
	if missing.Tool != "duckdb" || len(missing.Hints) == 0 {
		t.Fatalf("the dialog needs a tool name and hints: %+v", missing)
	}
	if !strings.Contains(missing.Error(), "brew install duckdb") {
		t.Fatalf("the message must carry the hint: %v", missing)
	}
}

// TestDuckLockedDatabaseErrorsInsteadOfHanging: DuckDB takes an exclusive
// lock, so a database a writer holds open cannot be read — the open must
// return that error promptly rather than block the pane.
func TestDuckLockedDatabaseErrorsInsteadOfHanging(t *testing.T) {
	bin := duckBin(t)
	p := writeDuckFixture(t)
	// A writer process parked on stdin keeps the exclusive lock.
	holder := exec.Command(bin, p)
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stdin.Close()
		holder.Process.Kill()
		holder.Wait()
	}()
	if _, err := stdin.Write([]byte("SELECT 1;\n")); err != nil {
		t.Fatal(err)
	}
	// Give the holder a moment to take the lock; retry until it has.
	var openErr error
	for i := 0; i < 40; i++ {
		src, err := Open(p)
		if err != nil {
			openErr = err
			break
		}
		src.Close()
	}
	if openErr == nil {
		t.Skip("the writer never took an exclusive lock on this platform")
	}
	if !strings.Contains(openErr.Error(), "lock") {
		t.Fatalf("a locked database must report the lock: %v", openErr)
	}
}

// TestDecodeDuckRowsPreservesColumnOrder: the JSON objects the CLI emits are
// decoded by token, because a map decode would scramble the grid's columns.
func TestDecodeDuckRowsPreservesColumnOrder(t *testing.T) {
	out := []byte(`[{"z":1,"a":"x","m":null},
{"z":2,"a":"y","m":true}]`)
	cols, rows, err := decodeDuckRows(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cols, ",") != "z,a,m" {
		t.Fatalf("columns = %v, want the emitted order", cols)
	}
	if len(rows) != 2 || rows[0][0].Text != "1" || rows[0][1].Text != "x" || !rows[0][2].Null {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if rows[1][2].Text != "true" {
		t.Fatalf("bool cell = %+v", rows[1][2])
	}
	// A row whose keys arrive in another order is realigned by name.
	shuffled := []byte(`[{"z":1,"a":"x"},{"a":"y","z":2}]`)
	_, rows, err = decodeDuckRows(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][0].Text != "2" || rows[1][1].Text != "y" {
		t.Fatalf("realigned row = %+v", rows[1])
	}
	// Empty result sets and non-result statements decode to nothing.
	if cols, rows, err := decodeDuckRows([]byte("[]\n")); err != nil || cols != nil || len(rows) != 0 {
		t.Fatalf("empty result = %v %v %v", cols, rows, err)
	}
	if _, _, err := decodeDuckRows(nil); err != nil {
		t.Fatalf("no output at all must decode to nothing: %v", err)
	}
	if _, _, err := decodeDuckRows([]byte("Parser Error: nope")); err == nil {
		t.Fatal("non-JSON output must be an error")
	}
}

func TestQuoteStringEscapes(t *testing.T) {
	if got := quoteString("O'Brien"); got != `'O''Brien'` {
		t.Fatalf("quoteString = %s", got)
	}
	if got := quoteIdent(`we"ird`); got != `"we""ird"` {
		t.Fatalf("quoteIdent = %s", got)
	}
}

func TestDuckProjectionWrapsBlobs(t *testing.T) {
	got := duckProjection([]duckColumn{{Name: "id", Type: "INTEGER"}, {Name: "pic", Type: "BLOB"}})
	want := `"id", '<blob ' || octet_length("pic") || ' bytes>' AS "pic"`
	if got != want {
		t.Fatalf("projection = %s", got)
	}
	if duckProjection(nil) != "*" {
		t.Fatal("an unknown column list must fall back to *")
	}
}
