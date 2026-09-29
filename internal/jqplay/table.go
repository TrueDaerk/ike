package jqplay

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/itchyny/gojq"
)

// table.go is the playground's CSV / TSV export (#2788): a result that is a
// list of objects — or of scalars — becomes rows a spreadsheet takes on
// paste. The rows are the result's values, not its rendered text, so the
// export does not depend on the -r / -c form the buffer shows.
//
// Row source: a single array output is the list; a single object or scalar is
// a one-row table; a stream of several outputs (`.[]`) is the list of its
// values, the way `-s` would collect them.
//
// Columns: the union of the objects' keys in order of first appearance. A Go
// map has no order, so within one object the keys are taken in the sorted
// order the result buffer prints them in (gojq's), and a key first met in a
// later row is appended after every column known so far. A list of scalars is
// one column without a header row.
//
// Cells: a string as is, a number or boolean in jq's spelling, null and a
// missing key as an empty cell, an array or object as its compact JSON text.
// A cell holding the delimiter, a double quote, CR or LF is quoted per
// RFC 4180 — surrounded by double quotes, inner quotes doubled; the same rule
// applies to TSV, which spreadsheets read the same way. Records end in LF.

// Delimiters of the two table formats.
const (
	CSV = ','
	TSV = '\t'
)

// Values returns the result's output values: the ones the run produced for jq
// and yq, the decoded JSON stream for an xmq run whose command wrote JSON
// (`to-json`), nil for any other xmq output.
func (r Result) Values() []any {
	if r.values != nil || r.dialect != DialectXMQ || r.ext != "json" {
		return r.values
	}
	dec := json.NewDecoder(strings.NewReader(r.Text()))
	var out []any
	for {
		var v any
		if err := dec.Decode(&v); err != nil {
			if errors.Is(err, io.EOF) {
				return out
			}
			return nil
		}
		out = append(out, v)
	}
}

// TableReason reports why the result cannot be exported as a table, "" when
// it can — what the export picker shows next to its disabled CSV / TSV rows.
// It is the encode itself, so no result it passes fails the export after.
func (r Result) TableReason() string {
	_, err := r.Delimited(CSV)
	if err != nil {
		return err.Error()
	}
	return ""
}

// Delimited encodes the result as CSV (sep CSV) or TSV (sep TSV); the error
// is TableReason's when the result is not a table.
func (r Result) Delimited(sep rune) (string, error) {
	rows, err := r.tableRows()
	if err != nil {
		return "", err
	}
	return EncodeDelimited(rows, sep)
}

// tableRows picks the rows out of the result's values.
func (r Result) tableRows() ([]any, error) {
	if len(r.Outputs) == 0 {
		return nil, errors.New("the result is empty")
	}
	vals := r.Values()
	if vals == nil {
		return nil, errors.New("the " + r.dialect.Name() + " result is not JSON — run a to-json command")
	}
	if len(vals) == 1 {
		if arr, ok := vals[0].([]any); ok {
			if len(arr) == 0 {
				return nil, errors.New("the result is an empty array")
			}
			return arr, nil
		}
	}
	return vals, nil
}

// EncodeDelimited writes rows — all objects or all scalars — as delimited
// text; any other mix is an error naming the first row that breaks it.
func EncodeDelimited(rows []any, sep rune) (string, error) {
	if len(rows) == 0 {
		return "", errors.New("the result has no rows")
	}
	if _, objects := rows[0].(map[string]any); objects {
		return encodeObjects(rows, sep)
	}
	var b strings.Builder
	for i, row := range rows {
		if !isScalar(row) {
			return "", rowError(i, row)
		}
		writeRecord(&b, []string{cellText(row)}, sep)
	}
	return b.String(), nil
}

// encodeObjects writes the header row and one record per object.
func encodeObjects(rows []any, sep rune) (string, error) {
	var cols []string
	seen := map[string]bool{}
	for i, row := range rows {
		obj, ok := row.(map[string]any)
		if !ok {
			return "", rowError(i, row)
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			if !seen[k] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			seen[k] = true
			cols = append(cols, k)
		}
	}
	var b strings.Builder
	writeRecord(&b, cols, sep)
	cells := make([]string, len(cols))
	for _, row := range rows {
		obj := row.(map[string]any)
		for i, k := range cols {
			cells[i] = cellText(obj[k])
		}
		writeRecord(&b, cells, sep)
	}
	return b.String(), nil
}

// rowError names why row i does not fit the table the first row started.
func rowError(i int, row any) error {
	return fmt.Errorf("a table needs a list of objects or of scalars — row %d is %s", i+1, kindName(row))
}

// kindName is a value's jq type name with its article.
func kindName(v any) string {
	switch v.(type) {
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case nil:
		return "null"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	}
	return "a number"
}

// isScalar reports whether v is neither an array nor an object.
func isScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

// cellText renders one cell's unquoted text.
func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	b, err := gojq.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// writeRecord appends one record, quoting each field that needs it.
func writeRecord(b *strings.Builder, fields []string, sep rune) {
	for i, f := range fields {
		if i > 0 {
			b.WriteRune(sep)
		}
		if strings.ContainsRune(f, sep) || strings.ContainsAny(f, "\"\r\n") {
			b.WriteByte('"')
			b.WriteString(strings.ReplaceAll(f, `"`, `""`))
			b.WriteByte('"')
			continue
		}
		b.WriteString(f)
	}
	b.WriteByte('\n')
}
