package datasrc

// export_test.go covers the CSV/JSON export (#2248): the round trip per
// backend, the escaping both formats owe their parsers, the NULL convention,
// and the row cap that must announce itself rather than pass a prefix off as
// the whole table.

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeNastyDB builds a table whose values break a naive writer: a comma, a
// double quote, a newline, a NULL and an empty string.
func writeNastyDB(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "nasty.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE rows_ (id INTEGER PRIMARY KEY, txt TEXT)`); err != nil {
		t.Fatal(err)
	}
	values := []any{`a,b`, `say "hi"`, "line1\nline2", nil, "", `<a> & 'b'`}
	for i, v := range values {
		if _, err := db.Exec(`INSERT INTO rows_ (id, txt) VALUES (?, ?)`, i+1, v); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestExportCSVRoundTripsEveryEscape(t *testing.T) {
	src, err := Open(writeNastyDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out := filepath.Join(t.TempDir(), "rows.csv")
	res, err := ExportFile(context.Background(), src, "rows_", "", Sort{Column: "id"}, out, 0)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if res.Rows != 6 || res.Capped {
		t.Fatalf("ExportFile = %+v, want 6 uncapped rows", res)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("the export must parse as CSV: %v", err)
	}
	want := [][]string{
		{"id", "txt"},
		{"1", `a,b`},
		{"2", `say "hi"`},
		{"3", "line1\nline2"},
		{"4", ""}, // NULL is the empty field: CSV has no null
		{"5", ""},
		{"6", `<a> & 'b'`},
	}
	if len(recs) != len(want) {
		t.Fatalf("got %d records, want %d: %v", len(recs), len(want), recs)
	}
	for i := range want {
		if !equalStrings2(recs[i], want[i]) {
			t.Errorf("record %d = %v, want %v", i, recs[i], want[i])
		}
	}
}

func TestExportJSONRoundTripsNullsAndEscapes(t *testing.T) {
	src, err := Open(writeNastyDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out := filepath.Join(t.TempDir(), "rows.json")
	if _, err := ExportFile(context.Background(), src, "rows_", "", Sort{Column: "id"}, out, 0); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("the export must parse as JSON: %v\n%s", err, data)
	}
	if len(got) != 6 {
		t.Fatalf("got %d rows, want 6", len(got))
	}
	if got[3]["txt"] != nil {
		t.Errorf("a SQL NULL must export as JSON null, got %#v", got[3]["txt"])
	}
	if got[4]["txt"] != "" {
		t.Errorf("an empty string must stay an empty string, got %#v", got[4]["txt"])
	}
	if got[2]["txt"] != "line1\nline2" {
		t.Errorf("the newline must survive, got %#v", got[2]["txt"])
	}
	// No HTML escaping: what the grid showed is what the file holds.
	if !strings.Contains(string(data), `<a> & 'b'`) {
		t.Errorf("angle brackets must not be \\u-escaped:\n%s", data)
	}
	// Numbers stay strings — the Source hands over display text, and guessing
	// a type back would break a value that only looks numeric.
	if _, ok := got[0]["id"].(string); !ok {
		t.Errorf("values export as strings, got %#v", got[0]["id"])
	}
}

func TestExportFollowsTheFilterAndTheSort(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out := filepath.Join(t.TempDir(), "users.csv")
	res, err := ExportFile(context.Background(), src, "users",
		`WHERE id <= 4`, Sort{Column: "id", Desc: true}, out, 0)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if res.Rows != 4 {
		t.Fatalf("the export must hold the filtered rows, got %d", res.Rows)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 || !strings.HasPrefix(lines[1], "4,") || !strings.HasPrefix(lines[4], "1,") {
		t.Fatalf("the export must follow the grid's order:\n%s", data)
	}
}

func TestExportCapsAndSaysSo(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out := filepath.Join(t.TempDir(), "capped.csv")
	res, err := ExportFile(context.Background(), src, "users", "", Sort{}, out, 10)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if res.Rows != 10 || !res.Capped {
		t.Fatalf("ExportFile = %+v, want 10 capped rows", res)
	}
	// A limit the result exactly fills is not a cap: nothing was cut off.
	full, err := ExportFile(context.Background(), src, "users", `WHERE id <= 10`, Sort{}, out, 10)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if full.Rows != 10 || full.Capped {
		t.Fatalf("ExportFile = %+v, want 10 uncapped rows", full)
	}
}

func TestExportEmptyResultIsAValidFile(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "none.csv")
	if _, err := ExportFile(context.Background(), src, "users", `WHERE id < 0`, Sort{}, csvPath, 0); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	data, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "id,name,note,pic" {
		t.Fatalf("an empty result still owes the file its header, got %q", data)
	}
	jsonPath := filepath.Join(dir, "none.json")
	if _, err := ExportFile(context.Background(), src, "users", `WHERE id < 0`, Sort{}, jsonPath, 0); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	data, err = os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) != 0 {
		t.Fatalf("an empty export must be an empty JSON array, got %q (%v)", data, err)
	}
}

func TestExportRejectsAnUnknownExtension(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out := filepath.Join(t.TempDir(), "users.xlsx")
	if _, err := ExportFile(context.Background(), src, "users", "", Sort{}, out, 0); err == nil {
		t.Fatal("an extension neither format claims must be refused")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a rejected export must not create the file")
	}
	if _, err := FormatForPath("x.CSV"); err != nil {
		t.Fatalf("the extension match is case-insensitive: %v", err)
	}
	if f, _ := FormatForPath("x.json"); f != FormatJSON {
		t.Fatal(".json must pick the JSON format")
	}
}

func TestExportStopsOnCancel(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(t.TempDir(), "users.csv")
	if _, err := ExportFile(ctx, src, "users", "", Sort{}, out, 0); err == nil {
		t.Fatal("a cancelled export must report the cancellation")
	}
}

func TestDuckExportRoundTrips(t *testing.T) {
	src, err := OpenDuckDB(writeDuckFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out := filepath.Join(t.TempDir(), "users.json")
	res, err := ExportFile(context.Background(), src, "users", `WHERE id <= 3`, Sort{Column: "id", Desc: true}, out, 0)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if res.Rows != 3 {
		t.Fatalf("got %d rows, want 3", res.Rows)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("the export must parse as JSON: %v\n%s", err, data)
	}
	if rows[0]["id"] != "3" || rows[2]["id"] != "1" {
		t.Fatalf("the sort must reach the file, got %v", rows)
	}
	// The NULL note of row 1 travels as JSON null, not as "".
	if rows[2]["note"] != nil {
		t.Fatalf("a DuckDB NULL must export as null, got %#v", rows[2]["note"])
	}
}

func TestParquetExportRoundTrips(t *testing.T) {
	rows := make([]flatRow, 0, 20)
	for i := int64(1); i <= 20; i++ {
		rows = append(rows, flatRow{ID: i, Name: "user-" + itoa(int(i))})
	}
	src, err := OpenParquet(writeParquet(t, "t.parquet", rows))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	// Unfiltered and unsorted, the export rides the pure-Go reader: no duckdb
	// CLI is needed for a plain parquet export.
	out := filepath.Join(t.TempDir(), "t.csv")
	res, err := ExportFile(context.Background(), src, "t.parquet", "", Sort{}, out, 0)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if res.Rows != 20 {
		t.Fatalf("got %d rows, want 20", res.Rows)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if lines[0] != "id,name,note" || lines[1] != "1,user-1," || len(lines) != 21 {
		t.Fatalf("unexpected parquet export:\n%s", data)
	}
}
