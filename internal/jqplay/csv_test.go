package jqplay

import (
	"context"
	"strings"
	"testing"
)

// csv_test.go covers the CSV/TSV input adapter (#2791): rows become an array
// of objects keyed by the header, cells stay strings, the separator is sniffed
// when no language names it, and a malformed row names its line.

// runCSV parses text through the adapter and runs program over it.
func runCSV(t *testing.T, text string, sep rune, program string) Result {
	t.Helper()
	in, err := ParseCSV(text, sep)
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	return Run(context.Background(), program, in)
}

func TestCSVRowsAsObjects(t *testing.T) {
	text := "name,age\nada,36\nlinus,007\n"
	if got := runCSV(t, text, ',', ".[0].name").Text(); got != `"ada"` {
		t.Errorf(".[0].name = %s, want \"ada\"", got)
	}
	// No coercion: the zeros of "007" survive, and the number is a string.
	if got := runCSV(t, text, ',', ".[1].age").Text(); got != `"007"` {
		t.Errorf(".[1].age = %s, want the string \"007\"", got)
	}
	if got := runCSV(t, text, ',', "length").Text(); got != "2" {
		t.Errorf("length = %s, want 2 rows", got)
	}
	in, _ := ParseCSV(text, ',')
	if in.Origin() != "csv rows: 2" {
		t.Errorf("Origin = %q, want csv rows: 2", in.Origin())
	}
	if in.Len() != 1 || in.Dialect() != DialectJQ {
		t.Errorf("the rows must be one jq value, got %d values in %v", in.Len(), in.Dialect())
	}
}

func TestCSVSniffsSeparator(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       rune
	}{
		{"comma", "a,b\n1,2\n", ','},
		{"semicolon", "a;b\n1,5;2,5\n", ';'},
		{"tab", "a\tb\n1\t2\n", '\t'},
		{"pipe", "a|b\n1|2\n", '|'},
		{"quoted comma ignored", "\"x,y\";b\n1;2\n", ';'},
		{"single column", "name\nada\n", ','},
		{"bom", bom + "a;b\n1;2\n", ';'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SniffSeparator(tc.text); got != tc.want {
				t.Errorf("SniffSeparator = %q, want %q", got, tc.want)
			}
			if got := runCSV(t, tc.text, 0, ".[0] | keys | length").Text(); got == "" {
				t.Errorf("sniffed parse produced no output")
			}
		})
	}
	if got := runCSV(t, "a;b\n1,5;2,5\n", 0, ".[0].b").Text(); got != `"2,5"` {
		t.Errorf("semicolon file: .[0].b = %s, want \"2,5\"", got)
	}
	if got := runCSV(t, bom+"name\tage\nada\t36\n", 0, ".[0].name").Text(); got != `"ada"` {
		t.Errorf("TSV with BOM: .[0].name = %s, want \"ada\"", got)
	}
}

func TestCSVExplicitSeparatorWins(t *testing.T) {
	// A TSV whose cells hold commas: the language's tab beats the sniff.
	if got := runCSV(t, "a\tb\n1,2,3\t4\n", '\t', ".[0].a").Text(); got != `"1,2,3"` {
		t.Errorf(".[0].a = %s, want \"1,2,3\"", got)
	}
}

func TestCSVMalformedRowNamesLine(t *testing.T) {
	_, err := ParseCSV("name,age\nada,36\nlinus\n", ',')
	if err == nil {
		t.Fatal("a short row must be an input error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "line 3") || !strings.HasPrefix(msg, "input is not valid CSV") {
		t.Errorf("error = %q, want a CSV error naming line 3", msg)
	}
	_, err = ParseCSV("a,b\n\"open,2\n", ',')
	if err == nil || !strings.Contains(err.Error(), "line") {
		t.Errorf("broken quote error = %v, want one naming a line", err)
	}
}

func TestCSVEdges(t *testing.T) {
	if _, err := ParseCSV("  \n", 0); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty buffer error = %v", err)
	}
	if got := runCSV(t, "a,b\n", ',', ".").Text(); got != "[]" {
		t.Errorf("header only = %s, want []", got)
	}
	if got := runCSV(t, "id,id\n1,2\n", ',', ".[0] | keys").Text(); !strings.Contains(got, `"id_2"`) {
		t.Errorf("duplicate header keys = %s, want id_2", got)
	}
	if got := runCSV(t, "a,b\r\n1,2\r\n", ',', ".[0].b").Text(); got != `"2"` {
		t.Errorf("CRLF: .[0].b = %s", got)
	}
}
