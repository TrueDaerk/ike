package langsql

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func optsSpaces4() sqlOptions { return sqlOptions{Indent: "    ", Case: caseUpper} }

// TestSQLGolden pins the layout contract with golden files: for every
// testdata/*.sql the formatted output must equal the matching *.golden, and
// formatting the golden again must reproduce it (idempotency).
func TestSQLGolden(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("testdata", "*.sql"))
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no golden inputs: %v", err)
	}
	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".sql")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := formatSQL(string(src), optsSpaces4())
			if err != nil {
				t.Fatalf("format: %v", err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
			}
			again, err := formatSQL(got, optsSpaces4())
			if err != nil || again != got {
				t.Fatalf("not idempotent (err=%v)\n--- first ---\n%s--- second ---\n%s", err, got, again)
			}
		})
	}
}

// TestSQLKeywordCasing: upper (default), lower and preserve; identifiers,
// strings and quoted identifiers are never re-cased.
func TestSQLKeywordCasing(t *testing.T) {
	src := `Select Name, "MiXeD" from Users where Name = 'John Select';`
	upper, err := formatSQL(src, sqlOptions{Indent: "  ", Case: caseUpper})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(upper, "SELECT Name,") || !strings.Contains(upper, "FROM Users") || !strings.Contains(upper, "WHERE Name = 'John Select'") {
		t.Fatalf("upper:\n%s", upper)
	}
	if !strings.Contains(upper, `"MiXeD"`) {
		t.Fatalf("quoted identifier must keep its case:\n%s", upper)
	}
	lower, _ := formatSQL(src, sqlOptions{Indent: "  ", Case: caseLower})
	if !strings.Contains(lower, "select Name,") || !strings.Contains(lower, "from Users") {
		t.Fatalf("lower:\n%s", lower)
	}
	preserve, _ := formatSQL(src, sqlOptions{Indent: "  ", Case: casePreserve})
	if !strings.Contains(preserve, "Select Name,") || !strings.Contains(preserve, "from Users") {
		t.Fatalf("preserve:\n%s", preserve)
	}
}

// TestSQLMalformedUntouched: lexer-level breakage (unbalanced parens,
// unterminated string) and parse errors (CGo builds) refuse to format.
func TestSQLMalformedUntouched(t *testing.T) {
	for _, src := range []string{
		"select (a from t;",
		"select 'oops from t;",
		"select /* nope",
	} {
		if _, err := formatSQL(src, optsSpaces4()); err == nil {
			t.Fatalf("malformed %q must refuse", src)
		}
	}
}

// TestSQLIndentStyle: the indent unit follows the options (tabs vs. spaces).
func TestSQLIndentStyle(t *testing.T) {
	src := "select a, b from t;"
	tabs, err := formatSQL(src, sqlOptions{Indent: "\t", Case: caseUpper})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tabs, "\n\tb\n") {
		t.Fatalf("tab indent expected:\n%q", tabs)
	}
	two, _ := formatSQL(src, sqlOptions{Indent: "  ", Case: caseUpper})
	if !strings.Contains(two, "\n  b\n") {
		t.Fatalf("2-space indent expected:\n%q", two)
	}
}

// TestSQLStatementSeparation: exactly one blank line between statements, the
// terminator kept.
func TestSQLStatementSeparation(t *testing.T) {
	got, err := formatSQL("select 1;select 2;\n\n\n\nselect 3;", optsSpaces4())
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT 1;\n\nSELECT 2;\n\nSELECT 3;\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestSQLCommentsPreserved: standalone comments keep their own line with the
// following statement, trailing comments stay trailing.
func TestSQLCommentsPreserved(t *testing.T) {
	src := "-- header\nselect a -- picks a\nfrom t; -- done\n\n/* next */\nselect 2;"
	got, err := formatSQL(src, optsSpaces4())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-- header\nSELECT a -- picks a\n", "FROM t; -- done", "/* next */\nSELECT 2;"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// TestSQLFormatRange: only the statements overlapping the selection are
// reformatted; the rest of the buffer stays byte-identical.
func TestSQLFormatRange(t *testing.T) {
	src := "select   1;\n\nselect a,b from t where x=1 and y=2;\n\nselect   3;"
	got, err := formatRangeSQL(src, 2, 2, optsSpaces4())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "select   1;\n") || !strings.HasSuffix(got, "select   3;") {
		t.Fatalf("unselected statements must stay untouched:\n%s", got)
	}
	if !strings.Contains(got, "SELECT a,\n    b\nFROM t\nWHERE x = 1\n    AND y = 2;") {
		t.Fatalf("selected statement must format:\n%s", got)
	}
}

// TestSQLFormatRangeIdempotent: range-formatting the already-formatted span
// again changes nothing.
func TestSQLFormatRangeIdempotent(t *testing.T) {
	src := "select   1;\n\nselect a,b from t;\n\nselect   3;"
	once, err := formatRangeSQL(src, 2, 2, optsSpaces4())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(once, "\n")
	first, last := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "SELECT a") {
			first = i
		}
		if firstIdx := first; firstIdx >= 0 && strings.HasSuffix(l, ";") && i >= firstIdx && last < 0 && strings.Contains(l, "t;") {
			last = i
		}
	}
	if first < 0 || last < 0 {
		t.Fatalf("cannot locate formatted span in:\n%s", once)
	}
	twice, err := formatRangeSQL(once, first, last, optsSpaces4())
	if err != nil || twice != once {
		t.Fatalf("range not idempotent (err=%v)\n--- once ---\n%s\n--- twice ---\n%s", err, once, twice)
	}
}
