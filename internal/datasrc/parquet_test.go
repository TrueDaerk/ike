package datasrc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

// flatRow is the fixture schema for the sniff, listing and paging tests.
type flatRow struct {
	ID   int64   `parquet:"id"`
	Name string  `parquet:"name"`
	Note *string `parquet:"note,optional"`
}

// writeParquet writes rows to a fresh file under t.TempDir and returns its
// path. Fixtures are generated here rather than committed, so the tests stay
// readable and no binary blob ages in the repo.
func writeParquet[T any](t *testing.T, name string, rows []T, opts ...parquet.WriterOption) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	w := parquet.NewGenericWriter[T](f, opts...)
	if _, err := w.Write(rows); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return path
}

func TestIsParquetSniff(t *testing.T) {
	good := writeParquet(t, "t.parquet", []flatRow{{ID: 1, Name: "a"}})
	if !IsParquet(good, nil) {
		t.Fatal("a written parquet file must sniff as parquet")
	}
	head, err := os.ReadFile(good)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !IsParquet(good, head[:8]) {
		t.Fatal("the handler's leading bytes must not change the verdict")
	}
	if !IsDatabase(good, head[:8]) {
		t.Fatal("the file handler's sniff must claim parquet files")
	}

	dir := t.TempDir()
	cases := []struct {
		name string
		data []byte
	}{
		{"text.txt", []byte("hello, world\n")},
		// The leading magic alone is not enough: the footer magic must close
		// the file too, or a text file starting with "PAR1" would be claimed.
		{"head-only.bin", []byte("PAR1 not really a parquet file at all")},
		{"tail-only.bin", []byte("not a parquet file at allPAR1")},
		{"tiny.bin", []byte("PAR1")},
		{"empty.bin", nil},
	}
	for _, tc := range cases {
		p := filepath.Join(dir, tc.name)
		if err := os.WriteFile(p, tc.data, 0o644); err != nil {
			t.Fatalf("write %s: %v", tc.name, err)
		}
		if IsParquet(p, tc.data) {
			t.Errorf("%s must not sniff as parquet", tc.name)
		}
	}
	if IsParquet(filepath.Join(dir, "missing.parquet"), nil) {
		t.Error("a missing file must not sniff as parquet")
	}
}

func TestParquetTablesAndOpenRouting(t *testing.T) {
	path := writeParquet(t, "people.parquet", []flatRow{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}})
	// Open must route by magic, without the extension deciding.
	src, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()
	tables, err := src.Tables()
	if err != nil {
		t.Fatalf("tables: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("a parquet file is one table, got %d", len(tables))
	}
	if tables[0].Name != "people.parquet" {
		t.Errorf("table name = %q, want the file's base name", tables[0].Name)
	}
	if tables[0].Rows != 2 {
		t.Errorf("rows = %d, want 2", tables[0].Rows)
	}
	if tables[0].IsView() {
		t.Error("a parquet table is not a view")
	}
}

func TestParquetSchemaView(t *testing.T) {
	rows := make([]flatRow, 4)
	for i := range rows {
		rows[i] = flatRow{ID: int64(i), Name: "n"}
	}
	path := writeParquet(t, "t.parquet", rows,
		parquet.MaxRowsPerRowGroup(2), parquet.Compression(&parquet.Snappy))
	src, err := OpenParquet(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()
	schema, err := src.Schema("t.parquet")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	for _, want := range []string{
		"id", "name", "note", // column names
		"INT64", "BYTE_ARRAY", // physical types
		"STRING",               // logical types
		"REQUIRED", "OPTIONAL", // nullability
		"4 rows", "2 row groups", "3 columns",
		"codec SNAPPY",
		"message ", // the native schema block
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema view lacks %q:\n%s", want, schema)
		}
	}
}

func TestParquetPagingWithoutFullLoad(t *testing.T) {
	const total = 1200
	rows := make([]flatRow, total)
	for i := range rows {
		rows[i] = flatRow{ID: int64(i), Name: "row"}
	}
	// Several row groups, so paging has to cross their boundaries.
	path := writeParquet(t, "big.parquet", rows, parquet.MaxRowsPerRowGroup(250))
	src, err := OpenParquet(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()

	const limit = 500
	seen := int64(0)
	for offset := int64(0); offset < total; offset += limit {
		page, err := src.Page("big.parquet", offset, limit)
		if err != nil {
			t.Fatalf("page at %d: %v", offset, err)
		}
		if page.Total != total {
			t.Errorf("total = %d, want %d", page.Total, total)
		}
		if page.Offset != offset {
			t.Errorf("offset = %d, want %d", page.Offset, offset)
		}
		want := int64(limit)
		if rest := total - offset; rest < want {
			want = rest
		}
		if int64(len(page.Rows)) != want {
			t.Fatalf("page at %d has %d rows, want %d", offset, len(page.Rows), want)
		}
		// Every page must start where the previous one ended: no overlap, no
		// skipped row.
		for i, row := range page.Rows {
			if got, want := row[0].Text, strconv.FormatInt(offset+int64(i), 10); got != want {
				t.Fatalf("row %d of page %d: id = %q, want %q", i, offset, got, want)
			}
		}
		seen += int64(len(page.Rows))
	}
	if seen != total {
		t.Errorf("paged over %d rows, want %d", seen, total)
	}

	// Past the end is an empty page, not an error — the pane clamps, but a
	// stale cursor must not break it.
	page, err := src.Page("big.parquet", total+10, limit)
	if err != nil {
		t.Fatalf("page past the end: %v", err)
	}
	if len(page.Rows) != 0 {
		t.Errorf("page past the end has %d rows, want 0", len(page.Rows))
	}
	if len(page.Columns) != 3 {
		t.Errorf("an empty page still names its %d columns, got %d", 3, len(page.Columns))
	}
}

func TestParquetNullCells(t *testing.T) {
	note := "here"
	path := writeParquet(t, "t.parquet", []flatRow{{ID: 1, Name: "a", Note: &note}, {ID: 2, Name: "", Note: nil}})
	src, err := OpenParquet(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()
	page, err := src.Page("t.parquet", 0, 10)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if page.Rows[0][2].Null || page.Rows[0][2].Text != "here" {
		t.Errorf("set optional = %+v, want the text", page.Rows[0][2])
	}
	if !page.Rows[1][2].Null {
		t.Error("an absent optional must be a NULL cell, not an empty string")
	}
	// The empty string is not NULL: the grid draws them differently.
	if page.Rows[1][1].Null || page.Rows[1][1].Text != "" {
		t.Errorf("empty string cell = %+v, want a non-null empty cell", page.Rows[1][1])
	}
}

// typedRow exercises the logical-type rendering rules.
type typedRow struct {
	Stamp  time.Time `parquet:"stamp,timestamp(millisecond)"`
	Micros time.Time `parquet:"micros,timestamp(microsecond)"`
	Day    int32     `parquet:"day,date"`
	Money  int64     `parquet:"money,decimal(2:9)"`
	Neg    int64     `parquet:"neg,decimal(3:9)"`
	Small  int32     `parquet:"small,decimal(4:6)"`
	Raw    []byte    `parquet:"raw"`
	Text   string    `parquet:"text"`
	Big    uint64    `parquet:"big,uint(64)"`
	Ratio  float64   `parquet:"ratio"`
	Yes    bool      `parquet:"yes"`
}

func TestParquetLogicalTypeRendering(t *testing.T) {
	stamp := time.Date(2024, 3, 1, 12, 34, 56, 789000000, time.UTC)
	path := writeParquet(t, "t.parquet", []typedRow{{
		Stamp:  stamp,
		Micros: stamp,
		Day:    19783, // 2024-03-01
		Money:  123456,
		Neg:    -42,
		Small:  5,
		Raw:    []byte{0xff, 0xfe, 0x00},
		Text:   "héllo",
		Big:    1 << 63,
		Ratio:  1.5,
		Yes:    true,
	}})
	src, err := OpenParquet(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()
	page, err := src.Page("t.parquet", 0, 1)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	got := map[string]string{}
	for i, c := range page.Columns {
		got[c] = page.Rows[0][i].Text
	}
	want := map[string]string{
		"stamp":  "2024-03-01T12:34:56.789Z",
		"micros": "2024-03-01T12:34:56.789Z",
		"day":    "2024-03-01",
		"money":  "1234.56",
		"neg":    "-0.042",
		"small":  "0.0005",
		"raw":    "<bytes 3>",
		"text":   "héllo",
		"big":    "9223372036854775808",
		"ratio":  "1.5",
		"yes":    "true",
	}
	for col, w := range want {
		if got[col] != w {
			t.Errorf("column %q rendered %q, want %q", col, got[col], w)
		}
	}
}

// nestedRow exercises list, map and struct rendering.
type nestedRow struct {
	Tags  []string          `parquet:"tags,list"`
	Attrs map[string]string `parquet:"attrs"`
	Inner struct {
		A int32     `parquet:"a"`
		B string    `parquet:"b"`
		T time.Time `parquet:"t,timestamp(millisecond)"`
	} `parquet:"inner"`
}

func TestParquetNestedRendering(t *testing.T) {
	var r nestedRow
	r.Tags = []string{"red", "blue"}
	r.Attrs = map[string]string{"k": "v"}
	r.Inner.A = 7
	r.Inner.B = "x"
	r.Inner.T = time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	var empty nestedRow
	empty.Tags = []string{}
	empty.Attrs = map[string]string{}

	path := writeParquet(t, "t.parquet", []nestedRow{r, empty})
	src, err := OpenParquet(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()
	page, err := src.Page("t.parquet", 0, 10)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if want := []string{"tags", "attrs", "inner"}; !equalStrings2(page.Columns, want) {
		t.Fatalf("columns = %v, want the top-level fields %v", page.Columns, want)
	}
	cells := page.Rows[0]
	if got, want := cells[0].Text, `["red","blue"]`; got != want {
		t.Errorf("list cell = %q, want %q", got, want)
	}
	if got, want := cells[1].Text, `{"k":"v"}`; got != want {
		t.Errorf("map cell = %q, want %q", got, want)
	}
	// The struct keeps its field names, and a nested timestamp renders by the
	// same rules as a top-level one.
	if got, want := cells[2].Text, `{"a":7,"b":"x","t":"2024-01-02T03:04:05Z"}`; got != want {
		t.Errorf("struct cell = %q, want %q", got, want)
	}
	if got, want := page.Rows[1][0].Text, `[]`; got != want {
		t.Errorf("empty list cell = %q, want %q", got, want)
	}
	if got, want := page.Rows[1][1].Text, `{}`; got != want {
		t.Errorf("empty map cell = %q, want %q", got, want)
	}
}

func TestParquetCorruptFileDegrades(t *testing.T) {
	good := writeParquet(t, "t.parquet", []flatRow{{ID: 1, Name: "a"}})
	data, err := os.ReadFile(good)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	dir := t.TempDir()
	cases := map[string][]byte{
		// Truncated mid-footer: the trailing magic is gone.
		"truncated.parquet": data[:len(data)/2],
		// Both magics present but the footer between them is nonsense.
		"garbled.parquet": append(append([]byte("PAR1"), make([]byte, 64)...), []byte("PAR1")...),
		"empty.parquet":   nil,
	}
	for name, content := range cases {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		src, err := Open(p)
		if err == nil {
			src.Close()
			t.Errorf("%s opened without an error; a corrupt file must degrade to a notice", name)
		}
	}
}

// equalStrings2 compares two string slices.
func equalStrings2(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return equalStrings(a, b)
}
