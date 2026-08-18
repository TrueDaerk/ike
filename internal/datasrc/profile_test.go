package datasrc

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// profile_test.go covers the column profile (#1940) on every backend: the SQL
// aggregates (SQLite, DuckDB), the bounded scan (Parquet, csv text), and the
// rendering both surfaces share.

// topOf returns the count the ranking recorded for value v ("" — the NULL
// group), and whether v is in the ranking at all.
func topOf(p Profile, v string, null bool) (int64, bool) {
	for _, t := range p.Top {
		if t.Value.Null == null && (null || t.Value.Text == v) {
			return t.Count, true
		}
	}
	return 0, false
}

func TestSQLiteProfileTextColumn(t *testing.T) {
	src, err := OpenSQLite(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	// note is "" on every third row and NULL everywhere else — the one shape
	// that proves the profile keeps empty and NULL apart.
	p, err := src.Profile(context.Background(), "users", "note", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows != 1200 {
		t.Fatalf("rows = %d, want 1200", p.Rows)
	}
	if p.Nulls != 800 {
		t.Fatalf("nulls = %d, want 800", p.Nulls)
	}
	if p.Empties != 400 {
		t.Fatalf("empties = %d, want 400", p.Empties)
	}
	if p.Distinct != 1 {
		t.Fatalf("distinct = %d, want 1 (the empty string)", p.Distinct)
	}
	if p.Numeric || p.HasMean {
		t.Fatal("a column of empty strings is not numeric")
	}
	if !p.HasLen || p.MinLen != 0 || p.MaxLen != 0 {
		t.Fatalf("length range = %d–%d (has=%v), want 0–0", p.MinLen, p.MaxLen, p.HasLen)
	}
	if n, ok := topOf(p, "", false); !ok || n != 400 {
		t.Fatalf("top values must rank the empty string 400 times: %+v", p.Top)
	}
	if n, ok := topOf(p, "", true); !ok || n != 800 {
		t.Fatalf("top values must rank NULL as its own group: %+v", p.Top)
	}
}

func TestSQLiteProfileNumericColumnHasMean(t *testing.T) {
	src, err := OpenSQLite(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	p, err := src.Profile(context.Background(), "users", "id", "")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Numeric || !p.HasMean {
		t.Fatalf("id must profile as numeric with a mean: %+v", p)
	}
	if p.Min.Text != "1" || p.Max.Text != "1200" {
		t.Fatalf("min/max = %q/%q, want 1/1200", p.Min.Text, p.Max.Text)
	}
	if p.Mean != 600.5 {
		t.Fatalf("mean = %v, want 600.5", p.Mean)
	}
	if p.Distinct != 1200 || p.Nulls != 0 {
		t.Fatalf("distinct/nulls = %d/%d, want 1200/0", p.Distinct, p.Nulls)
	}
	if p.HasLen {
		t.Fatal("a numeric column reports a mean, not a length range")
	}
}

func TestSQLiteProfileHonoursTheFilterClause(t *testing.T) {
	src, err := OpenSQLite(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	p, err := src.Profile(context.Background(), "users", "id", "WHERE id <= 10")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows != 10 || p.Max.Text != "10" {
		t.Fatalf("a filtered profile must describe the filtered rows: %+v", p)
	}
	if p.Filter != "WHERE id <= 10" {
		t.Fatalf("the profile must carry its clause, got %q", p.Filter)
	}
	// The read-only contract holds against the clause here too.
	if _, err := src.Profile(context.Background(), "users", "id", "WHERE 1=1; DROP TABLE users"); err != ErrMultiStatement {
		t.Fatalf("a second statement must be refused, got %v", err)
	}
}

func TestSQLiteProfileEmptyTableAndCancel(t *testing.T) {
	src, err := OpenSQLite(writeFixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	p, err := src.Profile(context.Background(), "empty", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows != 0 || p.Distinct != 0 || len(p.Top) != 0 {
		t.Fatalf("an empty table profiles to zeros: %+v", p)
	}
	if p.HasMean || p.HasLen || p.Numeric {
		t.Fatalf("no values, no type-specific extras: %+v", p)
	}
	// No values means no extremes — not "the smallest value is NULL".
	if text := p.Text(); !strings.Contains(text, "min       —") {
		t.Fatalf("an empty column has no min:\n%s", text)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Profile(ctx, "users", "id", ""); err == nil {
		t.Fatal("a cancelled context must abort the profile")
	}
}

func TestDuckProfileMatchesTheSQLiteShape(t *testing.T) {
	p := writeDuckFixture(t)
	src, err := OpenDuckDB(p)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	// note: NULL once, '' on every third row, 'n<i>' otherwise.
	prof, err := src.Profile(context.Background(), "users", "note", "")
	if err != nil {
		t.Fatal(err)
	}
	if prof.Rows != 1200 || prof.Nulls != 1 || prof.Empties != 400 {
		t.Fatalf("rows/nulls/empties = %d/%d/%d, want 1200/1/400", prof.Rows, prof.Nulls, prof.Empties)
	}
	if prof.Numeric {
		t.Fatal("a column of 'n<i>' strings is not numeric")
	}
	if !prof.HasLen || prof.MinLen != 0 {
		t.Fatalf("text columns report their length range: %+v", prof)
	}
	if n, ok := topOf(prof, "", false); !ok || n != 400 {
		t.Fatalf("the empty string must top the ranking: %+v", prof.Top)
	}

	num, err := src.Profile(context.Background(), "users", "id", "")
	if err != nil {
		t.Fatal(err)
	}
	if !num.Numeric || !num.HasMean || num.Mean != 600.5 {
		t.Fatalf("id must profile as numeric with mean 600.5: %+v", num)
	}
	if num.Min.Text != "1" || num.Max.Text != "1200" {
		t.Fatalf("min/max = %q/%q, want 1/1200", num.Min.Text, num.Max.Text)
	}

	filtered, err := src.Profile(context.Background(), "users", "id", "WHERE id <= 10")
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Rows != 10 || filtered.Max.Text != "10" {
		t.Fatalf("a filtered profile must describe the filtered rows: %+v", filtered)
	}
}

func TestParquetProfileScansTheColumn(t *testing.T) {
	note := "n"
	rows := []flatRow{
		{ID: 3, Name: "b", Note: &note},
		{ID: 1, Name: "a"},
		{ID: 2, Name: "b"},
	}
	src, err := OpenParquet(writeParquet(t, "p.parquet", rows))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	p, err := src.Profile(context.Background(), "p.parquet", "name", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows != 3 || p.Distinct != 2 || p.Nulls != 0 {
		t.Fatalf("rows/distinct/nulls = %d/%d/%d, want 3/2/0", p.Rows, p.Distinct, p.Nulls)
	}
	if p.Min.Text != "a" || p.Max.Text != "b" {
		t.Fatalf("min/max = %q/%q, want a/b", p.Min.Text, p.Max.Text)
	}
	if n, ok := topOf(p, "b", false); !ok || n != 2 {
		t.Fatalf("the ranking must count b twice: %+v", p.Top)
	}
	if p.Capped {
		t.Fatal("a three-row file is not capped")
	}

	// The optional column is NULL on two of the three rows — absent, not "".
	nulls, err := src.Profile(context.Background(), "p.parquet", "note", "")
	if err != nil {
		t.Fatal(err)
	}
	if nulls.Nulls != 2 || nulls.Empties != 0 || nulls.Distinct != 1 {
		t.Fatalf("nulls/empties/distinct = %d/%d/%d, want 2/0/1", nulls.Nulls, nulls.Empties, nulls.Distinct)
	}

	// An int64 column comes back as a numeric profile even though the reader
	// hands the scan rendered text.
	num, err := src.Profile(context.Background(), "p.parquet", "id", "")
	if err != nil {
		t.Fatal(err)
	}
	if !num.Numeric || num.Mean != 2 || num.Min.Text != "1" || num.Max.Text != "3" {
		t.Fatalf("id must profile numerically: %+v", num)
	}

	if _, err := src.Profile(context.Background(), "p.parquet", "nope", ""); err == nil {
		t.Fatal("an unknown column must error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Profile(ctx, "p.parquet", "name", ""); err == nil {
		t.Fatal("a cancelled context must abort the scan")
	}
}

func TestParquetProfileUnderAFilterGoesThroughDuckDB(t *testing.T) {
	duckBin(t) // the filtered profile is the one path that needs the CLI
	rows := []flatRow{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 3, Name: "b"}}
	src, err := OpenParquet(writeParquet(t, "p.parquet", rows))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	p, err := src.Profile(context.Background(), "p.parquet", "id", "WHERE id >= 2")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows != 2 || p.Min.Text != "2" || p.Max.Text != "3" {
		t.Fatalf("filtered profile = %+v, want 2 rows over 2–3", p)
	}
	if !p.Numeric || !p.HasMean || p.Mean != 2.5 {
		t.Fatalf("id must profile numerically under a filter: %+v", p)
	}
	if p.Capped {
		t.Fatal("the SQL path is exact, never capped")
	}
}

func TestCSVProfileCountsEmptyAndMissingApart(t *testing.T) {
	lines := []string{
		`name,qty,note`,
		`a,1,"x"`,
		`b,2,`, // empty note
		`c,3`,  // no note field at all: missing, not empty
		``,     // a blank line is not a row
		`a,4,y`,
	}
	p := ProfileCSV("data.csv", "note", lines, ',', 2, true)
	if p.Rows != 4 {
		t.Fatalf("rows = %d, want 4 (the blank line is no row)", p.Rows)
	}
	if p.Nulls != 1 {
		t.Fatalf("nulls = %d, want 1 (the short row)", p.Nulls)
	}
	if p.Empties != 1 {
		t.Fatalf("empties = %d, want 1", p.Empties)
	}
	if p.Distinct != 3 {
		t.Fatalf("distinct = %d, want 3 ('', x, y)", p.Distinct)
	}
	if p.Capped {
		t.Fatal("a six-line buffer is not capped")
	}
	// Quotes are stripped like a column name's are.
	if n, ok := topOf(p, "x", false); !ok || n != 1 {
		t.Fatalf("a quoted field profiles unquoted: %+v", p.Top)
	}

	// The header row is a header, not a value.
	names := ProfileCSV("data.csv", "name", lines, ',', 0, true)
	if names.Rows != 4 || names.Distinct != 3 {
		t.Fatalf("rows/distinct = %d/%d, want 4/3", names.Rows, names.Distinct)
	}
	if n, ok := topOf(names, "a", false); !ok || n != 2 {
		t.Fatalf("a must rank twice: %+v", names.Top)
	}
	if names.Numeric {
		t.Fatal("a column of names is not numeric")
	}
	if !names.HasLen || names.MinLen != 1 || names.MaxLen != 1 {
		t.Fatalf("length range = %d–%d, want 1–1", names.MinLen, names.MaxLen)
	}
}

func TestCSVProfileNumericColumnToleratesGaps(t *testing.T) {
	lines := []string{
		"qty",
		"10",
		"",   // blank line: no row
		"2",  //
		"",   // (kept out on purpose — see the explicit empty field below)
		",",  // an empty field, not a number
		"30", //
	}
	p := ProfileCSV("data.csv", "qty", lines, ',', 0, true)
	if !p.Numeric || !p.HasMean {
		t.Fatalf("a numeric column with an empty cell stays numeric: %+v", p)
	}
	if p.Mean != 14 {
		t.Fatalf("mean = %v, want 14 ((10+2+30)/3)", p.Mean)
	}
	if p.Min.Text != "2" || p.Max.Text != "30" {
		t.Fatalf("min/max = %q/%q, want 2/30 — numbers compare numerically", p.Min.Text, p.Max.Text)
	}
	if p.Empties != 1 {
		t.Fatalf("empties = %d, want 1", p.Empties)
	}

	// One word demotes the whole column to text, and then min/max are
	// lexicographic again.
	mixed := ProfileCSV("data.csv", "qty", append(lines, "n/a"), ',', 0, true)
	if mixed.Numeric || mixed.HasMean {
		t.Fatalf("a column with a word in it is not numeric: %+v", mixed)
	}
	if mixed.Min.Text != "" || mixed.Max.Text != "n/a" {
		t.Fatalf("min/max = %q/%q, want ''/n/a", mixed.Min.Text, mixed.Max.Text)
	}
}

func TestCSVProfileStatesItsRowCap(t *testing.T) {
	lines := make([]string, 0, ProfileLimit+11)
	lines = append(lines, "n")
	for i := 0; i < ProfileLimit+10; i++ {
		lines = append(lines, strconv.Itoa(i%7))
	}
	p := ProfileCSV("big.csv", "n", lines, ',', 0, true)
	if !p.Capped {
		t.Fatal("a scan past the limit must be marked capped")
	}
	if p.Rows != ProfileLimit || p.Scanned != ProfileLimit {
		t.Fatalf("rows/scanned = %d/%d, want %d", p.Rows, p.Scanned, int64(ProfileLimit))
	}
	text := p.Text()
	if !strings.Contains(text, "first 100000 rows only") {
		t.Fatalf("the rendered profile must state its cap:\n%s", text)
	}
}

func TestProfileLinesRenderTheTypeSpecificExtras(t *testing.T) {
	num := Profile{
		Table: "t", Column: "qty", Rows: 4, Nulls: 1, Distinct: 3,
		Min: Cell{Text: "1"}, Max: Cell{Text: "9"},
		Numeric: true, Mean: 4.5, HasMean: true,
		Top: []TopValue{{Value: Cell{Text: "1"}, Count: 2}, {Value: Cell{Null: true}, Count: 1}},
	}
	text := num.Text()
	for _, want := range []string{"column: qty (numeric)", "mean", "4.5", "rows      4", "nulls     1 (25.0%)", "NULL"} {
		if !strings.Contains(text, want) {
			t.Fatalf("numeric profile missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "length") {
		t.Fatalf("a numeric profile has no length range:\n%s", text)
	}

	txt := Profile{
		Table: "t", Column: "name", Rows: 2, Distinct: 2,
		Min: Cell{Text: "a"}, Max: Cell{Text: "bbb"},
		MinLen: 1, MaxLen: 3, HasLen: true,
	}
	out := txt.Text()
	if !strings.Contains(out, "column: name (text)") || !strings.Contains(out, "length    1–3") {
		t.Fatalf("text profile missing its length range:\n%s", out)
	}
	if strings.Contains(out, "mean") {
		t.Fatalf("a text profile has no mean:\n%s", out)
	}
	if strings.Contains(out, "filter:") {
		t.Fatalf("an unfiltered profile must not claim a filter:\n%s", out)
	}
}
