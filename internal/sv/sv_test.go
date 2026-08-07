package sv

import (
	"reflect"
	"testing"
)

func TestFieldsSplitsOnSeparator(t *testing.T) {
	got := Fields("a,bb,ccc", ',')
	want := []Field{{0, 1}, {2, 4}, {5, 8}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fields = %v, want %v", got, want)
	}
}

func TestFieldsQuotedSeparatorLiteral(t *testing.T) {
	got := Fields(`"a,b",c`, ',')
	want := []Field{{0, 5}, {6, 7}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fields = %v, want %v", got, want)
	}
	// An escaped quote inside quotes stays inside the field.
	got = Fields(`"a""b,c",d`, ',')
	want = []Field{{0, 8}, {9, 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("escaped-quote Fields = %v, want %v", got, want)
	}
}

func TestFieldsTrailingAndEmpty(t *testing.T) {
	if got := Fields("a,", ','); !reflect.DeepEqual(got, []Field{{0, 1}, {2, 2}}) {
		t.Fatalf("trailing separator Fields = %v", got)
	}
	if got := Fields("", ','); !reflect.DeepEqual(got, []Field{{0, 0}}) {
		t.Fatalf("empty line Fields = %v", got)
	}
}

// TestIndexAtLocatesColumn: a separator column belongs to the field it closes,
// a column past the line end to the last field (#1659).
func TestIndexAtLocatesColumn(t *testing.T) {
	const line = "a,bb,ccc" // fields [0,1) [2,4) [5,8), separators at 1 and 4
	for _, tc := range []struct{ col, want int }{
		{0, 0}, {1, 0}, {2, 1}, {3, 1}, {4, 1}, {5, 2}, {7, 2}, {99, 2},
	} {
		if got := IndexAt(line, ',', tc.col); got != tc.want {
			t.Errorf("IndexAt(col %d) = %d, want %d", tc.col, got, tc.want)
		}
	}
	// A quoted separator does not start a new column.
	if got := IndexAt(`"a,b",c`, ',', 3); got != 0 {
		t.Errorf("quoted separator column = %d, want 0", got)
	}
}

// TestHeaderNamesFirstRow: the header heuristic and its unquoting (#1659).
func TestHeaderNamesFirstRow(t *testing.T) {
	names, ok := Header(`name,"qty, total"," x "`, ',')
	if !ok {
		t.Fatalf("header row rejected")
	}
	if want := []string{"name", "qty, total", "x"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("Header = %v, want %v", names, want)
	}
	if names, ok := Header(`a,"b""c"`, ','); !ok || names[1] != `b"c` {
		t.Fatalf("escaped quote in header = %v (ok %v)", names, ok)
	}
}

// TestHeaderRejectsDataRow: a numeric or empty field means the first row is
// data, so callers fall back to the bare column index (#1659).
func TestHeaderRejectsDataRow(t *testing.T) {
	for _, line := range []string{"apple,3", "a,,b", "1,2", "", "   ", "-1.5e3,x"} {
		if names, ok := Header(line, ','); ok {
			t.Errorf("Header(%q) accepted as header: %v", line, names)
		}
	}
}

func TestSeparatorFixedPerLanguage(t *testing.T) {
	if got := Separator("tsv", []string{"a;b,c"}); got != '\t' {
		t.Fatalf("tsv separator = %q", got)
	}
	if got := Separator("psv", nil); got != '|' {
		t.Fatalf("psv separator = %q", got)
	}
}

func TestSeparatorSniffsSemicolon(t *testing.T) {
	if got := Separator("csv", []string{"a;b;c", "1;2;3"}); got != ';' {
		t.Fatalf("semicolon csv separator = %q", got)
	}
	if got := Separator("csv", []string{"a,b", "1,2"}); got != ',' {
		t.Fatalf("comma csv separator = %q", got)
	}
	// Commas inside quotes do not count for the sniff.
	if got := Separator("csv", []string{`"a,b,c";d`, `"1,2,3";4`}); got != ';' {
		t.Fatalf("quoted-comma csv separator = %q", got)
	}
}
