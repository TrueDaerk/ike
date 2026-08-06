// Package langcsv registers the separator-delimited file languages (#1589):
// csv, tsv and psv. They have no Tree-sitter grammar — their entire structure
// is Go-computed through the lang.Language.Spans seam (#1585): every column
// gets a cycling rainbow capture (the theme-derived rainbow.N slots of #789)
// and the separators style as punctuation. The editor's table-rendering layer
// (internal/editor, #1589) adds the display-only alignment, separator conceal
// and pinned header on top; the shared split rules live in internal/sv.
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langcsv

import (
	"fmt"

	"ike/internal/highlight"
	"ike/internal/lang"
	"ike/internal/sv"
	"ike/plugins/languages/register"
)

func init() {
	for id, exts := range map[string][]string{
		"csv": {"csv"},
		"tsv": {"tsv"},
		"psv": {"psv"},
	} {
		id := id
		register.Language(lang.Language{
			ID:         id,
			Extensions: exts,
			Spans:      func(lines []string) []lang.Span { return columnSpans(id, lines) },
		})
	}
}

// columnSpans emits one rainbow span per field and a punctuation span per
// separator, per line — rainbow-csv style: column N cycles through the
// theme-derived rainbow.0…rainbow.5 slots.
func columnSpans(id string, lines []string) []lang.Span {
	sep := sv.Separator(id, lines)
	var out []lang.Span
	for li, line := range lines {
		fields := sv.Fields(line, sep)
		for i, f := range fields {
			if f.Start < f.End {
				out = append(out, lang.Span{
					Line: li, StartCol: f.Start, EndCol: f.End,
					Capture: fmt.Sprintf("rainbow.%d", i%highlight.RainbowColors),
				})
			}
			if i+1 < len(fields) {
				// The separator sits between this field's end and the next
				// field's start.
				out = append(out, lang.Span{
					Line: li, StartCol: f.End, EndCol: f.End + 1, Capture: "punctuation",
				})
			}
		}
	}
	return out
}
