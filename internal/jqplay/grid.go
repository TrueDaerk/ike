package jqplay

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// grid.go is the shape check and cell model behind the playground's table view
// (#2794): a result that is a list of objects — or of scalars — shown as a grid
// instead of JSON text. It shares the CSV / TSV export's row source and column
// union (table.go), so a result the table view shows is one the export writes
// with the same columns in the same order; what differs is the cell text,
// which is for reading rather than for a spreadsheet: null reads as `null`
// and a key the row lacks is marked missing (the grid draws it as ∅), where
// the export writes both as an empty field.
//
// The table view is stricter than the export about the *outer* shape: a lone
// object or scalar is a document, not a list, so it stays in the text view
// instead of becoming a one-row table.

// Grid is a result laid out as rows and columns.
type Grid struct {
	// Columns are the objects' keys, the union in order of first appearance;
	// nil for a list of scalars, which is one unnamed column.
	Columns []string
	// Rows hold one cell per column, in result order.
	Rows [][]GridCell
	// Stream reports that the rows are the values of a multi-output stream
	// (`.[]`) rather than the items of one array — the rows then have no
	// index a path could name.
	Stream bool
}

// GridCell is one value of the grid.
type GridCell struct {
	// Text is the value's display text: a string as is (control characters
	// escaped, so a cell stays one line), null, a number or a boolean in jq's
	// spelling, an array or object as its compact JSON.
	Text string
	// Missing marks a key the row's object does not carry.
	Missing bool
	// Value is the decoded value itself, what sorting compares.
	Value any
}

// Grid lays the result out as a table, or returns why its shape does not fit:
// it must be a non-empty array — or a stream of several values — whose items
// are all objects or all scalars.
func (r Result) Grid() (Grid, error) {
	rows, err := r.tableRows()
	if err != nil {
		return Grid{}, err
	}
	vals := r.Values()
	stream := len(vals) > 1
	if !stream {
		if _, ok := vals[0].([]any); !ok {
			return Grid{}, errors.New("the result is " + kindName(vals[0]) + ", not a list — a table needs an array of objects or of scalars")
		}
	}
	g := Grid{Stream: stream, Rows: make([][]GridCell, len(rows))}
	if _, objects := rows[0].(map[string]any); !objects {
		for i, row := range rows {
			if !isScalar(row) {
				return Grid{}, rowError(i, row)
			}
			g.Rows[i] = []GridCell{{Text: gridText(row), Value: row}}
		}
		return g, nil
	}
	if g.Columns, err = objectColumns(rows); err != nil {
		return Grid{}, err
	}
	for i, row := range rows {
		obj := row.(map[string]any)
		cells := make([]GridCell, len(g.Columns))
		for c, k := range g.Columns {
			v, ok := obj[k]
			if !ok {
				cells[c] = GridCell{Missing: true}
				continue
			}
			cells[c] = GridCell{Text: gridText(v), Value: v}
		}
		g.Rows[i] = cells
	}
	return g, nil
}

// gridText renders one cell's display text.
func gridText(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return gridEscape(t)
	}
	return cellText(v)
}

// gridEscape keeps a string cell on one line and free of terminal control
// sequences: a control character is written the way JSON would escape it.
func gridEscape(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case isControl(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isControl reports a C0 control character or DEL.
func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// CompareCells orders two cells for a column sort: a missing key first, then
// jq's type order — null, false, true, numbers, strings, arrays, objects —
// numbers by value, strings by bytes, arrays and objects by their JSON text.
func CompareCells(a, b GridCell) int {
	ra, rb := cellRank(a), cellRank(b)
	if ra != rb {
		return ra - rb
	}
	switch ra {
	case rankNumber:
		return numberOf(a.Value).Cmp(numberOf(b.Value))
	case rankString:
		return strings.Compare(a.Value.(string), b.Value.(string))
	case rankArray, rankObject:
		return strings.Compare(a.Text, b.Text)
	}
	return 0
}

// The ranks CompareCells orders cell types by.
const (
	rankMissing = iota
	rankNull
	rankFalse
	rankTrue
	rankNumber
	rankString
	rankArray
	rankObject
)

// cellRank is a cell's place in the type order.
func cellRank(c GridCell) int {
	if c.Missing {
		return rankMissing
	}
	switch t := c.Value.(type) {
	case nil:
		return rankNull
	case bool:
		if t {
			return rankTrue
		}
		return rankFalse
	case string:
		return rankString
	case []any:
		return rankArray
	case map[string]any:
		return rankObject
	}
	return rankNumber
}

// numberOf widens any of the number representations a value can carry to a big.Float.
func numberOf(v any) *big.Float {
	switch t := v.(type) {
	case int:
		return new(big.Float).SetInt64(int64(t))
	case float64:
		if math.IsNaN(t) {
			return new(big.Float).SetInf(true) // jq sorts nan below every number
		}
		return big.NewFloat(t)
	case *big.Int:
		return new(big.Float).SetInt(t)
	case json.Number:
		// The input decoder keeps numbers verbatim (UseNumber), and a value
		// the program passed through untouched is still one.
		if f, ok := new(big.Float).SetString(string(t)); ok {
			return f
		}
	}
	return new(big.Float)
}
