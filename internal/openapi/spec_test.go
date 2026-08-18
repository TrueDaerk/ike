package openapi

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// load parses a fixture spec, failing the test on any read/parse error.
func load(t *testing.T, name string) *Spec {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return spec
}

// find returns the operation of a method/path pair.
func find(t *testing.T, spec *Spec, method, path string) *Operation {
	t.Helper()
	for _, op := range spec.Operations {
		if op.Method == method && op.Path == path {
			return op
		}
	}
	t.Fatalf("no %s %s in the spec", method, path)
	return nil
}

// TestParsePetstore (#1939): the representative spec yields every operation
// with its tag, summary, effective security and Accept type.
func TestParsePetstore(t *testing.T) {
	spec := load(t, "petstore.yaml")
	if spec.Version != "3.0.3" {
		t.Errorf("version = %q, want 3.0.3", spec.Version)
	}
	if spec.Title != "Swagger Petstore" || spec.APIVersion != "1.0.0" {
		t.Errorf("info = %q %q", spec.Title, spec.APIVersion)
	}
	if len(spec.Operations) != 6 {
		t.Fatalf("got %d operations, want 6", len(spec.Operations))
	}
	if got := spec.TagDescriptions["pets"]; got != "Everything about your Pets" {
		t.Errorf("tag description = %q", got)
	}
	if len(spec.Skipped) != 0 {
		t.Errorf("nothing should be skipped, got %v", spec.Skipped)
	}

	list := find(t, spec, "GET", "/pets")
	if list.OperationID != "listPets" || list.Summary != "List all pets" || list.Tag != "pets" {
		t.Errorf("listPets = %+v", list)
	}
	if list.Accept != "application/json" {
		t.Errorf("Accept = %q, want application/json", list.Accept)
	}
	if !reflect.DeepEqual(list.Security, []string{"bearerAuth"}) {
		t.Errorf("security = %v, want the document default", list.Security)
	}

	// An operation's own `security` overrides the document's, and an empty
	// list means "no credential at all".
	if del := find(t, spec, "DELETE", "/pets/{petId}"); !reflect.DeepEqual(del.Security, []string{"apiKeyAuth"}) {
		t.Errorf("deletePet security = %v", del.Security)
	} else if !del.Deprecated {
		t.Error("deletePet is declared deprecated")
	}
	if health := find(t, spec, "GET", "/health"); len(health.Security) != 0 {
		t.Errorf("health security = %v, want none", health.Security)
	} else if health.Tag != "" {
		t.Errorf("health tag = %q, want untagged", health.Tag)
	}
}

// TestParseServerVariables (#1939): a templated server URL resolves through
// the declared defaults, so the host is dispatchable as written.
func TestParseServerVariables(t *testing.T) {
	spec := load(t, "petstore.yaml")
	want := "https://eu.petstore.example.com/v1"
	if len(spec.Servers) != 1 || spec.Servers[0] != want {
		t.Errorf("servers = %v, want [%s]", spec.Servers, want)
	}
}

// TestParseParameters (#1939): path-item parameters reach every operation of
// the path, a path parameter is required whatever the document says, and the
// suggested value comes from example → default → first enum → type.
func TestParseParameters(t *testing.T) {
	spec := load(t, "petstore.yaml")
	show := find(t, spec, "GET", "/pets/{petId}")
	byName := map[string]Param{}
	for _, p := range show.Params {
		byName[p.Name] = p
	}
	pet, ok := byName["petId"]
	if !ok {
		t.Fatal("the path item's petId must reach the operation")
	}
	if pet.In != "path" || !pet.Required || pet.Example != "7" {
		t.Errorf("petId = %+v, want a required path param with example 7", pet)
	}
	hdr, ok := byName["X-Request-Id"]
	if !ok {
		t.Fatal("the operation's own header parameter is missing")
	}
	if hdr.In != "header" || hdr.Required || hdr.Example != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("X-Request-Id = %+v, want an optional header with a uuid stand-in", hdr)
	}

	list := find(t, spec, "GET", "/pets")
	for _, p := range list.Params {
		switch p.Name {
		case "limit":
			if p.Required || p.Example != "20" { // schema default
				t.Errorf("limit = %+v, want optional with the schema default", p)
			}
		case "status":
			if !p.Required || p.Example != "available" { // first enum value
				t.Errorf("status = %+v, want required with the first enum value", p)
			}
		}
	}
}

// TestParseRequestBody (#1939): a $ref'd JSON body becomes a pretty-printed
// example carrying exactly the schema's required members.
func TestParseRequestBody(t *testing.T) {
	spec := load(t, "petstore.yaml")
	create := find(t, spec, "POST", "/pets")
	if create.Body == nil || create.Body.MediaType != "application/json" || !create.Body.Required {
		t.Fatalf("body = %+v", create.Body)
	}
	if create.Body.Example != "{\n  \"name\": \"string\"\n}" {
		t.Errorf("body example =\n%s\nwant only the required \"name\" member", create.Body.Example)
	}

	// example/default of the individual members win over the type stand-in.
	order := find(t, spec, "POST", "/store/orders")
	if want := "{\n  \"petId\": 7,\n  \"quantity\": 1\n}"; order.Body.Example != want {
		t.Errorf("order body =\n%s\nwant\n%s", order.Body.Example, want)
	}
}

// TestParseJSONAndYAMLAgree (#1939): the same document in either syntax
// produces the same model — and therefore the same generated file.
func TestParseJSONAndYAMLAgree(t *testing.T) {
	fromYAML := Generate(load(t, "petstore.yaml"), Options{SpecName: "petstore"})
	fromJSON := Generate(load(t, "petstore.json"), Options{SpecName: "petstore"})
	if fromYAML.HTTP != fromJSON.HTTP {
		t.Errorf("YAML and JSON disagree:\n--- yaml ---\n%s\n--- json ---\n%s", fromYAML.HTTP, fromJSON.HTTP)
	}
	if fromYAML.Env != fromJSON.Env || fromYAML.PrivateEnv != fromJSON.PrivateEnv {
		t.Error("YAML and JSON produce different environments")
	}
}

// TestParseRejectsUnsupportedDocuments (#1939): everything that is not an
// OpenAPI 3.x document fails with a message naming what it is instead.
func TestParseRejectsUnsupportedDocuments(t *testing.T) {
	swagger, err := os.ReadFile("testdata/swagger2.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		src  []byte
		want string
	}{
		{"swagger 2.0", swagger, "Swagger 2.0 is not supported"},
		{"no version field", []byte(`{"paths": {}}`), `no "openapi" version field`},
		{"future major", []byte("openapi: 4.0.0\npaths: {}\n"), "unsupported OpenAPI version"},
		{"not an object", []byte("- one\n- two\n"), "top level is not an object"},
		{"garbage", []byte("openapi: [3.0.0\n\tbroken"), "cannot parse spec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src)
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestParseTolerance (#1939): a document the reader cannot fully represent
// still yields every operation it can, and says what it left out.
func TestParseTolerance(t *testing.T) {
	spec := load(t, "edge.yaml")
	if len(spec.Operations) != 9 {
		t.Fatalf("got %d operations, want 9 — a skipped detail must not drop a request", len(spec.Operations))
	}
	want := []string{
		`security scheme weird: unsupported type "mutualTLS"`,
		"external reference other.yaml#/components/schemas/Thing not resolved",
		`parameter trace has unsupported location "matrix"`,
		"request body application/xml left empty",
	}
	joined := strings.Join(spec.Skipped, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("skip log does not mention %q:\n%s", w, joined)
		}
	}
	// The unsupported parameter is dropped, the usable ones survive.
	search := find(t, spec, "GET", "/search")
	for _, p := range search.Params {
		if p.Name == "trace" {
			t.Error("a parameter with an unsupported location must not be generated")
		}
	}
}

// TestParseSchemaComposition (#1939): allOf merges its branches, oneOf takes
// the first, and a self-referential schema terminates.
func TestParseSchemaComposition(t *testing.T) {
	spec := load(t, "edge.yaml")
	if got := find(t, spec, "POST", "/compose").Body.Example; got != "{\n  \"extra\": \"string\",\n  \"id\": 0\n}" {
		t.Errorf("allOf body =\n%s", got)
	}
	if got := find(t, spec, "POST", "/either").Body.Example; got != "{\n  \"id\": 0\n}" {
		t.Errorf("oneOf body =\n%s", got)
	}
	// A recursive schema must not hang: the response is only read for its
	// media type, but the same walker builds it.
	if got := find(t, spec, "GET", "/tree").Accept; got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
}

// TestParseSecuritySchemes (#1939): every scheme kind the generator can write
// a credential for is kept, the rest are skipped by name.
func TestParseSecuritySchemes(t *testing.T) {
	spec := load(t, "edge.yaml")
	for _, name := range []string{"basicAuth", "queryKey", "oidc"} {
		if _, ok := spec.Schemes[name]; !ok {
			t.Errorf("scheme %s must be usable", name)
		}
	}
	if _, ok := spec.Schemes["weird"]; ok {
		t.Error("mutualTLS has no request-file spelling and must be skipped")
	}
	if key := spec.Schemes["queryKey"]; key.Type != "apiKey" || key.In != "query" || key.Param != "api_key" {
		t.Errorf("queryKey = %+v", key)
	}
}

// TestParseFormBody (#1939): an urlencoded body renders as key=value pairs
// rather than being left empty.
func TestParseFormBody(t *testing.T) {
	spec := load(t, "edge.yaml")
	form := find(t, spec, "POST", "/form")
	if form.Body.MediaType != "application/x-www-form-urlencoded" {
		t.Fatalf("media type = %q", form.Body.MediaType)
	}
	if form.Body.Example != "agree=false&name=string" {
		t.Errorf("form body = %q", form.Body.Example)
	}
}

// TestStringExampleByFormat (#1939): a formatted string field gets a value of
// that format, not the literal word "string".
func TestStringExampleByFormat(t *testing.T) {
	cases := map[string]string{
		"date-time": "2024-01-01T00:00:00Z",
		"date":      "2024-01-01",
		"uuid":      "00000000-0000-0000-0000-000000000000",
		"email":     "user@example.com",
		"":          "string",
		"unknown":   "string",
	}
	for format, want := range cases {
		if got := stringExample(format); got != want {
			t.Errorf("stringExample(%q) = %q, want %q", format, got, want)
		}
	}
}

// TestSortOperations (#1939): tag, then path, then read order of methods —
// untagged last, so a re-import diffs cleanly against the previous one.
func TestSortOperations(t *testing.T) {
	ops := []*Operation{
		{Tag: "", Path: "/health", Method: "GET"},
		{Tag: "store", Path: "/orders", Method: "GET"},
		{Tag: "pets", Path: "/pets", Method: "POST"},
		{Tag: "pets", Path: "/pets", Method: "GET"},
		{Tag: "pets", Path: "/animals", Method: "DELETE"},
	}
	sortOperations(ops)
	var got []string
	for _, op := range ops {
		got = append(got, op.Tag+" "+op.Method+" "+op.Path)
	}
	want := []string{
		"pets DELETE /animals",
		"pets GET /pets",
		"pets POST /pets",
		"store GET /orders",
		" GET /health",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order =\n%v\nwant\n%v", got, want)
	}
}
