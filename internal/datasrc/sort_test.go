package datasrc

// sort_test.go covers the column sort (#2248) per backend: the order is the
// engine's ORDER BY, it survives paging, and it composes with a filter the
// user wrote — including one that already ends in an ORDER BY or a LIMIT.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSortOrderByQuotesTheColumn(t *testing.T) {
	if got := (Sort{}).orderBy(); got != "" {
		t.Fatalf("an inactive sort renders no ORDER BY, got %q", got)
	}
	if got := (Sort{Column: "name"}).orderBy(); got != ` ORDER BY "name" ASC` {
		t.Fatalf("orderBy = %q", got)
	}
	if got := (Sort{Column: "name", Desc: true}).orderBy(); got != ` ORDER BY "name" DESC` {
		t.Fatalf("orderBy = %q", got)
	}
	// A column name is a result column, not free text — but it is quoted all
	// the same, so an embedded quote cannot escape the identifier.
	if got := (Sort{Column: `od"d`}).orderBy(); got != ` ORDER BY "od""d" ASC` {
		t.Fatalf("a hostile column name must stay inside its quotes, got %q", got)
	}
}

func TestFilteredQueryPutsTheSortOutsideTheClause(t *testing.T) {
	q := filteredQuery(`SELECT * FROM "t"`, `WHERE id > 1 LIMIT 10`, Sort{Column: "name", Desc: true}, 0, 5)
	iAlias := strings.Index(q, ") AS "+filterAlias)
	iOrder := strings.Index(q, "ORDER BY \"name\"")
	if iAlias < 0 || iOrder < iAlias {
		t.Fatalf("the sort must follow the subquery, got:\n%s", q)
	}
	if !strings.HasSuffix(q, "LIMIT 5 OFFSET 0") {
		t.Fatalf("the pane's window must still sit last, got:\n%s", q)
	}
}

// firstColumnValues reads one column of a page as plain strings.
func firstColumnValues(page Page, col int) []string {
	out := make([]string, 0, len(page.Rows))
	for _, r := range page.Rows {
		out = append(out, r[col].Text)
	}
	return out
}

// assertSortedPages walks the first two pages of a descending sort on `id`
// and checks that the order holds across the page boundary — the whole point
// of putting the ORDER BY outside the pane's LIMIT/OFFSET window.
func assertSortedPages(t *testing.T, src Source, table string, total int64) {
	t.Helper()
	sort := Sort{Column: "id", Desc: true}
	first, err := src.PageWhere(table, "", sort, 0, 5)
	if err != nil {
		t.Fatalf("PageWhere: %v", err)
	}
	want := []string{"1200", "1199", "1198", "1197", "1196"}
	if got := firstColumnValues(first, 0); !equalStrings2(got, want) {
		t.Fatalf("descending first page = %v, want %v", got, want)
	}
	second, err := src.PageWhere(table, "", sort, 5, 5)
	if err != nil {
		t.Fatalf("PageWhere page 2: %v", err)
	}
	want = []string{"1195", "1194", "1193", "1192", "1191"}
	if got := firstColumnValues(second, 0); !equalStrings2(got, want) {
		t.Fatalf("descending second page = %v, want %v", got, want)
	}
	asc, err := src.PageWhere(table, "", Sort{Column: "id"}, 0, 3)
	if err != nil {
		t.Fatalf("ascending PageWhere: %v", err)
	}
	if got := firstColumnValues(asc, 0); !equalStrings2(got, []string{"1", "2", "3"}) {
		t.Fatalf("ascending first page = %v", got)
	}
	// The sort changes nothing about the row count.
	n, err := src.Count(table, "")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != total {
		t.Fatalf("Count = %d, want %d", n, total)
	}
}

func TestSQLiteSortPagesAndComposesWithTheFilter(t *testing.T) {
	src, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	assertSortedPages(t, src, "users", 1200)

	// A filter narrows, the sort orders what is left.
	page, err := src.PageWhere("users", `WHERE id <= 10`, Sort{Column: "id", Desc: true}, 0, 3)
	if err != nil {
		t.Fatalf("filtered sort: %v", err)
	}
	if got := firstColumnValues(page, 0); !equalStrings2(got, []string{"10", "9", "8"}) {
		t.Fatalf("filtered descending = %v", got)
	}
	// A clause that already ends in ORDER BY … LIMIT still composes: the
	// user's LIMIT bounds the set, the pane's sort orders it.
	page, err = src.PageWhere("users", `WHERE id <= 10 ORDER BY id DESC LIMIT 3`, Sort{Column: "id"}, 0, 5)
	if err != nil {
		t.Fatalf("sort over a bounded clause: %v", err)
	}
	if got := firstColumnValues(page, 0); !equalStrings2(got, []string{"8", "9", "10"}) {
		t.Fatalf("sort over `ORDER BY id DESC LIMIT 3` = %v, want the last three ascending", got)
	}
	// The read-only contract still holds against the sorted path.
	if _, err := src.PageWhere("users", `WHERE id = 1; DROP TABLE users`, Sort{Column: "id"}, 0, 1); err == nil {
		t.Fatal("a second statement must be refused under a sort too")
	}
}

func TestDuckSortPagesAndComposesWithTheFilter(t *testing.T) {
	src, err := OpenDuckDB(writeDuckFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	assertSortedPages(t, src, "users", 1200)

	page, err := src.PageWhere("users", `WHERE id <= 10`, Sort{Column: "id", Desc: true}, 0, 3)
	if err != nil {
		t.Fatalf("filtered sort: %v", err)
	}
	if got := firstColumnValues(page, 0); !equalStrings2(got, []string{"10", "9", "8"}) {
		t.Fatalf("filtered descending = %v", got)
	}
	if _, err := src.PageWhere("users", `WHERE id = 1; DROP TABLE users`, Sort{Column: "id"}, 0, 1); err == nil {
		t.Fatal("a second statement must be refused under a sort too")
	}
}

func TestParquetSortRunsThroughDuckDB(t *testing.T) {
	duckBin(t) // the parquet sort borrows the CLI, like the filter does
	rows := make([]flatRow, 0, 100)
	for i := int64(1); i <= 100; i++ {
		rows = append(rows, flatRow{ID: i, Name: "user-" + itoa(int(i))})
	}
	path := writeParquet(t, "big.parquet", rows)
	src, err := OpenParquet(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	page, err := src.PageWhere("big.parquet", "", Sort{Column: "id", Desc: true}, 0, 3)
	if err != nil {
		t.Fatalf("sorted page: %v", err)
	}
	if got := firstColumnValues(page, 0); !equalStrings2(got, []string{"100", "99", "98"}) {
		t.Fatalf("descending parquet page = %v", got)
	}
	next, err := src.PageWhere("big.parquet", "", Sort{Column: "id", Desc: true}, 3, 3)
	if err != nil {
		t.Fatalf("second sorted page: %v", err)
	}
	if got := firstColumnValues(next, 0); !equalStrings2(got, []string{"97", "96", "95"}) {
		t.Fatalf("second descending parquet page = %v", got)
	}
	// The file itself is untouched by any of it.
	info, err := os.Stat(filepath.Clean(path))
	if err != nil || info.Size() == 0 {
		t.Fatalf("the parquet file must survive the sort: %v", err)
	}
}
