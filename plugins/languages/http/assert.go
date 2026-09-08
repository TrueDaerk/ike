package langhttp

// assert.go is the editor side of the `# @assert` directive (#2546):
// highlighting — the line is a comment to the grammar, and its parts must
// read as the structure they are, exactly like a capture directive — and
// completion of the directive's keywords, so `status`, `jsonpath` and
// `contains` need not be remembered.

import (
	"regexp"
	"strings"

	"ike/internal/httpfile"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// assertSpans produces the directive spans of the whole buffer: the marker as
// a keyword, the subject as a type, the header name or JSONPath as a
// property, the operator as an operator, and the expected value as a number
// or a string. A directive that does not parse gets its marker painted and
// nothing else — the parts are not there to point at.
func assertSpans(lines []string) []lang.Span {
	var out []lang.Span
	for i, line := range lines {
		a, ok := httpfile.AssertDirective(line)
		if !ok {
			continue
		}
		runes := []rune(line)
		marker := indexRunes(runes, 0, httpfile.AssertKeyword)
		if marker < 0 {
			continue
		}
		at := marker + len(httpfile.AssertKeyword)
		out = append(out, span(i, marker, at, "keyword"))
		if a.Subject == "" {
			continue
		}
		at = appendPart(&out, runes, i, at, a.Subject, "type")
		if a.Arg != "" {
			at = appendPart(&out, runes, i, at, a.Arg, "property")
		}
		if a.Op == "" {
			continue
		}
		at = appendPart(&out, runes, i, at, a.Op, "operator")
		if a.Expected == "" {
			continue
		}
		capture := "string"
		if numberRE.MatchString(a.Expected) {
			capture = "number"
		}
		// The expected value may have been unquoted; paint from its first
		// character to the end of the trimmed line so the quotes read with it.
		start := indexRunes(runes, at, a.Expected)
		if start < 0 {
			continue
		}
		if start > at && (runes[start-1] == '"' || runes[start-1] == '\'' || runes[start-1] == '/') {
			start--
		}
		end := len([]rune(strings.TrimRight(line, " \t")))
		out = append(out, span(i, start, end, capture))
	}
	return out
}

// appendPart paints want where it next occurs at or after from and returns
// the column past it; from is returned unchanged when it is not found.
func appendPart(out *[]lang.Span, runes []rune, line, from int, want, capture string) int {
	at := indexRunes(runes, from, want)
	if at < 0 {
		return from
	}
	end := at + len([]rune(want))
	*out = append(*out, span(line, at, end, capture))
	return end
}

// numberRE is the number test the span producer applies to an expected
// value: an integer or decimal, with an optional sign.
var numberRE = regexp.MustCompile(`^[+-]?\d+(\.\d+)?$`)

// directiveMarkerRE matches a comment line on which a directive marker is
// being typed: `# @`, `# @as`, `// @cap`.
var directiveMarkerRE = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@([A-Za-z]*)$`)

// assertSpecRE matches a comment line holding an `@assert` directive up to
// the caret, capturing the spec typed so far (possibly empty).
var assertSpecRE = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@assert(?:[ \t]+(.*))?$`)

// directiveKeywords are the markers offered on a `# @` line.
var directiveKeywords = []string{httpfile.AssertKeyword, "@capture"}

// directiveItems completes the parts of a directive line (#2546), given the
// text before the caret: the marker after `# @`, then — inside an `@assert`
// — the subject, the header name for `header`, and the operator. Everything
// else on a comment line completes nothing, as before.
func directiveItems(before string) []ilsp.CompletionItem {
	if m := directiveMarkerRE.FindStringSubmatch(before); m != nil {
		var items []ilsp.CompletionItem
		for _, k := range directiveKeywords {
			if !matches("@"+m[1], k) {
				continue
			}
			items = append(items, ilsp.CompletionItem{
				Label: k, FilterText: k, InsertText: k + " ", Detail: "directive", SortText: k,
			})
		}
		return items
	}
	m := assertSpecRE.FindStringSubmatch(before)
	if m == nil {
		return nil
	}
	spec := m[1]
	fields := strings.Fields(spec)
	typing := endsInField(spec) && len(fields) > 0
	typed := ""
	if typing {
		typed = fields[len(fields)-1]
	}
	// done counts the fields settled before the one being typed.
	done := len(fields)
	if typing {
		done--
	}
	if done == 0 {
		return fuzzyItems(httpfile.AssertSubjects, typed, "assert subject")
	}
	subject := fields[0]
	switch subject {
	case httpfile.AssertHeader:
		switch done {
		case 1:
			return fuzzyItems(headerNames, typed, "header")
		case 2:
			return fuzzyItems(httpfile.AssertOps, typed, "assert operator")
		}
	case httpfile.AssertJSONPath:
		if done == 2 {
			return fuzzyItems(httpfile.AssertOps, typed, "assert operator")
		}
	case httpfile.AssertStatus, httpfile.AssertBody, httpfile.AssertTime:
		if done == 1 {
			return fuzzyItems(httpfile.AssertOps, typed, "assert operator")
		}
	}
	return nil
}
