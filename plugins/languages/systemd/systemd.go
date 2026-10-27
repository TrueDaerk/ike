// Package langsystemd registers the systemd unit-file language (#3063): the
// unit files (`*.service`, `*.timer`, …) and their drop-ins
// (`foo.service.d/override.conf`, which would otherwise resolve to ini).
// Like ini (#1595) and crontab (#1624) there is no Tree-sitter grammar; the
// structure is Go-computed through the lang.Language.Spans seam (#1585), so a
// directive can be checked against the directive table (directives.go) and an
// unknown one styled apart from a known one.
//
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langsystemd

import (
	"strings"

	"ike/internal/epochtime"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
	"ike/internal/secret"
	"ike/plugins/languages/register"
)

// The captures the highlighter emits. A known directive styles as an
// attribute, an unknown one as a plain variable so a typo reads differently.
const (
	capSection   = "type"
	capKnown     = "attribute"
	capUnknown   = "variable"
	capSpecifier = "constant.builtin"
	capContinue  = "escape"
)

func init() {
	dropIns := make([]lang.DropIn, len(UnitTypes))
	for i, t := range UnitTypes {
		dropIns[i] = lang.DropIn{DirSuffix: "." + t + ".d", Ext: "conf"}
	}
	register.Language(lang.Language{
		ID:          "systemd",
		Extensions:  UnitTypes,
		DropIns:     dropIns,
		LineComment: "#",
		Spans:       systemdSpans,
		// Values are styled as strings, yet their table entries complete
		// (#3064).
		CompletionCode: completionCode,
	})
}

// specifiers are the letters systemd expands after `%` (systemd.unit(5),
// "Specifiers"); `%%` is a literal percent sign.
const specifiers = "aAbBCdDEfgGhHiIjJlLmMnNopPqsStTuUvVwWyY%"

// booleans are the values systemd parses as a boolean.
var booleans = map[string]bool{
	"yes": true, "no": true, "true": true, "false": true,
	"on": true, "off": true, "1": true, "0": true,
}

// systemdSpans emits the highlight spans for a unit file, line by line.
// Comments are full lines starting with `#` or `;` — systemd has no inline
// comments. A value ending in `\` continues on the next line; comment lines
// inside a continuation are skipped, as systemd does. The secret masks, number
// hints and network-literal hints of the ini family ride along, as in ini.
func systemdSpans(lines []string) []lang.Span {
	// The secret masks (#2345) come first: overlapping spans resolve
	// first-covering-wins, so the mask must precede the value styling below.
	out := append(secret.PairSpans(lines, "="), systemdStructure(lines)...)
	// Number hints over sizes and spans (#1627, #1684) and network literals
	// (#1653) — systemd shares ini's `#`/`;` comment leaders.
	hints, stamps := numhint.SpansWith(lines, epochtime.Spans(lines, epochtime.Value), numhint.CommentLeaders("ini")...)
	out = append(out, stamps...)
	out = append(out, hints...)
	return append(out, nethint.Spans(lines)...)
}

// systemdStructure emits the structural spans — sections, directives,
// values, comments, continuations — which never overlap one another.
func systemdStructure(lines []string) []lang.Span {
	var out []lang.Span
	section, continued := "", false
	for li, line := range lines {
		runes := []rune(line)
		start, end := trimIndex(runes)
		switch {
		case start >= end:
			continued = false
		case runes[start] == '#' || runes[start] == ';':
			out = append(out, lang.Span{Line: li, StartCol: start, EndCol: end, Capture: "comment"})
		case continued:
			var spans []lang.Span
			spans, continued = valueSpans(li, runes, start, end)
			out = append(out, spans...)
		case runes[start] == '[' && runes[end-1] == ']':
			section = string(runes[start+1 : end-1])
			out = append(out,
				lang.Span{Line: li, StartCol: start, EndCol: start + 1, Capture: "punctuation"},
				lang.Span{Line: li, StartCol: start + 1, EndCol: end - 1, Capture: capSection},
				lang.Span{Line: li, StartCol: end - 1, EndCol: end, Capture: "punctuation"},
			)
		default:
			var spans []lang.Span
			spans, continued = directiveSpans(li, runes, start, end, section)
			out = append(out, spans...)
		}
	}
	return out
}

// directiveSpans styles one `Name=value` line and reports whether its value
// continues on the next line.
func directiveSpans(li int, runes []rune, start, end int, section string) ([]lang.Span, bool) {
	eq := -1
	for i := start; i < end; i++ {
		if runes[i] == '=' {
			eq = i
			break
		}
	}
	if eq < 0 {
		return []lang.Span{{Line: li, StartCol: start, EndCol: end, Capture: capUnknown}}, false
	}
	var out []lang.Span
	if ke := trimEnd(runes, start, eq); ke > start {
		capture := capUnknown
		if Known(section, string(runes[start:ke])) {
			capture = capKnown
		}
		out = append(out, lang.Span{Line: li, StartCol: start, EndCol: ke, Capture: capture})
	}
	out = append(out, lang.Span{Line: li, StartCol: eq, EndCol: eq + 1, Capture: "punctuation"})
	vs := eq + 1
	for vs < end && isSpace(runes[vs]) {
		vs++
	}
	spans, continued := valueSpans(li, runes, vs, end)
	return append(out, spans...), continued
}

// Known reports whether name is a directive of section in the table, or an
// `X-` extension (in an extension section or by its own name).
func Known(section, name string) bool {
	if IsExtension(section) || IsExtension(name) {
		return true
	}
	_, ok := Lookup(section, name)
	return ok
}

// valueSpans styles the value in [start, end) — a boolean whole, otherwise as
// a string broken around `%` specifiers — plus a trailing `\` continuation,
// which it reports. The spans never overlap.
func valueSpans(li int, runes []rune, start, end int) ([]lang.Span, bool) {
	var out []lang.Span
	continued := end > start && runes[end-1] == '\\'
	if continued {
		end--
		out = append(out, lang.Span{Line: li, StartCol: end, EndCol: end + 1, Capture: capContinue})
		end = trimEnd(runes, start, end)
	}
	if start >= end {
		return out, continued
	}
	if booleans[strings.ToLower(string(runes[start:end]))] {
		return append(out, lang.Span{Line: li, StartCol: start, EndCol: end, Capture: "boolean"}), continued
	}
	from := start
	flush := func(to int) {
		if to > from {
			out = append(out, lang.Span{Line: li, StartCol: from, EndCol: to, Capture: "string"})
		}
	}
	for i := start; i < end-1; i++ {
		if runes[i] == '%' && strings.ContainsRune(specifiers, runes[i+1]) {
			flush(i)
			out = append(out, lang.Span{Line: li, StartCol: i, EndCol: i + 2, Capture: capSpecifier})
			i++
			from = i + 1
		}
	}
	flush(end)
	return out, continued
}

// trimIndex returns the rune-column range [start, end) of line with leading
// and trailing whitespace stripped; start >= end means a blank line.
func trimIndex(runes []rune) (start, end int) {
	end = len(runes)
	for start < end && isSpace(runes[start]) {
		start++
	}
	return start, trimEnd(runes, start, end)
}

// trimEnd shrinks end back over trailing whitespace within [start, end).
func trimEnd(runes []rune, start, end int) int {
	for end > start && isSpace(runes[end-1]) {
		end--
	}
	return end
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }
