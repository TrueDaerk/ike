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
