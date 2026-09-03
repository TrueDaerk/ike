package graphql

import (
	"strings"
	"testing"
)

// context_test.go pins the caret walk: standing at "|" in an unfinished query,
// what applies there and against which type?

func TestAnalyzeResolvesTheCaret(t *testing.T) {
	s := fixtureSchema()
	tests := []struct {
		name   string
		before string
		kind   CaretKind
		typ    string
		field  string
		prefix string
	}{
		{"root selection set", "{ ", CaretField, "Query", "", ""},
		{"root with a prefix", "{ he", CaretField, "Query", "", "he"},
		{"named operation root", "query Hero {\n  ", CaretField, "Query", "", ""},
		{"mutation root", "mutation {\n  re", CaretField, "Mutation", "", "re"},
		{"nested selection set", "{ hero { ", CaretField, "Character", "", ""},
		{"two levels down", "{ hero { homeworld { na", CaretField, "Planet", "", "na"},
		{"back out of a set", "{ hero { name }\n  sea", CaretField, "Query", "", "sea"},
		{"list field keeps its named type", "{ search(text: \"x\") { ", CaretField, "Character", "", ""},
		{"argument list", "{ hero(ep", CaretArgument, "Query", "hero", "ep"},
		{"argument list after a comma", "{ search(text: \"a\", fi", CaretArgument, "Query", "search", "fi"},
		{"variable definition type", "query Hero($episode: Ep", CaretType, "", "", "Ep"},
		{"type condition", "{ hero { ... on Char", CaretType, "", "", "Char"},
		// Nothing schema-aware belongs on the value side of an argument, in a
		// string, or in a comment.
		{"argument value", "{ hero(episode: EM", CaretNone, "", "", "EM"},
		{"inside a string", `{ search(text: "he`, CaretNone, "", "", "he"},
		{"inside a comment", "{ # he", CaretNone, "", "", "he"},
		// A field the schema does not know ends the chain rather than guessing.
		{"unknown field", "{ nope { ", CaretNone, "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Analyze(s, tc.before)
			if got.Kind != tc.kind || got.Type != tc.typ || got.Field != tc.field || got.Prefix != tc.prefix {
				t.Errorf("Analyze(%q) = %+v, want kind %v type %q field %q prefix %q",
					tc.before, got, tc.kind, tc.typ, tc.field, tc.prefix)
			}
		})
	}
}

func TestAnalyzeWithoutSchema(t *testing.T) {
	if got := Analyze(nil, "{ he"); got.Kind != CaretNone || got.Prefix != "he" {
		t.Errorf("Analyze(nil) = %+v, want CaretNone with the typed prefix", got)
	}
}

func TestSplitBody(t *testing.T) {
	tests := []struct {
		name             string
		lines            []string
		query, variables string
		varsLine         int
	}{
		{"no variables", []string{"{ a }"}, "{ a }", "", -1},
		{"variables", []string{"{ a }", "", `{"x":1}`}, "{ a }", `{"x":1}`, 2},
		{"blank line inside the query", []string{"query A {", "  a", "}", "", "query B { b }"},
			"query A {\n  a\n}\n\nquery B { b }", "", -1},
		// A trailing blank line is not a separator: the chunk behind it is
		// empty, so there is nothing that could be a variables object.
		{"trailing blank", []string{"{ a }", ""}, "{ a }\n", "", -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q, v, line := SplitBody(tc.lines)
			if q != tc.query || v != tc.variables || line != tc.varsLine {
				t.Errorf("SplitBody = (%q, %q, %d), want (%q, %q, %d)",
					q, v, line, tc.query, tc.variables, tc.varsLine)
			}
		})
	}
}

func TestStripCommentsKeepsOffsets(t *testing.T) {
	src := `{ a # comment {` + "\n" + `  b "text {" }`
	got := StripComments(src)
	if len(got) != len(src) {
		t.Fatalf("length changed: %d vs %d", len(got), len(src))
	}
	// Both the comment's and the string's braces are gone, the structural ones
	// are not — two "{" in, one "{" out, and the line break survives.
	if n := strings.Count(got, "{"); n != 1 {
		t.Errorf("braces left = %d, want only the structural one: %q", n, got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Errorf("newline lost: %q", got)
	}
	if !strings.HasPrefix(got, "{ a ") || !strings.HasSuffix(got, " }") {
		t.Errorf("structure changed: %q", got)
	}
}
