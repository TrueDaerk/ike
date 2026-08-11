package datasrc

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestCheckClauseRejectsSecondStatement(t *testing.T) {
	bad := []string{
		`WHERE id > 3; DROP TABLE users`,
		`; DELETE FROM users`,
		`WHERE id > 3;`,
		"WHERE name = 'x' -- a comment\n; DROP TABLE users",
	}
	for _, clause := range bad {
		if err := checkClause(clause); !errors.Is(err, ErrMultiStatement) {
			t.Errorf("checkClause(%q) = %v, want ErrMultiStatement", clause, err)
		}
	}
	// A separator that only lives inside a literal, an identifier or a
	// comment is part of the filter, not a second statement.
	good := []string{
		`WHERE note = 'a;b'`,
		`WHERE note = 'it''s; fine' ORDER BY id DESC LIMIT 10`,
		`WHERE "od;d" = 1`,
		`WHERE id > 3 -- ; not a statement`,
		`WHERE id > 3 /* ; still not */ ORDER BY id`,
		``,
	}
	for _, clause := range good {
		if err := checkClause(clause); err != nil {
			t.Errorf("checkClause(%q) = %v, want nil", clause, err)
		}
	}
	// An unterminated literal or comment would swallow the wrapper's closing
	// parenthesis, so it is refused before the engine sees it.
	for _, clause := range []string{`WHERE note = 'open`, `WHERE id > 1 /* open`} {
		if err := checkClause(clause); err == nil {
			t.Errorf("checkClause(%q) must reject an unterminated literal/comment", clause)
		}
	}
}

func TestFilteredQueryWrapsTrailingComment(t *testing.T) {
	q := filteredQuery(`SELECT * FROM "t"`, `WHERE id > 1 -- mine`, 500, 100)
	if !strings.Contains(q, "\n) AS "+filterAlias) {
		t.Fatalf("the closing parenthesis must start a new line, got:\n%s", q)
	}
	if !strings.HasSuffix(q, "LIMIT 100 OFFSET 500") {
		t.Fatalf("the pane's window must sit outside the clause, got:\n%s", q)
	}
}

func TestSQLitePageWhereFiltersAndPages(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	// 400 even ids among the 1200 rows, newest first: two pages of 250.
	const clause = `WHERE id % 3 = 0 ORDER BY id`
	var seen []string
	for offset := int64(0); ; offset += 250 {
		page, err := src.PageWhere("users", clause, offset, 250)
		if err != nil {
			t.Fatalf("filtered page at %d: %v", offset, err)
		}
		// The filtered total is counted in the background too (#1795).
		if page.Total != -1 {
			t.Fatalf("filtered page total = %d, want it uncounted", page.Total)
		}
		if page.Offset != offset {
			t.Fatalf("offset = %d, want %d", page.Offset, offset)
		}
		if len(page.Rows) == 0 {
			break
		}
		for _, row := range page.Rows {
			seen = append(seen, row[0].Text)
		}
	}
	if len(seen) != 400 {
		t.Fatalf("paged over %d filtered rows, want 400", len(seen))
	}
	// No duplicates, no gaps: the ids are exactly 3, 6, … 1200 in order.
	for i, got := range seen {
		if want := strconv.Itoa((i + 1) * 3); got != want {
			t.Fatalf("filtered row %d = %q, want %q", i, got, want)
		}
	}
}

func TestSQLitePageWhereUserLimitIsPagedNotFought(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	// The user's own LIMIT bounds the whole result; the pane still pages it.
	page, err := src.PageWhere("users", `ORDER BY id DESC LIMIT 10`, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	// The user's LIMIT bounds the count as well — which Count reports, the
	// page itself never counting (#1795).
	if n, err := src.Count("users", `ORDER BY id DESC LIMIT 10`); err != nil || n != 10 {
		t.Fatalf("count under a user LIMIT = %d (%v), want 10", n, err)
	}
	if len(page.Rows) != 4 || page.Rows[0][0].Text != "1200" {
		t.Fatalf("first filtered page = %d rows starting at %q", len(page.Rows), page.Rows[0][0].Text)
	}
	last, err := src.PageWhere("users", `ORDER BY id DESC LIMIT 10`, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Rows) != 2 || last.Rows[0][0].Text != "1192" {
		t.Fatalf("last filtered page = %+v", last.Rows)
	}
}

func TestSQLitePageWhereEmptyClauseIsThePlainPage(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	plain, err := src.Page("users", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := src.PageWhere("users", "   ", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != plain.Total || len(filtered.Rows) != len(plain.Rows) {
		t.Fatalf("an empty clause must page like the plain table: %+v vs %+v", filtered, plain)
	}
}

func TestSQLitePageWhereBrokenClauseErrors(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := src.PageWhere("users", `WHERE nope >`, 0, 10); err == nil {
		t.Fatal("a syntax error must come back as the engine's error")
	}
	// The grid can still page after a rejected clause: nothing was mutated.
	if _, err := src.Page("users", 0, 10); err != nil {
		t.Fatalf("the source stays usable after a broken clause: %v", err)
	}
}

func TestSQLitePageWhereStaysReadOnly(t *testing.T) {
	p := writeFixtureDB(t)
	src, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	// A clause carrying a second statement never reaches the engine.
	if _, err := src.PageWhere("users", `WHERE id = 1; DROP TABLE users`, 0, 10); !errors.Is(err, ErrMultiStatement) {
		t.Fatalf("a multi-statement clause = %v, want ErrMultiStatement", err)
	}
	// And the connection itself refuses a write even if one got through: the
	// DSN is mode=ro.
	s, ok := src.(*sqliteSource)
	if !ok {
		t.Fatalf("expected the sqlite backend, got %T", src)
	}
	if _, err := s.db.Exec(`DROP TABLE users`); err == nil {
		t.Fatal("the read-only connection must refuse a write")
	}
	// The table is untouched, seen through a fresh writable connection.
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int64
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("the table must still exist: %v", err)
	}
	if n != 1200 {
		t.Fatalf("users holds %d rows, want 1200", n)
	}
}

func TestSQLiteFilterPrefixQuotesTheTable(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if got, want := src.FilterPrefix(`od"d`), `SELECT * FROM "od""d" `; got != want {
		t.Fatalf("FilterPrefix = %q, want %q", got, want)
	}
}

func TestDuckPageWhereFiltersAndPages(t *testing.T) {
	src, err := Open(writeDuckFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	const clause = `WHERE id % 3 = 0 ORDER BY id`
	var seen []string
	for offset := int64(0); ; offset += 250 {
		page, err := src.PageWhere("users", clause, offset, 250)
		if err != nil {
			t.Fatalf("filtered page at %d: %v", offset, err)
		}
		// The filtered total is counted in the background too (#1795).
		if page.Total != -1 {
			t.Fatalf("filtered page total = %d, want it uncounted", page.Total)
		}
		if len(page.Rows) == 0 {
			break
		}
		for _, row := range page.Rows {
			seen = append(seen, row[0].Text)
		}
	}
	if len(seen) != 400 {
		t.Fatalf("paged over %d filtered rows, want 400", len(seen))
	}
	for i, got := range seen {
		if want := strconv.Itoa((i + 1) * 3); got != want {
			t.Fatalf("filtered row %d = %q, want %q", i, got, want)
		}
	}
}

func TestDuckPageWhereRejectsAndErrors(t *testing.T) {
	src, err := Open(writeDuckFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	// The CLI runs a whole script from one argument, so the separator is
	// refused here rather than trusted to the engine.
	if _, err := src.PageWhere("users", `WHERE id = 1; DROP TABLE users`, 0, 10); !errors.Is(err, ErrMultiStatement) {
		t.Fatalf("a multi-statement clause = %v, want ErrMultiStatement", err)
	}
	if _, err := src.PageWhere("users", `WHERE nope >`, 0, 10); err == nil {
		t.Fatal("a syntax error must come back as the engine's error")
	}
	// The table survived both, and the grid still pages.
	if _, err := src.Page("users", 0, 10); err != nil {
		t.Fatalf("the source stays usable: %v", err)
	}
	if n, err := src.Count("users", ""); err != nil || n != 1200 {
		t.Fatalf("users holds %d rows (%v), want 1200", n, err)
	}
	if got, want := src.FilterPrefix("users"), `SELECT * FROM "users" `; got != want {
		t.Fatalf("FilterPrefix = %q, want %q", got, want)
	}
}

func TestParquetPageWhereFiltersThroughDuckDB(t *testing.T) {
	duckBin(t) // filtering a parquet table is the one path that needs the CLI
	const total = 600
	rows := make([]flatRow, total)
	for i := range rows {
		rows[i] = flatRow{ID: int64(i), Name: "row-" + strconv.Itoa(i)}
	}
	path := writeParquet(t, "big.parquet", rows, parquet.MaxRowsPerRowGroup(250))
	src, err := OpenParquet(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	const clause = `WHERE id >= 500 ORDER BY id`
	page, err := src.PageWhere("big.parquet", clause, 0, 60)
	if err != nil {
		t.Fatalf("filtered page: %v", err)
	}
	// The page itself does not count (#1795) — one CLI invocation, not two.
	if page.Total != -1 {
		t.Fatalf("filtered page total = %d, want it uncounted", page.Total)
	}
	if n, err := src.Count("big.parquet", clause); err != nil || n != 100 {
		t.Fatalf("filtered count = %d (%v), want 100", n, err)
	}
	if len(page.Rows) != 60 || page.Rows[0][0].Text != "500" {
		t.Fatalf("first filtered page = %d rows starting at %q", len(page.Rows), page.Rows[0][0].Text)
	}
	if len(page.Columns) != 3 || page.Columns[0] != "id" {
		t.Fatalf("filtered columns = %v", page.Columns)
	}
	rest, err := src.PageWhere("big.parquet", clause, 60, 60)
	if err != nil {
		t.Fatalf("second filtered page: %v", err)
	}
	if len(rest.Rows) != 40 || rest.Rows[0][0].Text != "560" {
		t.Fatalf("second filtered page = %d rows starting at %q", len(rest.Rows), rest.Rows[0][0].Text)
	}
	// The file is untouched by the query and the pure-Go reader still pages.
	if _, err := src.Page("big.parquet", 0, 10); err != nil {
		t.Fatalf("the reader stays usable after a filter: %v", err)
	}
	if _, err := src.PageWhere("big.parquet", `WHERE id = 1; DROP TABLE x`, 0, 10); !errors.Is(err, ErrMultiStatement) {
		t.Fatal("a multi-statement clause must be refused before the CLI")
	}
	if _, err := src.PageWhere("big.parquet", `WHERE nope >`, 0, 10); err == nil {
		t.Fatal("a syntax error must come back as the engine's error")
	}
}

func TestParquetFilterWithoutDuckDBIsActionable(t *testing.T) {
	saved := lookDuckDB
	lookDuckDB = func() (string, error) {
		return "", &MissingToolError{Tool: "duckdb", Why: "reading DuckDB databases", Hints: []string{"brew install duckdb"}}
	}
	defer func() { lookDuckDB = saved }()
	path := writeParquet(t, "small.parquet", []flatRow{{ID: 1, Name: "a"}})
	src, err := OpenParquet(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	// The unfiltered grid never needs the CLI …
	if _, err := src.Page("small.parquet", 0, 10); err != nil {
		t.Fatalf("plain paging must not need duckdb: %v", err)
	}
	// … only the filter does, and it says so.
	_, err = src.PageWhere("small.parquet", `WHERE id = 1`, 0, 10)
	var missing *MissingToolError
	if !errors.As(err, &missing) {
		t.Fatalf("filter without the CLI = %v, want a MissingToolError", err)
	}
	if !strings.Contains(missing.Why, "filtering") {
		t.Fatalf("the message must say only filtering needs the tool: %q", missing.Why)
	}
}
