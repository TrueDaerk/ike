package testdata

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// write.go renders a spec into every supported format. The writers are
// **streaming** — begin, one call per row, end — so a million-row CSV costs a
// 64 KiB buffer rather than a million rows held in memory, and so the same
// row values feed every format unchanged.
//
// Each writer escapes for its own grammar rather than leaning on a document
// encoder, because a document encoder wants the whole file in memory first.
// The round-trip tests parse every writer's output with the real parser
// (encoding/csv, encoding/json, encoding/xml, yaml.v3, BurntSushi/toml,
// SQLite) precisely to keep the hand-rolled escaping honest.

// timeLayout is how a date value renders in the text formats.
const timeLayout = time.RFC3339

// logTimeLayout keeps milliseconds — a log stream without sub-second
// resolution reads as if everything happened at once.
const logTimeLayout = "2006-01-02T15:04:05.000Z07:00"

// sqlTimeLayout is the portable SQL datetime literal.
const sqlTimeLayout = "2006-01-02 15:04:05"

// bufSize is the streaming buffer; big enough that a wide row never splits
// into many small writes.
const bufSize = 64 * 1024

// Write generates the spec and renders it to w. An invalid spec is refused
// before anything is written.
func Write(w io.Writer, spec Spec) error {
	g, err := NewGenerator(spec)
	if err != nil {
		return err
	}
	fw, err := writerFor(g.Spec().Format)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(w, bufSize)
	if err := fw.begin(bw, g); err != nil {
		return err
	}
	for i := 0; i < g.Spec().Rows; i++ {
		vals, err := g.Row(i)
		if err != nil {
			return err
		}
		if err := fw.row(bw, g, i, vals); err != nil {
			return err
		}
	}
	if err := fw.end(bw, g); err != nil {
		return err
	}
	return bw.Flush()
}

// Render is Write into a byte slice — what the scratch creation path uses,
// since a scratch is written whole anyway.
func Render(spec Spec) ([]byte, error) {
	var buf bytes.Buffer
	if err := Write(&buf, spec); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PreviewRows caps the dialog's live preview.
const PreviewRows = 5

// Preview renders the spec capped to PreviewRows — the dialog's live preview.
// It runs the very same parser, evaluator and writers as Render, so what the
// preview shows is byte-for-byte what a generation with the same seed writes.
func Preview(spec Spec) ([]byte, error) {
	if spec.Rows > PreviewRows {
		spec.Rows = PreviewRows
	}
	return Render(spec)
}

// formatWriter is one render target's streaming encoder.
type formatWriter interface {
	begin(bw *bufio.Writer, g *Generator) error
	row(bw *bufio.Writer, g *Generator, idx int, vals []any) error
	end(bw *bufio.Writer, g *Generator) error
}

// writerFor builds a fresh writer for the format; fresh because a writer may
// hold per-run state (the CSV encoder, the SQL column list).
func writerFor(f Format) (formatWriter, error) {
	switch f {
	case FormatCSV:
		return &sepWriter{comma: ','}, nil
	case FormatTSV:
		return &sepWriter{comma: '\t'}, nil
	case FormatJSON:
		return &jsonWriter{}, nil
	case FormatNDJSON:
		return &ndjsonWriter{}, nil
	case FormatXML:
		return &xmlWriter{}, nil
	case FormatYAML:
		return &yamlWriter{}, nil
	case FormatTOML:
		return &tomlWriter{}, nil
	case FormatSQL:
		return &sqlWriter{}, nil
	case FormatLog:
		return &logWriter{}, nil
	}
	return nil, fmt.Errorf("unknown format %q", string(f))
}

// ------------------------------------------------------------------ CSV/TSV

// sepWriter renders CSV and TSV through encoding/csv, which owns the quoting
// rules for both.
type sepWriter struct {
	comma rune
	w     *csv.Writer
	rec   []string
}

func (s *sepWriter) begin(bw *bufio.Writer, g *Generator) error {
	s.w = csv.NewWriter(bw)
	s.w.Comma = s.comma
	s.rec = append([]string(nil), g.Names()...)
	return s.w.Write(s.rec)
}

func (s *sepWriter) row(_ *bufio.Writer, _ *Generator, _ int, vals []any) error {
	for i, v := range vals {
		s.rec[i] = plainValue(v)
	}
	return s.w.Write(s.rec)
}

func (s *sepWriter) end(*bufio.Writer, *Generator) error {
	s.w.Flush()
	return s.w.Error()
}

// plainValue renders a value as bare text — the CSV/TSV and XML idiom.
func plainValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return t.Format(timeLayout)
	}
	return fmt.Sprint(v)
}

// --------------------------------------------------------------------- JSON

// jsonWriter renders an array of objects, indented, one key per line — the
// shape JSON folding and the jq playground are exercised with.
type jsonWriter struct{}

func (jsonWriter) begin(bw *bufio.Writer, _ *Generator) error {
	_, err := bw.WriteString("[\n")
	return err
}

func (jsonWriter) row(bw *bufio.Writer, g *Generator, idx int, vals []any) error {
	if idx > 0 {
		if _, err := bw.WriteString(",\n"); err != nil {
			return err
		}
	}
	if _, err := bw.WriteString("  {\n"); err != nil {
		return err
	}
	names := g.Names()
	for i, name := range names {
		sep := ",\n"
		if i == len(names)-1 {
			sep = "\n"
		}
		if _, err := bw.WriteString("    " + jsonString(name) + ": " + jsonValue(vals[i]) + sep); err != nil {
			return err
		}
	}
	_, err := bw.WriteString("  }")
	return err
}

func (jsonWriter) end(bw *bufio.Writer, g *Generator) error {
	if g.Spec().Rows > 0 {
		if _, err := bw.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err := bw.WriteString("]\n")
	return err
}

// ndjsonWriter renders one compact object per line.
type ndjsonWriter struct{}

func (ndjsonWriter) begin(*bufio.Writer, *Generator) error { return nil }

func (ndjsonWriter) row(bw *bufio.Writer, g *Generator, _ int, vals []any) error {
	if err := bw.WriteByte('{'); err != nil {
		return err
	}
	for i, name := range g.Names() {
		if i > 0 {
			if err := bw.WriteByte(','); err != nil {
				return err
			}
		}
		if _, err := bw.WriteString(jsonString(name) + ":" + jsonValue(vals[i])); err != nil {
			return err
		}
	}
	_, err := bw.WriteString("}\n")
	return err
}

func (ndjsonWriter) end(*bufio.Writer, *Generator) error { return nil }

// jsonValue renders a value as a JSON literal. Dates become RFC3339 strings —
// JSON has no date type, and a string keeps every consumer happy.
func jsonValue(v any) string {
	switch t := v.(type) {
	case string:
		return jsonString(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return jsonString(t.Format(timeLayout))
	}
	return jsonString(fmt.Sprint(v))
}

// jsonString quotes a string the way encoding/json does, minus the HTML
// escaping — generated sample data is not embedded in a web page, and
// < in every URL would only obscure it.
func jsonString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// ---------------------------------------------------------------------- XML

// xmlWriter renders a root element holding one <row> per generated row.
type xmlWriter struct{}

func (xmlWriter) begin(bw *bufio.Writer, g *Generator) error {
	_, err := bw.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<" + xmlName(g.Spec().Table, "records") + ">\n")
	return err
}

func (xmlWriter) row(bw *bufio.Writer, g *Generator, _ int, vals []any) error {
	if _, err := bw.WriteString("  <row>\n"); err != nil {
		return err
	}
	for i, n := range g.Names() {
		name := xmlName(n, "field"+strconv.Itoa(i+1))
		if _, err := bw.WriteString("    <" + name + ">" + xmlEscape(plainValue(vals[i])) + "</" + name + ">\n"); err != nil {
			return err
		}
	}
	_, err := bw.WriteString("  </row>\n")
	return err
}

func (xmlWriter) end(bw *bufio.Writer, g *Generator) error {
	_, err := bw.WriteString("</" + xmlName(g.Spec().Table, "records") + ">\n")
	return err
}

// xmlName turns a field or table name into a legal XML element name: letters,
// digits, '-', '_' and '.', never starting with a digit. A name that reduces
// to nothing falls back to the positional default, so a field called "1" or
// "€" still produces a parseable document.
func xmlName(s, fallback string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == "-" {
		return fallback
	}
	if c := out[0]; c >= '0' && c <= '9' || c == '-' || c == '.' {
		out = "_" + out
	}
	return out
}

// xmlEscape escapes character data. encoding/xml's EscapeText is the
// authority; it also turns newlines into &#xA; which keeps a multi-line
// paragraph on one line of the document.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return ""
	}
	return buf.String()
}

// --------------------------------------------------------------------- YAML

// yamlWriter renders a list of maps.
type yamlWriter struct{}

func (yamlWriter) begin(*bufio.Writer, *Generator) error { return nil }

func (yamlWriter) row(bw *bufio.Writer, g *Generator, _ int, vals []any) error {
	for i, name := range g.Names() {
		lead := "  "
		if i == 0 {
			lead = "- "
		}
		if _, err := bw.WriteString(lead + yamlKey(name) + ": " + yamlValue(vals[i]) + "\n"); err != nil {
			return err
		}
	}
	return nil
}

func (yamlWriter) end(bw *bufio.Writer, g *Generator) error {
	if g.Spec().Rows == 0 {
		_, err := bw.WriteString("[]\n")
		return err
	}
	return nil
}

// yamlKey quotes a key unless it is a plain, unambiguous scalar.
func yamlKey(s string) string {
	if plainYAML(s) {
		return s
	}
	return jsonString(s)
}

// yamlValue renders a value. Strings are always double-quoted — YAML's
// double-quoted scalar uses JSON's escape rules, which sidesteps the whole
// plain-scalar minefield (a value like "yes", "3.14" or "- x" would otherwise
// change type or break the document).
func yamlValue(v any) string {
	switch t := v.(type) {
	case string:
		return jsonString(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return jsonString(t.Format(timeLayout))
	}
	return jsonString(fmt.Sprint(v))
}

// plainYAML reports whether s can be written as a plain scalar: an
// identifier-ish key that no YAML resolver reads as a number, bool or null.
func plainYAML(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	if c := s[0]; c >= '0' && c <= '9' || c == '-' || c == '.' {
		return false
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "nan":
		return false
	}
	return true
}

// --------------------------------------------------------------------- TOML

// tomlWriter renders an array of tables named after the spec's table.
type tomlWriter struct{}

func (tomlWriter) begin(*bufio.Writer, *Generator) error { return nil }

func (tomlWriter) row(bw *bufio.Writer, g *Generator, idx int, vals []any) error {
	if idx > 0 {
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}
	if _, err := bw.WriteString("[[" + tomlKey(g.Spec().Table) + "]]\n"); err != nil {
		return err
	}
	for i, name := range g.Names() {
		if _, err := bw.WriteString(tomlKey(name) + " = " + tomlValue(vals[i]) + "\n"); err != nil {
			return err
		}
	}
	return nil
}

func (tomlWriter) end(*bufio.Writer, *Generator) error { return nil }

// tomlKey renders a bare key when the name allows it, a quoted key otherwise.
func tomlKey(s string) string {
	if s == "" {
		return jsonString(s)
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-':
		default:
			return jsonString(s)
		}
	}
	return s
}

// tomlValue renders a value; dates use TOML's native offset date-time, which
// is what makes a generated TOML file worth reading in a TOML-aware viewer.
func tomlValue(v any) string {
	switch t := v.(type) {
	case string:
		return jsonString(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		// TOML floats must carry a fractional part; a whole number written
		// bare would decode as an integer and break a typed round-trip.
		s := strconv.FormatFloat(t, 'f', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return t.Format(time.RFC3339)
	}
	return jsonString(fmt.Sprint(v))
}

// ---------------------------------------------------------------------- SQL

// sqlWriter renders one INSERT statement per row against the spec's table.
type sqlWriter struct {
	cols string
}

func (s *sqlWriter) begin(bw *bufio.Writer, g *Generator) error {
	names := g.Names()
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = sqlIdent(n)
	}
	s.cols = strings.Join(quoted, ", ")
	_, err := bw.WriteString("-- " + strconv.Itoa(g.Spec().Rows) + " generated rows for " +
		sqlIdent(g.Spec().Table) + "\n")
	return err
}

func (s *sqlWriter) row(bw *bufio.Writer, g *Generator, _ int, vals []any) error {
	lits := make([]string, len(vals))
	for i, v := range vals {
		lits[i] = sqlValue(v)
	}
	_, err := bw.WriteString("INSERT INTO " + sqlIdent(g.Spec().Table) + " (" + s.cols +
		") VALUES (" + strings.Join(lits, ", ") + ");\n")
	return err
}

func (s *sqlWriter) end(*bufio.Writer, *Generator) error { return nil }

// sqlIdent quotes an identifier the standard way — double quotes, an embedded
// double quote doubled.
func sqlIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// sqlValue renders a value as a SQL literal; strings are single-quoted with
// the quote doubled, which is the escaping every dialect agrees on.
func sqlValue(v any) string {
	switch t := v.(type) {
	case string:
		return sqlString(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "1"
		}
		return "0"
	case time.Time:
		return sqlString(t.Format(sqlTimeLayout))
	}
	return sqlString(fmt.Sprint(v))
}

// sqlString quotes a string literal, flattening embedded newlines so one
// statement stays on one line (a generated INSERT file is read as a list).
func sqlString(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ---------------------------------------------------------------------- log

// logWriter renders logfmt lines: the ts/level/msg triple every log viewer
// keys on, followed by the spec's fields as extra pairs — so a generated log
// file exercises the log timeline *and* carries the requested columns.
type logWriter struct{}

func (logWriter) begin(*bufio.Writer, *Generator) error { return nil }

func (logWriter) row(bw *bufio.Writer, g *Generator, _ int, vals []any) error {
	ts, level, msg := g.logEntry()
	line := "ts=" + ts.Format(logTimeLayout) + " level=" + level + " msg=" + logfmtValue(msg)
	for i, name := range g.Names() {
		line += " " + logfmtKey(name) + "=" + logfmtValue(plainValue(vals[i]))
	}
	_, err := bw.WriteString(line + "\n")
	return err
}

func (logWriter) end(*bufio.Writer, *Generator) error { return nil }

// logfmtKey reduces a field name to a bare logfmt key; logfmt has no quoted
// keys, so anything outside the safe set becomes '_'.
func logfmtKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "field"
	}
	return b.String()
}

// logfmtValue quotes a value when it is empty or carries anything a bare
// logfmt token may not — whitespace, quotes or the separator itself.
func logfmtValue(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\r\n\"=") {
		return strconv.Quote(s)
	}
	return s
}
