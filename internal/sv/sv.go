// Package sv is the shared model of separator-delimited (csv/tsv/…) text
// (#1589): which language ids are *sv languages, which separator a buffer
// uses, and how a line splits into fields with quoting honored. It is a leaf
// package so both the csv language plugin (rainbow column spans) and the
// editor's table-rendering layer split identically.
package sv

import (
	"strconv"
	"strings"
)

// Langs maps the registered *sv language ids to their default separator; a
// zero value means the separator is sniffed from the content (csv files in
// locales with decimal commas conventionally use ';').
var Langs = map[string]rune{
	"csv": 0,
	"tsv": '\t',
	"psv": '|',
}

// IsLang reports whether id is a registered *sv language.
func IsLang(id string) bool {
	_, ok := Langs[id]
	return ok
}

// sniffLimit bounds how many lines Separator inspects.
const sniffLimit = 100

// Separator resolves the separator for a buffer of language id: the
// language's fixed separator, or — for csv — a sniff over the first lines
// counting ',' vs ';' outside quotes (ties and empty files keep ',').
func Separator(id string, lines []string) rune {
	sep := Langs[id]
	if sep != 0 {
		return sep
	}
	comma, semi := 0, 0
	for i, line := range lines {
		if i >= sniffLimit {
			break
		}
		inQuote := false
		for _, r := range line {
			switch {
			case r == '"':
				inQuote = !inQuote
			case inQuote:
			case r == ',':
				comma++
			case r == ';':
				semi++
			}
		}
	}
	if semi > comma {
		return ';'
	}
	return ','
}

// Field is one field of a split line: the half-open rune-column range
// [Start, End) of its content, exclusive of the separators around it.
type Field struct {
	Start, End int
}

// Fields splits a line at sep into field column ranges, honoring quoting: a
// '"' toggles a quoted region in which separators are literal, and a doubled
// '""' inside quotes is an escaped quote. The quotes themselves stay part of
// the field (display is raw bytes; only separators are structure). A line
// always yields at least one field; a trailing separator yields a trailing
// empty field, like encoding/csv would.
func Fields(line string, sep rune) []Field {
	runes := []rune(line)
	fields := []Field{{Start: 0}}
	inQuote := false
	for i, r := range runes {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == sep && !inQuote:
			fields[len(fields)-1].End = i
			fields = append(fields, Field{Start: i + 1})
		}
	}
	fields[len(fields)-1].End = len(runes)
	return fields
}

// IndexAt returns the index of the field containing the rune column col on a
// line split at sep. A column on a separator belongs to the field the
// separator closes; a column past the line end belongs to the last field.
func IndexAt(line string, sep rune, col int) int {
	fields := Fields(line, sep)
	for i, f := range fields {
		if col <= f.End {
			return i
		}
	}
	return len(fields) - 1
}

// Unquote is a field's display name: surrounding whitespace and quotes
// stripped, doubled `""` unescaped. Fields() keeps the quotes because display
// is raw bytes; a name shown outside the buffer wants them gone. Whitespace is
// trimmed on both sides of the quotes — a padded name is padding, not a name.
func Unquote(field string) string {
	field = strings.TrimSpace(field)
	if len(field) >= 2 && field[0] == '"' && field[len(field)-1] == '"' {
		field = strings.ReplaceAll(field[1:len(field)-1], `""`, `"`)
	}
	return strings.TrimSpace(field)
}

// Header returns the column names of line when it reads like a header row,
// and false when it does not: a header names its columns, so every field must
// be non-empty and non-numeric (a first data row of an unheadered file
// typically carries a number, an empty cell, or both). Callers fall back to
// the bare column index on false.
func Header(line string, sep rune) ([]string, bool) {
	if strings.TrimSpace(line) == "" {
		return nil, false
	}
	runes := []rune(line)
	fields := Fields(line, sep)
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		name := Unquote(string(runes[f.Start:f.End]))
		if name == "" {
			return nil, false
		}
		if _, err := strconv.ParseFloat(name, 64); err == nil {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}
