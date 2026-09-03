package graphql

import (
	"strings"
	"testing"
)

func TestParseIntrospectionFlattensTypeRefs(t *testing.T) {
	s := fixtureSchema()
	if s.QueryType != "Query" || s.MutationType != "Mutation" {
		t.Fatalf("roots = %q/%q", s.QueryType, s.MutationType)
	}
	if s.SubscriptionType != "" {
		t.Errorf("subscription root = %q, want none", s.SubscriptionType)
	}
	f, ok := s.FieldByName("Query", "search")
	if !ok {
		t.Fatal("Query.search missing")
	}
	// The two forms a consumer asks for: the rendered signature and the named
	// type a selection set continues from.
	if f.Type != "[Character]" {
		t.Errorf("search type = %q, want [Character]", f.Type)
	}
	if f.TypeName != "Character" {
		t.Errorf("search named type = %q, want Character", f.TypeName)
	}
	if len(f.Args) != 2 || f.Args[0].Type != "String!" || f.Args[1].Default != "10" {
		t.Errorf("search args = %+v", f.Args)
	}
	if legacy, _ := s.FieldByName("Query", "legacyHero"); legacy == nil || !legacy.Deprecated {
		t.Error("legacyHero should be kept and marked deprecated")
	}
}

func TestParseIntrospectionSortsTypes(t *testing.T) {
	s := fixtureSchema()
	for i := 1; i < len(s.Types); i++ {
		if s.Types[i-1].Name > s.Types[i].Name {
			t.Fatalf("types are not sorted: %q before %q", s.Types[i-1].Name, s.Types[i].Name)
		}
	}
}

func TestParseIntrospectionReportsServerErrors(t *testing.T) {
	_, err := ParseIntrospection([]byte(`{"errors":[{"message":"introspection is disabled"}]}`))
	if err == nil || !strings.Contains(err.Error(), "introspection is disabled") {
		t.Fatalf("err = %v, want the server's message", err)
	}
}

func TestParseIntrospectionRejectsNonSchema(t *testing.T) {
	if _, err := ParseIntrospection([]byte(`{"data":{}}`)); err == nil {
		t.Fatal("a response without __schema was accepted")
	}
	if _, err := ParseIntrospection([]byte(`not json`)); err == nil {
		t.Fatal("a non-JSON response was accepted")
	}
}

func TestRootType(t *testing.T) {
	s := fixtureSchema()
	if got := s.RootType("mutation"); got != "Mutation" {
		t.Errorf("mutation root = %q", got)
	}
	// Anything else — including the anonymous shorthand's empty keyword — is a
	// query.
	for _, op := range []string{"query", "", "subscription"} {
		want := "Query"
		if op == "subscription" {
			want = "" // this schema has none
		}
		if got := s.RootType(op); got != want {
			t.Errorf("root of %q = %q, want %q", op, got, want)
		}
	}
}

func TestSDLRendersTheSchema(t *testing.T) {
	sdl := fixtureSchema().SDL()
	for _, want := range []string{
		"schema {\n  query: Query\n  mutation: Mutation\n}",
		"type Character implements Named {",
		"hero(episode: Episode = NEWHOPE): Character",
		"search(text: String!, first: Int = 10): [Character]",
		"legacyHero: Character @deprecated",
		"interface Named {",
		"enum Episode {",
		"  NEWHOPE",
		`"""`,
		"The hero of an episode.",
	} {
		if !strings.Contains(sdl, want) {
			t.Errorf("SDL is missing %q:\n%s", want, sdl)
		}
	}
	// Introspection meta types and built-in scalars are noise in a document
	// meant to be read.
	for _, unwanted := range []string{"__Schema", "scalar String"} {
		if strings.Contains(sdl, unwanted) {
			t.Errorf("SDL should not carry %q:\n%s", unwanted, sdl)
		}
	}
}
