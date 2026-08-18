package datasrc

// csvprofile.go profiles one column of separator-delimited text (#1940) — the
// csv/tsv/psv buffers the editor renders as a table (#1589). There is no query
// engine behind a text buffer, so this is the bounded-scan shape of the
// profile: the same accumulator the Parquet reader uses, fed field by field,
// stopping at ProfileLimit rows and saying so.
//
// Splitting goes through internal/sv, the shared model of separator-delimited
// text, so the profile's idea of "column 3" is exactly the one the table
// rendering and the status line already show. Values are unquoted the way a
// column name is: `"foo"` profiles as foo, and a padded field is padding, not
// a value.

import "ike/internal/sv"

// ProfileCSV profiles field idx of the separator-delimited rows in lines.
// table and column name the result; skipHeader drops the first line, which
// the caller decides with sv.Header. Blank lines are not rows and are
// skipped; a row too short to reach idx contributes a missing value (NULL),
// which is what distinguishes "the column is empty here" from "the row does
// not have this column at all".
//
// The scan stops after ProfileLimit rows and marks the result Capped — the
// caller states the cap, since numbers over the head of a million-line file
// must never read as numbers over the file.
func ProfileCSV(table, column string, lines []string, sep rune, idx int, skipHeader bool) Profile {
	acc := newScanProfile()
	capped := false
	rows := int64(0)
	for i, line := range lines {
		if i == 0 && skipHeader {
			continue
		}
		if line == "" {
			continue // a blank line (the trailing newline, mostly) is no row
		}
		if rows >= ProfileLimit {
			capped = true
			break
		}
		rows++
		acc.add(csvField(line, sep, idx))
	}
	return acc.result(table, column, "", capped)
}

// csvField is one row's value in column idx: the unquoted field text, or a
// NULL cell when the row has no such field.
func csvField(line string, sep rune, idx int) Cell {
	fields := sv.Fields(line, sep)
	if idx < 0 || idx >= len(fields) {
		return Cell{Null: true}
	}
	runes := []rune(line)
	f := fields[idx]
	return Cell{Text: sv.Unquote(string(runes[f.Start:f.End]))}
}
