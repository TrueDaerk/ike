package langhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ike/internal/complete"
	"ike/internal/graphql"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// graphql_complete_test.go drives the schema-aware completion end to end: a
// cached schema on disk, a buffer with a GRAPHQL block, and a caret inside its
// query section.

// fixtureSchemaJSON is the cache file's own shape — the trimmed model, not an
// introspection payload — so the test exercises exactly what the completion
// source reads.
const fixtureSchemaJSON = `{
  "queryType": "Query",
  "mutationType": "Mutation",
  "types": [
    {"name": "Character", "kind": "OBJECT", "fields": [
      {"name": "name", "type": "String!", "typeName": "String"},
      {"name": "homeworld", "type": "Planet", "typeName": "Planet"},
      {"name": "friends", "type": "[Character]", "typeName": "Character"}
    ]},
    {"name": "Episode", "kind": "ENUM"},
    {"name": "Mutation", "kind": "OBJECT", "fields": [
      {"name": "rename", "type": "Character", "typeName": "Character"}
    ]},
    {"name": "Planet", "kind": "OBJECT", "fields": [
      {"name": "name", "type": "String", "typeName": "String"}
    ]},
    {"name": "Query", "kind": "OBJECT", "fields": [
      {"name": "hero", "description": "The hero of an episode.",
       "type": "Character", "typeName": "Character",
       "args": [{"name": "episode", "type": "Episode", "typeName": "Episode", "default": "NEWHOPE"}]},
      {"name": "search", "type": "[Character]", "typeName": "Character",
       "args": [{"name": "text", "type": "String!", "typeName": "String"},
                {"name": "first", "type": "Int", "typeName": "Int"}]},
      {"name": "legacyHero", "type": "Character", "typeName": "Character", "deprecated": true}
    ]}
  ]
}`

// withCachedSchema points the schema cache at a temporary state directory and
// seeds it with the fixture, so the source has something to answer from.
func withCachedSchema(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	var s graphql.Schema
	if err := json.Unmarshal([]byte(fixtureSchemaJSON), &s); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := graphql.NewCache().Store(endpoint, &s); err != nil {
		t.Fatalf("store: %v", err)
	}
}

// graphQLCompleteAt completes at the "|" marker in a GRAPHQL request file.
func graphQLCompleteAt(t *testing.T, query string) []ilsp.CompletionItem {
	t.Helper()
	src := "### hero\nGRAPHQL https://example.com/graphql\n\n" + query + "\n"
	return completeAt(t, src)
}

func TestGraphQLCompletionOffersRootFields(t *testing.T) {
	withCachedSchema(t, "example.com")
	items := graphQLCompleteAt(t, "{\n  |\n}")
	for _, want := range []string{"hero", "search", "legacyHero", "__typename"} {
		if !has(items, want) {
			t.Errorf("root fields %v are missing %q", labels(items), want)
		}
	}
	if detail := detailFor(items, "hero"); detail != "Character" {
		t.Errorf("hero detail = %q, want its type", detail)
	}
	if detail := detailFor(items, "legacyHero"); !strings.Contains(detail, "deprecated") {
		t.Errorf("legacyHero detail = %q, want the deprecation marker", detail)
	}
}

func TestGraphQLCompletionFollowsTheSelectionPath(t *testing.T) {
	withCachedSchema(t, "example.com")
	items := graphQLCompleteAt(t, "{\n  hero {\n    homeworld {\n      |\n    }\n  }\n}")
	if !has(items, "name") {
		t.Errorf("Planet fields = %v, want name", labels(items))
	}
	// Query's own fields must not leak two levels down.
	if has(items, "hero") {
		t.Errorf("Query.hero offered inside Planet: %v", labels(items))
	}
}

func TestGraphQLCompletionOffersArguments(t *testing.T) {
	withCachedSchema(t, "example.com")
	items := graphQLCompleteAt(t, "{\n  hero(|)\n}")
	if !has(items, "episode") {
		t.Errorf("arguments = %v, want episode", labels(items))
	}
	// Accepting an argument leaves the caret where the value goes.
	if got := insertFor(items, "episode"); got != "episode: " {
		t.Errorf("insert = %q, want %q", got, "episode: ")
	}
	if detail := detailFor(items, "episode"); detail != "Episode = NEWHOPE" {
		t.Errorf("detail = %q, want the type and default", detail)
	}
}

func TestGraphQLCompletionOffersTypesInVariableDefinitions(t *testing.T) {
	withCachedSchema(t, "example.com")
	items := graphQLCompleteAt(t, "query Hero($episode: |) {\n  hero\n}")
	if !has(items, "Episode") || !has(items, "Character") {
		t.Errorf("types = %v, want the schema's named types", labels(items))
	}
	// Fields are not types: nothing lowercase belongs here.
	if has(items, "hero") {
		t.Errorf("a field leaked into the type position: %v", labels(items))
	}
}

func TestGraphQLCompletionUsesTheOperationRoot(t *testing.T) {
	withCachedSchema(t, "example.com")
	items := graphQLCompleteAt(t, "mutation {\n  |\n}")
	if !has(items, "rename") {
		t.Errorf("mutation fields = %v, want rename", labels(items))
	}
	if has(items, "hero") {
		t.Errorf("Query fields offered inside a mutation: %v", labels(items))
	}
}

// The variables object below the query is JSON, not a selection set.
func TestGraphQLCompletionStaysOutOfTheVariablesSection(t *testing.T) {
	withCachedSchema(t, "example.com")
	src := "GRAPHQL https://example.com/graphql\n\n{ hero { name } }\n\n{\n  \"he|\"\n}\n"
	if items := completeAt(t, src); len(items) != 0 {
		t.Errorf("variables section offered %v, want nothing", labels(items))
	}
}

// Without a cached schema the query section simply offers nothing — an empty
// batch closes the popup rather than filling it with buffer words.
func TestGraphQLCompletionWithoutACachedSchema(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	if items := graphQLCompleteAt(t, "{\n  |\n}"); len(items) != 0 {
		t.Errorf("items = %v, want none without a schema", labels(items))
	}
}

// A "{{name}}" inside a query still completes the request file's variables:
// the placeholder context keeps precedence over the schema one.
func TestGraphQLQueryStillCompletesPlaceholders(t *testing.T) {
	withCachedSchema(t, "example.com")
	src := "@who = luke\n\n### hero\nGRAPHQL https://example.com/graphql\n\n" +
		"{ hero(name: \"{{w|\") { name } }\n"
	items := completeAt(t, src)
	if !has(items, "who") {
		t.Errorf("items = %v, want the file variable", labels(items))
	}
}

// The completion source only answers .http buffers, so the schema lookup must
// not fire for a request that is not GraphQL at all.
func TestGraphQLCompletionIgnoresOrdinaryBodies(t *testing.T) {
	withCachedSchema(t, "example.com")
	src := "POST https://example.com/things\nContent-Type: application/json\n\n{\n  \"a|\"\n}\n"
	if items := completeAt(t, src); len(items) != 0 {
		t.Errorf("a JSON body offered %v, want nothing", labels(items))
	}
}

// completeAtPath is completeAt with a caller-chosen buffer path, for the one
// case where the path decides the answer.
func completeAtPath(t *testing.T, path, src string) []ilsp.CompletionItem {
	t.Helper()
	idx := strings.Index(src, "|")
	clean := strings.Replace(src, "|", "", 1)
	line := strings.Count(clean[:idx], "\n")
	col := len([]rune(clean[strings.LastIndex(clean[:idx], "\n")+1 : idx]))
	s := newHTTPSource()
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: path, Text: clean})
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: line, Col: col})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestGraphQLCompletionInARestFile(t *testing.T) {
	withCachedSchema(t, "example.com")
	src := "GRAPHQL https://example.com/graphql\n\n{\n  |\n}\n"
	if items := completeAtPath(t, "/p/req.rest", src); !has(items, "hero") {
		t.Errorf("a .rest file offered %v, want the schema's fields", labels(items))
	}
}
