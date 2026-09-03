package langhttp

import (
	"strings"
	"testing"

	"ike/internal/httpfile"
)

// graphql_test.go covers the editor side of a GRAPHQL block (#2423): where the
// two sections are, and how the query section is painted when no grammar
// claims it.

const graphQLBuffer = `### hero
GRAPHQL https://example.com/graphql

query Hero($episode: Episode) {
  hero(episode: $episode) {
    name
  }
}

{
  "episode": "EMPIRE"
}
`

func TestGraphQLLexerPaintsTheQuery(t *testing.T) {
	lines := strings.Split(graphQLBuffer, "\n")
	// The lexer, exercised directly: the region route depends on whether this
	// build registers a "graphql" language, which is not what is under test.
	spans := graphQLLineSpans(3, lines[3]) // "query Hero($episode: Episode) {"
	if got := captureAt(spans, 3, 0); got != "keyword" {
		t.Errorf("\"query\" capture = %q, want keyword", got)
	}
	if got := captureAt(spans, 3, 6); got != "type" {
		t.Errorf("operation name capture = %q, want type", got)
	}
	if got := captureAt(spans, 3, 11); got != "variable" {
		t.Errorf("\"$episode\" capture = %q, want variable", got)
	}
	if got := captureAt(spans, 3, 21); got != "type" {
		t.Errorf("\"Episode\" capture = %q, want type", got)
	}

	fieldLine := lines[4] // "  hero(episode: $episode) {"
	spans = graphQLLineSpans(4, fieldLine)
	if got := captureAt(spans, 4, 2); got != "function" {
		t.Errorf("field capture = %q, want function", got)
	}
	if got := captureAt(spans, 4, 7); got != "property" {
		t.Errorf("argument-name capture = %q, want property", got)
	}
}

func TestGraphQLLexerHandlesStringsAndComments(t *testing.T) {
	spans := graphQLLineSpans(0, `  hero(name: "luke") # the droid`)
	if got := captureAt(spans, 0, 14); got != "string" {
		t.Errorf("string capture = %q, want string", got)
	}
	if got := captureAt(spans, 0, 22); got != "comment" {
		t.Errorf("comment capture = %q, want comment", got)
	}
	// A "{" inside the comment is not structure and must not be punctuation.
	spans = graphQLLineSpans(0, `# { not a selection set }`)
	for _, s := range spans {
		if s.Capture != "comment" {
			t.Errorf("comment line yielded %q at %d", s.Capture, s.StartCol)
		}
	}
}

// The lexer covers the query section only: the variables object below is JSON
// and gets its own region.
func TestGraphQLSpansCoverOnlyTheQuerySection(t *testing.T) {
	if graphQLHighlighted() {
		t.Skip("this build registers a graphql grammar; the lexer stands down")
	}
	lines := strings.Split(graphQLBuffer, "\n")
	spans := graphQLSpans(httpfile.Parse(graphQLBuffer), lines)
	if len(spans) == 0 {
		t.Fatal("no spans for the query section")
	}
	for _, s := range spans {
		if s.Line < 3 || s.Line > 7 {
			t.Errorf("span on line %d is outside the query section (3..7): %+v", s.Line, s)
		}
	}
}
