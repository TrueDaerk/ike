package httpfile

import (
	"os"
	"strings"
	"testing"

	"ike/internal/graphql"
)

// graphql_test.go covers the GRAPHQL block (#2423): the parse-time split of
// query and variables, the operation-name detection, and the rewrite into the
// POST + JSON envelope that goes on the wire.

const heroBlock = `### hero
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

func TestParseGraphQLBlockSplitsQueryAndVariables(t *testing.T) {
	f := Parse(heroBlock)
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d, want 1 (errors %v)", len(f.Requests), f.Errors)
	}
	r := f.Requests[0]
	if r.Method != GraphQLMethod {
		t.Errorf("method = %q, want %q", r.Method, GraphQLMethod)
	}
	if r.GraphQL == nil {
		t.Fatal("GraphQL spec is nil")
	}
	wantQuery := "query Hero($episode: Episode) {\n  hero(episode: $episode) {\n    name\n  }\n}"
	if r.GraphQL.Query != wantQuery {
		t.Errorf("query =\n%q\nwant\n%q", r.GraphQL.Query, wantQuery)
	}
	if want := "{\n  \"episode\": \"EMPIRE\"\n}"; r.GraphQL.Variables != want {
		t.Errorf("variables = %q, want %q", r.GraphQL.Variables, want)
	}
	if r.GraphQL.OperationName != "Hero" {
		t.Errorf("operationName = %q, want %q", r.GraphQL.OperationName, "Hero")
	}
	// The line ranges address the sections in the file, which is what the
	// region/span producers and the completion source walk.
	if r.GraphQL.QueryStart != 4 || r.GraphQL.QueryEnd != 8 {
		t.Errorf("query lines = %d..%d, want 4..8", r.GraphQL.QueryStart, r.GraphQL.QueryEnd)
	}
	if r.GraphQL.VarsStart != 10 || r.GraphQL.VarsEnd != 12 {
		t.Errorf("variables lines = %d..%d, want 10..12", r.GraphQL.VarsStart, r.GraphQL.VarsEnd)
	}
}

func TestParseGraphQLBlockWithoutVariables(t *testing.T) {
	f := Parse("GRAPHQL https://example.com/graphql\n\n{ hero { name } }\n")
	r := f.Requests[0]
	if r.GraphQL == nil {
		t.Fatal("GraphQL spec is nil")
	}
	if r.GraphQL.Variables != "" {
		t.Errorf("variables = %q, want none", r.GraphQL.Variables)
	}
	if r.GraphQL.Query != "{ hero { name } }" {
		t.Errorf("query = %q", r.GraphQL.Query)
	}
	if r.GraphQL.OperationName != "" {
		t.Errorf("operationName = %q, want none for the anonymous shorthand", r.GraphQL.OperationName)
	}
	if r.GraphQL.VarsStart != 0 {
		t.Errorf("VarsStart = %d, want 0", r.GraphQL.VarsStart)
	}
}

// A query holding a blank line of its own must keep it: the split scans from
// the end, and the chunk behind such a blank does not open with "{".
func TestParseGraphQLQueryKeepsItsOwnBlankLines(t *testing.T) {
	src := "GRAPHQL https://example.com/graphql\n\n" +
		"query A {\n  a\n}\n\nquery B {\n  b\n}\n"
	r := Parse(src).Requests[0]
	if r.GraphQL.Variables != "" {
		t.Fatalf("variables = %q, want none", r.GraphQL.Variables)
	}
	if !strings.Contains(r.GraphQL.Query, "query B") {
		t.Errorf("second operation dropped from the query: %q", r.GraphQL.Query)
	}
	if r.GraphQL.OperationName != "A" {
		t.Errorf("operationName = %q, want the first named operation", r.GraphQL.OperationName)
	}
}

func TestGraphQLOperationNameDetection(t *testing.T) {
	tests := []struct{ query, want string }{
		{"query Hero { hero { name } }", "Hero"},
		{"mutation AddThing($n: String!) { add(n: $n) { id } }", "AddThing"},
		{"subscription Ticks { ticks }", "Ticks"},
		{"{ hero { name } }", ""},
		{"query { hero { name } }", ""},
		// A field named like a keyword inside the selection set is not an
		// operation: only the text before the first "{" is examined.
		{"{ mutation something }", ""},
		// A "#" comment naming an operation must not win over the real one.
		{"# query Fake\nquery Real { a }", "Real"},
	}
	for _, tc := range tests {
		if got := graphql.OperationName(tc.query); got != tc.want {
			t.Errorf("operation name of %q = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestResolveGraphQLBuildsEnvelope(t *testing.T) {
	r := Parse(heroBlock).Requests[0]
	out, err := r.Resolve(os.LookupEnv)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out.Method != "POST" {
		t.Errorf("method = %q, want POST", out.Method)
	}
	if ct, _ := out.Header("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	want := `{"query":"query Hero($episode: Episode) {\n  hero(episode: $episode) {\n    name\n  }\n}",` +
		`"variables":{"episode":"EMPIRE"},"operationName":"Hero"}`
	if out.Body != want {
		t.Errorf("body =\n%s\nwant\n%s", out.Body, want)
	}
}

func TestResolveGraphQLSubstitutesBeforeEnveloping(t *testing.T) {
	src := "@who = luke\n\n### hero\nGRAPHQL https://example.com/graphql\n\n" +
		"query { hero(name: \"{{who}}\") { name } }\n"
	f := Parse(src)
	out, err := f.Requests[0].ResolveVars(&Vars{File: f.VarMap(), Lookup: os.LookupEnv})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(out.Body, `hero(name: \"luke\")`) {
		t.Errorf("placeholder not substituted before enveloping: %s", out.Body)
	}
}

// application/graphql carries the query itself, so no envelope is built.
func TestResolveGraphQLRawMediaSendsTheQueryAlone(t *testing.T) {
	src := "GRAPHQL https://example.com/graphql\nContent-Type: application/graphql\n\n{ hero { name } }\n"
	out, err := Parse(src).Requests[0].Resolve(os.LookupEnv)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out.Body != "{ hero { name } }" {
		t.Errorf("body = %q, want the raw query", out.Body)
	}
	if out.Method != "POST" {
		t.Errorf("method = %q, want POST", out.Method)
	}
}

func TestResolveGraphQLRejectsBrokenVariables(t *testing.T) {
	src := "GRAPHQL https://example.com/graphql\n\n{ hero { name } }\n\n{ not json }\n"
	if _, err := Parse(src).Requests[0].Resolve(os.LookupEnv); err == nil {
		t.Fatal("resolve accepted variables that are not JSON")
	}
}

// An anonymous operation sends no operationName rather than an empty one.
func TestGraphQLEnvelopeOmitsEmptyMembers(t *testing.T) {
	body, err := graphql.Envelope("{ a }", "", "")
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if got := string(body); got != `{"query":"{ a }"}` {
		t.Errorf("envelope = %s", got)
	}
}
