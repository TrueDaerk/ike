// Package langini registers the ini-style config language (#1595): `[section]`
// headers, `key = value` (or `key: value`) pairs and `#`/`;` line comments,
// covering .ini and .conf. There is no Tree-sitter grammar — like csv (#1589)
// the whole structure is Go-computed through the lang.Language.Spans seam
// (#1585): sections style as type, keys as property, separators as
// punctuation, values as string and comment lines as comment.
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langini

import (
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
	"ike/plugins/languages/register"
)

func init() {
	register.Language(lang.Language{
		ID:          "ini",
		Extensions:  []string{"ini", "conf"},
		Spans:       iniSpans,
		LineComment: "#",
	})
}

// iniSpans emits the highlight spans for an ini buffer, line by line. Only
// full-line comments are recognised — a `#` or `;` inside a value stays part
// of the value, since ini dialects disagree on inline comments and a URL or
// password must not be half-greyed. Columns are rune columns, matching the
// lang.Span contract.
func iniSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		start, end := trimIndex(runes)
		if start >= end {
			continue
		}
		switch {
		case runes[start] == '#' || runes[start] == ';':
			out = append(out, lang.Span{Line: li, StartCol: start, EndCol: end, Capture: "comment"})
		case runes[start] == '[' && runes[end-1] == ']':
			out = append(out,
				lang.Span{Line: li, StartCol: start, EndCol: start + 1, Capture: "punctuation"},
				lang.Span{Line: li, StartCol: start + 1, EndCol: end - 1, Capture: "type"},
				lang.Span{Line: li, StartCol: end - 1, EndCol: end, Capture: "punctuation"},
			)
		default:
			out = append(out, pairSpans(li, runes, start, end)...)
		}
	}
	// Number-readability hints (#1627): byte sizes, durations, digit grouping
	// and radix readings over the `key = value` pairs. Network literals
	// (#1653): CIDR prefixes and punycode hosts — an ini/conf file is where
	// most allow-lists live.
	out = append(out, numhint.Spans(lines)...)
	return append(out, nethint.Spans(lines)...)
}

// pairSpans styles one `key = value` line: property key, punctuation
// separator (`=` or `:`, whichever comes first), string value. A line with no
// separator (a bare flag like `hidepid`) styles whole as property.
func pairSpans(li int, runes []rune, start, end int) []lang.Span {
	sep := -1
	for i := start; i < end; i++ {
		if runes[i] == '=' || runes[i] == ':' {
			sep = i
			break
		}
	}
	if sep < 0 {
		return []lang.Span{{Line: li, StartCol: start, EndCol: end, Capture: "property"}}
	}
	out := make([]lang.Span, 0, 3)
	if ke := trimEnd(runes, start, sep); ke > start {
		out = append(out, lang.Span{Line: li, StartCol: start, EndCol: ke, Capture: "property"})
	}
	out = append(out, lang.Span{Line: li, StartCol: sep, EndCol: sep + 1, Capture: "punctuation"})
	vs := sep + 1
	for vs < end && runes[vs] == ' ' {
		vs++
	}
	if vs < end {
		out = append(out, lang.Span{Line: li, StartCol: vs, EndCol: end, Capture: "string"})
	}
	return out
}

// trimIndex returns the rune-column range [start, end) of line with leading
// and trailing whitespace stripped; start >= end means a blank line.
func trimIndex(runes []rune) (start, end int) {
	end = len(runes)
	for start < end && isSpace(runes[start]) {
		start++
	}
	for end > start && isSpace(runes[end-1]) {
		end--
	}
	return start, end
}

// trimEnd shrinks end back over trailing whitespace within [start, end).
func trimEnd(runes []rune, start, end int) int {
	for end > start && isSpace(runes[end-1]) {
		end--
	}
	return end
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }
