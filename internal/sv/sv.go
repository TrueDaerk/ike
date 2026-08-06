// Package sv is the shared model of separator-delimited (csv/tsv/…) text
// (#1589): which language ids are *sv languages, which separator a buffer
// uses, and how a line splits into fields with quoting honored. It is a leaf
// package so both the csv language plugin (rainbow column spans) and the
// editor's table-rendering layer split identically.
package sv

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
