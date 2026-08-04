package lsp

import "strings"

// partial.go implements the union-partial-match classifier (#1510): a type
// mismatch where the expected type is one of the members of the inferred
// union — pyright's
//
//	Argument of type "str | None" cannot be assigned to parameter "method" of type "str"
//
// is usually the realistic branch being fine and the None branch being the
// complaint, which a project may want at a lower severity than a completely
// wrong type. The classifier is exposed as the `partial` pseudo-condition in
// the ignore/severity rule grammar, so it stays opt-in and composes with the
// other conditions — in particular `source=`, because the message phrasing is
// server-specific.
//
// Classification is on the message text alone: the first quoted segment is
// the inferred (actual) type, the last quoted segment the expected type, and
// the diagnostic classifies as partial when every top-level union member of
// the expected type also appears in the actual type's union — with the actual
// union strictly larger. The message must read as an assignability complaint
// (contain "assign") so unrelated messages with quoted words never match.

// partialTypeMatch reports whether msg reads as a union-partial type
// mismatch.
func partialTypeMatch(msg string) bool {
	if !strings.Contains(strings.ToLower(msg), "assign") {
		return false
	}
	quoted := quotedSegments(msg)
	if len(quoted) < 2 {
		return false
	}
	actual := splitUnion(quoted[0])
	expected := splitUnion(quoted[len(quoted)-1])
	if len(actual) <= len(expected) {
		return false
	}
	for _, e := range expected {
		if !containsType(actual, e) {
			return false
		}
	}
	return true
}

// quotedSegments returns the contents of every double-quoted segment in s,
// in order. An unterminated quote yields no segment for the tail.
func quotedSegments(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '"')
		if i < 0 {
			return out
		}
		s = s[i+1:]
		j := strings.IndexByte(s, '"')
		if j < 0 {
			return out
		}
		out = append(out, s[:j])
		s = s[j+1:]
	}
}

// splitUnion splits a rendered type into its top-level union members: `|`
// separates members only at bracket depth zero, so `list[int | str] | None`
// yields ["list[int | str]", "None"]. Members are space-trimmed; empty
// members are dropped.
func splitUnion(t string) []string {
	var out []string
	depth, start := 0, 0
	flush := func(end int) {
		if m := strings.TrimSpace(t[start:end]); m != "" {
			out = append(out, m)
		}
	}
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
		case '|':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(t))
	return out
}

// containsType reports whether members contains t (exact, case-sensitive —
// type names are).
func containsType(members []string, t string) bool {
	for _, m := range members {
		if m == t {
			return true
		}
	}
	return false
}
