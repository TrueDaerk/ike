package testdata

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" database/sql driver
	"gopkg.in/yaml.v3"
)

// hostileDSL exercises the escaping every writer hand-rolls: names carrying
// a leading digit, a dash and a dot, and values covering all five value types
// including multi-line paragraph text and a template string full of grammar
// characters (quotes, commas, newlines, '=').
const hostileDSL = `id        = id()
full-name = full_name()
1st       = int(-50..50)
when      = date(2021-06-01..2021-06-30)
flag      = bool()
ratio     = float(0..1)
text      = paragraph()
site      = url(example.com)
qu.ote    = "he said \"hi, {full-name}\"\nthen left = done"
`

// hostileSpec wraps hostileDSL for one format.
func hostileSpec(format Format) Spec {
	return Spec{Format: format, Rows: 30, Seed: 1234, Table: "sample rows", DSL: hostileDSL}
}

// specNames parses the spec's field names — the header/keys a round-trip
// checks against.
func specNames(t *testing.T, spec Spec) []string {
	t.Helper()
	g, err := NewGenerator(spec)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	return g.Names()
}

// expectRows regenerates the spec's values so a round-trip can be compared
// against what the generator actually produced — the seed makes that exact.
func expectRows(t *testing.T, spec Spec) [][]any {
	t.Helper()
	g, err := NewGenerator(spec)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	out := make([][]any, spec.Rows)
	for i := range out {
		var err error
		if out[i], err = g.Row(i); err != nil {
			t.Fatalf("Row(%d): %v", i, err)
		}
	}
	return out
}

func render(t *testing.T, spec Spec) string {
	t.Helper()
	data, err := Render(spec)
	if err != nil {
		t.Fatalf("Render(%s): %v", spec.Format, err)
	}
	return string(data)
}

// TestCSVRoundTrip parses CSV and TSV back with encoding/csv.
func TestCSVRoundTrip(t *testing.T) {
	for _, f := range []Format{FormatCSV, FormatTSV} {
		t.Run(string(f), func(t *testing.T) {
			spec := hostileSpec(f).Normalized()
			want := expectRows(t, spec)
			r := csv.NewReader(strings.NewReader(render(t, spec)))
			r.Comma = ','
			if f == FormatTSV {
				r.Comma = '\t'
			}
			recs, err := r.ReadAll()
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(recs) != spec.Rows+1 {
				t.Fatalf("got %d records, want header + %d rows", len(recs), spec.Rows)
			}
			names := specNames(t, spec)
			for i, name := range names {
				if recs[0][i] != name {
					t.Fatalf("header[%d] = %q, want %q", i, recs[0][i], name)
				}
			}
			for r, rec := range recs[1:] {
				for c := range names {
					if got, w := rec[c], plainValue(want[r][c]); got != w {
						t.Fatalf("row %d col %d = %q, want %q", r, c, got, w)
					}
				}
			}
		})
	}
}

// TestJSONRoundTrip parses the array-of-objects output with encoding/json.
func TestJSONRoundTrip(t *testing.T) {
	spec := hostileSpec(FormatJSON).Normalized()
	want := expectRows(t, spec)
	var got []map[string]any
	if err := json.Unmarshal([]byte(render(t, spec)), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	checkObjects(t, spec, want, got)
}

// TestNDJSONRoundTrip parses one object per line.
func TestNDJSONRoundTrip(t *testing.T) {
	spec := hostileSpec(FormatNDJSON).Normalized()
	want := expectRows(t, spec)
	lines := strings.Split(strings.TrimRight(render(t, spec), "\n"), "\n")
	if len(lines) != spec.Rows {
		t.Fatalf("got %d lines, want %d", len(lines), spec.Rows)
	}
	got := make([]map[string]any, len(lines))
	for i, l := range lines {
		if err := json.Unmarshal([]byte(l), &got[i]); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
	}
	checkObjects(t, spec, want, got)
}

// checkObjects compares parsed JSON/YAML maps against the generated values.
func checkObjects(t *testing.T, spec Spec, want [][]any, got []map[string]any) {
	t.Helper()
	if len(got) != spec.Rows {
		t.Fatalf("got %d objects, want %d", len(got), spec.Rows)
	}
	names := specNames(t, spec)
	for r, obj := range got {
		if len(obj) != len(names) {
			t.Fatalf("row %d has %d keys, want %d", r, len(obj), len(names))
		}
		for c, name := range names {
			v, ok := obj[name]
			if !ok {
				t.Fatalf("row %d missing key %q", r, name)
			}
			if err := sameValue(want[r][c], v); err != nil {
				t.Fatalf("row %d key %q: %v", r, name, err)
			}
		}
	}
}

// sameValue compares a generated value with the one a document parser handed
// back, allowing for each parser's own numeric and date typing.
func sameValue(want, got any) error {
	switch w := want.(type) {
	case string:
		if s, ok := got.(string); !ok || s != w {
			return fmt.Errorf("got %#v, want string %q", got, w)
		}
	case bool:
		if b, ok := got.(bool); !ok || b != w {
			return fmt.Errorf("got %#v, want bool %v", got, w)
		}
	case int64:
		n, err := toFloat(got)
		if err != nil || n != float64(w) {
			return fmt.Errorf("got %#v, want int %d", got, w)
		}
	case float64:
		n, err := toFloat(got)
		if err != nil || n != w {
			return fmt.Errorf("got %#v, want float %v", got, w)
		}
	case time.Time:
		switch g := got.(type) {
		case string:
			if g != w.Format(timeLayout) {
				return fmt.Errorf("got %q, want %q", g, w.Format(timeLayout))
			}
		case time.Time:
			if !g.Equal(w) {
				return fmt.Errorf("got %s, want %s", g, w)
			}
		default:
			return fmt.Errorf("got %#v, want a date", got)
		}
	}
	return nil
}

// toFloat normalizes the numeric types the parsers return.
func toFloat(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int64:
		return float64(n), nil
	case int:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		return strconv.ParseFloat(n, 64)
	}
	return 0, fmt.Errorf("not a number: %#v", v)
}

// xmlDoc mirrors the generated document loosely: rows of arbitrary children,
// which is what lets the test see the element names the writer sanitized.
type xmlDoc struct {
	Rows []struct {
		Fields []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	} `xml:"row"`
}

// TestXMLRoundTrip parses the generated document with encoding/xml and checks
// the element names were sanitized into legal XML names.
func TestXMLRoundTrip(t *testing.T) {
	spec := hostileSpec(FormatXML).Normalized()
	want := expectRows(t, spec)
	var doc xmlDoc
	if err := xml.Unmarshal([]byte(render(t, spec)), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Rows) != spec.Rows {
		t.Fatalf("got %d rows, want %d", len(doc.Rows), spec.Rows)
	}
	names := specNames(t, spec)
	for r, row := range doc.Rows {
		if len(row.Fields) != len(names) {
			t.Fatalf("row %d has %d elements, want %d", r, len(row.Fields), len(names))
		}
		for c, fname := range names {
			if name, w := row.Fields[c].XMLName.Local, xmlName(fname, "x"); name != w {
				t.Fatalf("row %d element %d = %q, want %q", r, c, name, w)
			}
			if got, w := row.Fields[c].Value, plainValue(want[r][c]); got != w {
				t.Fatalf("row %d element %d = %q, want %q", r, c, got, w)
			}
		}
	}
}

// TestYAMLRoundTrip parses the list of maps with yaml.v3.
func TestYAMLRoundTrip(t *testing.T) {
	spec := hostileSpec(FormatYAML).Normalized()
	want := expectRows(t, spec)
	var got []map[string]any
	if err := yaml.Unmarshal([]byte(render(t, spec)), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	checkObjects(t, spec, want, got)
}

// TestTOMLRoundTrip parses the array of tables with BurntSushi/toml, including
// the native datetime typing of the date kind.
func TestTOMLRoundTrip(t *testing.T) {
	spec := hostileSpec(FormatTOML).Normalized()
	want := expectRows(t, spec)
	var doc map[string][]map[string]any
	if _, err := toml.Decode(render(t, spec), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows, ok := doc[spec.Table]
	if !ok {
		t.Fatalf("no [[%s]] tables in %v", spec.Table, keysOf(doc))
	}
	checkObjects(t, spec, want, rows)
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestSQLRoundTrip executes the generated INSERTs against a real in-memory
// SQLite database — the only honest way to check identifier and literal
// quoting — and reads the values back.
func TestSQLRoundTrip(t *testing.T) {
	spec := hostileSpec(FormatSQL).Normalized()
	want := expectRows(t, spec)

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	fnames := specNames(t, spec)
	cols := make([]string, len(fnames))
	for i, n := range fnames {
		cols[i] = sqlIdent(n) + " TEXT"
	}
	if _, err := db.Exec("CREATE TABLE " + sqlIdent(spec.Table) + " (" + strings.Join(cols, ", ") + ")"); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, line := range strings.Split(render(t, spec), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if _, err := db.Exec(line); err != nil {
			t.Fatalf("exec %q: %v", line, err)
		}
	}

	names := make([]string, len(fnames))
	for i, n := range fnames {
		names[i] = sqlIdent(n)
	}
	rows, err := db.Query("SELECT " + strings.Join(names, ", ") + " FROM " + sqlIdent(spec.Table) + " ORDER BY rowid")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		cells := make([]string, len(fnames))
		ptrs := make([]any, len(cells))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		for c := range fnames {
			w := sqlPlain(want[n][c])
			if cells[c] != w {
				t.Fatalf("row %d col %d = %q, want %q", n, c, cells[c], w)
			}
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if n != spec.Rows {
		t.Fatalf("inserted %d rows, want %d", n, spec.Rows)
	}
}

// sqlPlain is the text a value has once it went through the SQL writer and
// came back out of a TEXT column: quotes stripped, newlines already flattened.
func sqlPlain(v any) string {
	lit := sqlValue(v)
	if strings.HasPrefix(lit, "'") {
		return strings.ReplaceAll(strings.Trim(lit, "'"), "''", "'")
	}
	return lit
}

// TestLogRoundTrip parses the logfmt lines back and checks the ts/level/msg
// triple plus the spec's fields.
func TestLogRoundTrip(t *testing.T) {
	spec := hostileSpec(FormatLog).Normalized()
	lines := strings.Split(strings.TrimRight(render(t, spec), "\n"), "\n")
	if len(lines) != spec.Rows {
		t.Fatalf("got %d lines, want %d", len(lines), spec.Rows)
	}
	// The log writer draws ts/level/msg from the same faker as the field
	// values, so the expectation has to replay the writer's exact draw order:
	// the row first, then the entry.
	g, err := NewGenerator(spec)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	names := g.Names()
	var prev time.Time
	for i, line := range lines {
		want, err := g.Row(i)
		if err != nil {
			t.Fatalf("Row(%d): %v", i, err)
		}
		wantTS, wantLevel, wantMsg := g.logEntry()
		pairs, err := parseLogfmt(line)
		if err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		ts, err := time.Parse(logTimeLayout, pairs["ts"])
		if err != nil {
			t.Fatalf("line %d ts %q: %v", i, pairs["ts"], err)
		}
		if !ts.Equal(wantTS.Truncate(time.Millisecond)) {
			t.Fatalf("line %d ts = %s, want %s", i, ts, wantTS)
		}
		if !ts.After(prev) {
			t.Fatalf("line %d timestamp %s did not advance", i, ts)
		}
		prev = ts
		if pairs["level"] != wantLevel {
			t.Fatalf("line %d level = %q, want %q", i, pairs["level"], wantLevel)
		}
		if pairs["msg"] != wantMsg {
			t.Fatalf("line %d msg = %q, want %q", i, pairs["msg"], wantMsg)
		}
		for c, fname := range names {
			key := logfmtKey(fname)
			got, ok := pairs[key]
			if !ok {
				t.Fatalf("line %d missing key %q", i, key)
			}
			if w := plainValue(want[c]); got != w {
				t.Fatalf("line %d key %q = %q, want %q", i, key, got, w)
			}
		}
	}
}

// parseLogfmt is a minimal logfmt reader — enough to prove the writer's
// quoting is machine-readable without pulling in a parser dependency.
func parseLogfmt(line string) (map[string]string, error) {
	out := map[string]string{}
	i, n := 0, len(line)
	for i < n {
		for i < n && line[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}
		eq := strings.IndexByte(line[i:], '=')
		if eq < 0 {
			return nil, fmt.Errorf("no '=' in %q", line[i:])
		}
		key := line[i : i+eq]
		i += eq + 1
		if i < n && line[i] == '"' {
			end := i + 1
			for end < n {
				if line[end] == '\\' {
					end += 2
					continue
				}
				if line[end] == '"' {
					break
				}
				end++
			}
			if end >= n {
				return nil, fmt.Errorf("unterminated quote in %q", line)
			}
			v, err := strconv.Unquote(line[i : end+1])
			if err != nil {
				return nil, err
			}
			out[key] = v
			i = end + 1
			continue
		}
		end := strings.IndexByte(line[i:], ' ')
		if end < 0 {
			out[key] = line[i:]
			break
		}
		out[key] = line[i : i+end]
		i += end
	}
	return out, nil
}

// TestRowCountHonored checks every writer emits exactly the requested number
// of rows, at both ends of the plausible range.
func TestRowCountHonored(t *testing.T) {
	for _, f := range Formats() {
		for _, rows := range []int{1, 7} {
			spec := Spec{Format: f, Rows: rows, Seed: 5, Table: "t", DSL: "id = id()\nname = first_name()"}
			out := render(t, spec)
			if got := countRows(f, out); got != rows {
				t.Fatalf("%s with %d rows produced %d", f, rows, got)
			}
		}
	}
}

// countRows counts a rendered document's rows per format, using each format's
// own row marker.
func countRows(f Format, out string) int {
	switch f {
	case FormatCSV, FormatTSV:
		return strings.Count(strings.TrimRight(out, "\n"), "\n") // header not counted
	case FormatJSON:
		var rows []map[string]any
		if json.Unmarshal([]byte(out), &rows) != nil {
			return -1
		}
		return len(rows)
	case FormatNDJSON, FormatLog:
		return len(strings.Split(strings.TrimRight(out, "\n"), "\n"))
	case FormatXML:
		return strings.Count(out, "<row>")
	case FormatYAML:
		var rows []map[string]any
		if yaml.Unmarshal([]byte(out), &rows) != nil {
			return -1
		}
		return len(rows)
	case FormatTOML:
		return strings.Count(out, "[[")
	case FormatSQL:
		return strings.Count(out, "INSERT INTO ")
	}
	return -1
}
