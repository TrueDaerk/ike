package app

import (
	"os"
	"strings"
	"testing"

	"ike/internal/jqplay"
)

// playgroundopen_csv_test.go covers the CSV input adapter's routing (#2791):
// playground.open on a csv/tsv buffer or a CSV-typed response opens the *jq*
// playground over the rows as an array of objects, and following the source
// file re-reads it as rows.

func TestPlaygroundOpenCSVOpensJQOverRows(t *testing.T) {
	for _, tc := range []struct {
		lang, body string
	}{
		{"csv", "name,age\nada,36\nlinus,54\n"},
		{"csv", "name;age\nada;36,5\nlinus;54\n"}, // semicolon, sniffed
		{"tsv", "name\tage\nada\t36\nlinus\t54\n"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			m := openDispatcher(t, dispatchApp(t, tc.lang, tc.body))
			if !m.playOpen() {
				t.Fatalf("playground.open must open a playground for %s", tc.lang)
			}
			if m.play.dialect != jqplay.DialectJQ || !m.play.csv {
				t.Fatalf("%s opened dialect %v (csv=%v), want jq over the CSV adapter", tc.lang, m.play.dialect, m.play.csv)
			}
			if m.play.inputErr != "" {
				t.Fatalf("input error %q", m.play.inputErr)
			}
			m = setProgram(m, ".[0].name")
			if got := m.play.result.Text(); got != `"ada"` {
				t.Errorf(".[0].name = %q, want \"ada\"", got)
			}
			if seg := m.playInputSegment(); !strings.Contains(seg, "csv rows: 2") {
				t.Errorf("info row input segment = %q, want the csv row count", seg)
			}
		})
	}
}

func TestPlaygroundOpenCSVMalformedRowIsInputError(t *testing.T) {
	m := openDispatcher(t, dispatchApp(t, "csv", "name,age\nada,36\nlinus\n"))
	if !m.playOpen() {
		t.Fatal("a malformed CSV still opens the playground, with the error on its info row")
	}
	if !strings.Contains(m.play.inputErr, "line 3") || !strings.Contains(m.play.inputErr, "CSV") {
		t.Errorf("inputErr = %q, want a CSV error naming line 3", m.play.inputErr)
	}
}

func TestPlaygroundOpenCSVResponse(t *testing.T) {
	for _, tc := range []struct{ name, ct, body string }{
		{"csv sniffed semicolon", "text/csv; charset=utf-8", "name;age\nada;36\n"},
		{"tsv", "text/tab-separated-values", "name\tage\nada\t36\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := openDispatcher(t, filledHTTP(t, typedResponse(tc.ct, tc.body)))
			if !m.playOpen() || !m.play.csv {
				t.Fatalf("%s response: want the jq playground over the CSV adapter", tc.name)
			}
			m = setProgram(m, ".[0].age")
			if got := m.play.result.Text(); got != `"36"` {
				t.Errorf(".[0].age = %q, want the string \"36\"", got)
			}
		})
	}
}

func TestPlaygroundCSVFollowsExternalChange(t *testing.T) {
	m := dispatchApp(t, "csv", "name\nada\n")
	path := m.activeEditor().Path()
	m = openDispatcher(t, m)
	m = setProgram(m, "length")
	if got := m.play.result.Text(); got != "1" {
		t.Fatalf("length = %q, want 1", got)
	}
	m = playExternalWrite(t, m, path, "name\nada\nlinus\n")
	if got := m.play.result.Text(); got != "2" {
		t.Errorf("after the external change length = %q, want 2 (re-read as rows)", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

// TestPlaygroundJSONIsNotCSV: the adapter is only for the separator-delimited
// languages — a JSON buffer keeps the plain JSON parse.
func TestPlaygroundJSONIsNotCSV(t *testing.T) {
	m := openDispatcher(t, dispatchApp(t, "json", `{"a":1}`))
	if !m.playOpen() || m.play.csv {
		t.Fatal("a JSON buffer must open jq without the CSV adapter")
	}
}
