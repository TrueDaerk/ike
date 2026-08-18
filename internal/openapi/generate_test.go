package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ike/internal/httpclient"
	"ike/internal/httpfile"
)

// generated renders a fixture spec and returns the request file plus the two
// environment maps, already decoded.
func generated(t *testing.T, name string) (string, map[string]string, map[string]string) {
	t.Helper()
	res := Generate(load(t, name), Options{SpecName: name})
	return res.HTTP, envVars(t, res.Env), envVars(t, res.PrivateEnv)
}

// envVars decodes one generated environment file's single environment.
func envVars(t *testing.T, src string) map[string]string {
	t.Helper()
	var envs map[string]map[string]string
	if err := json.Unmarshal([]byte(src), &envs); err != nil {
		t.Fatalf("generated environment is not valid JSON: %v\n%s", err, src)
	}
	vars, ok := envs[EnvName]
	if !ok {
		t.Fatalf("generated environment has no %q environment: %s", EnvName, src)
	}
	return vars
}

// TestGenerateParsesAsHTTPFile (#1939): the generated file is a valid .http
// file — every block parses, none of the comments, folded query lines or tag
// headings produce a parse error.
func TestGenerateParsesAsHTTPFile(t *testing.T) {
	for _, name := range []string{"petstore.yaml", "edge.yaml"} {
		t.Run(name, func(t *testing.T) {
			src, _, _ := generated(t, name)
			f := httpfile.Parse(src)
			for _, e := range f.Errors {
				t.Errorf("parse error in generated file: %v", e)
			}
			if len(f.Requests) == 0 {
				t.Fatal("the generated file holds no request")
			}
			if !strings.HasPrefix(src, GeneratedMarker) {
				t.Errorf("the file must open with the generated marker, got %q", firstLine(src))
			}
		})
	}
}

// TestGenerateBlocksMatchOperations (#1939): one block per operation, named
// after its operationId, grouped by tag with the summary as a comment.
func TestGenerateBlocksMatchOperations(t *testing.T) {
	spec := load(t, "petstore.yaml")
	res := Generate(spec, Options{SpecName: "petstore.yaml"})
	f := httpfile.Parse(res.HTTP)
	if len(f.Requests) != len(spec.Operations) {
		t.Fatalf("got %d blocks, want %d operations", len(f.Requests), len(spec.Operations))
	}
	var names []string
	for _, r := range f.Requests {
		names = append(names, r.Name)
	}
	want := []string{"listPets", "createPet", "showPetById", "deletePet", "placeOrder", "health"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("blocks = %v, want %v (tag, then path, then method)", names, want)
	}
	if !strings.Contains(res.HTTP, "### pets\n# Everything about your Pets\n") {
		t.Error("the tag heading and its description must open the group")
	}
	if !strings.Contains(res.HTTP, "### listPets\n# List all pets\n") {
		t.Error("the operation summary must sit above its block")
	}
	if !strings.Contains(res.HTTP, "### deletePet\n# Remove a pet\n# deprecated\n") {
		t.Error("a deprecated operation must say so")
	}
}

// TestGenerateParameterPlaceholders (#1939): path and query parameters become
// {{name}} placeholders, optional query parameters are written as commented
// continuation lines, and the environment seeds every one of them.
func TestGenerateParameterPlaceholders(t *testing.T) {
	src, pub, _ := generated(t, "petstore.yaml")
	if !strings.Contains(src, "GET {{host}}/pets/{{petId}}\n") {
		t.Error("the path parameter must be a placeholder in the request line")
	}
	if !strings.Contains(src, "    ? status = {{status}}\n") {
		t.Error("a required query parameter must be a live folded line")
	}
	if !strings.Contains(src, "#   & limit = {{limit}}\n") {
		t.Error("an optional query parameter must be commented out")
	}
	if !strings.Contains(src, "# X-Request-Id: {{X-Request-Id}}\n") {
		t.Error("an optional header parameter must be commented out")
	}
	for name, want := range map[string]string{
		"host":         "https://eu.petstore.example.com/v1",
		"petId":        "7",
		"limit":        "20",
		"status":       "available",
		"X-Request-Id": "00000000-0000-0000-0000-000000000000",
	} {
		if pub[name] != want {
			t.Errorf("environment %s = %q, want %q", name, pub[name], want)
		}
	}
}

// TestGenerateAuthUsesVariables (#1939): bearer, basic and apiKey schemes all
// become {{name}} placeholders resolved from the *private* environment — a
// generated file never holds a credential.
func TestGenerateAuthUsesVariables(t *testing.T) {
	src, pub, priv := generated(t, "petstore.yaml")
	if !strings.Contains(src, "Authorization: Bearer {{bearerAuth}}\n") {
		t.Error("the bearer scheme must map to a placeholder")
	}
	if !strings.Contains(src, "X-API-Key: {{apiKeyAuth}}\n") {
		t.Error("an apiKey header scheme must map to its header plus a placeholder")
	}
	for _, name := range []string{"bearerAuth", "apiKeyAuth"} {
		if _, ok := priv[name]; !ok {
			t.Errorf("%s belongs in the private environment", name)
		}
		if _, ok := pub[name]; ok {
			t.Errorf("%s must not be in the committed environment", name)
		}
		if priv[name] != "" {
			t.Errorf("%s = %q, want an empty value for the user to fill in", name, priv[name])
		}
	}
	// An operation with `security: []` carries no credential at all.
	if strings.Contains(blockOf(t, src, "health"), "Authorization") {
		t.Error("an operation that opts out of security must carry no Authorization header")
	}

	edge, _, edgePriv := generated(t, "edge.yaml")
	if !strings.Contains(edge, "Authorization: Basic {{basicAuth}}\n") {
		t.Error("the basic scheme must map to a Basic placeholder")
	}
	if !strings.Contains(edge, "    & api_key = {{queryKey}}\n") {
		t.Error("an apiKey query scheme must map to a folded query line")
	}
	if _, ok := edgePriv["queryKey"]; !ok {
		t.Error("a query credential is still a credential and belongs in the private environment")
	}
}

// TestGenerateQueryCredentialStaysFolded (#1939): a query credential must be
// written above the first header line — the parser only folds `?`/`&` lines
// that directly follow the request line, so a misplaced one would break the
// whole block.
func TestGenerateQueryCredentialStaysFolded(t *testing.T) {
	src, _, _ := generated(t, "edge.yaml")
	f := httpfile.Parse(src)
	for _, r := range f.Requests {
		if r.Name != "search" {
			continue
		}
		if !strings.Contains(r.Target, "api_key={{queryKey}}") {
			t.Errorf("target = %q, want the folded credential merged into the query", r.Target)
		}
		if _, ok := r.Header("Cookie"); !ok {
			t.Error("the cookie parameter must survive as a Cookie header")
		}
		return
	}
	t.Fatal("no search block")
}

// TestGenerateIsDeterministic (#1939): two runs over the same document are
// byte-identical, so re-importing a spec diffs cleanly.
func TestGenerateIsDeterministic(t *testing.T) {
	for _, name := range []string{"petstore.yaml", "edge.yaml"} {
		first := Generate(load(t, name), Options{SpecName: name})
		for i := 0; i < 5; i++ {
			again := Generate(load(t, name), Options{SpecName: name})
			if again.HTTP != first.HTTP {
				t.Fatalf("%s: run %d differs from the first", name, i)
			}
			if again.Env != first.Env || again.PrivateEnv != first.PrivateEnv {
				t.Fatalf("%s: run %d produced a different environment", name, i)
			}
		}
	}
}

// TestGenerateSkipsAreVisible (#1939): what could not be generated is listed
// in the result *and* in the file's header, where it is read.
func TestGenerateSkipsAreVisible(t *testing.T) {
	res := Generate(load(t, "edge.yaml"), Options{SpecName: "edge.yaml"})
	if len(res.Skipped) == 0 {
		t.Fatal("the edge fixture must produce skips")
	}
	for _, s := range res.Skipped {
		if !strings.Contains(res.HTTP, "# not generated: "+s) {
			t.Errorf("the header does not mention %q", s)
		}
	}
	if s := SkipSummary(res.Skipped); !strings.HasPrefix(s, "7 skipped (") || !strings.Contains(s, "more)") {
		t.Errorf("SkipSummary = %q", s)
	}
	if SkipSummary(nil) != "" {
		t.Error("an empty skip log has no summary")
	}
}

// TestGenerateDuplicateBlockNames (#1939): two operations sharing an
// operationId must not share a block name — the response history keys on it.
func TestGenerateDuplicateBlockNames(t *testing.T) {
	src, _, _ := generated(t, "edge.yaml")
	f := httpfile.Parse(src)
	seen := map[string]bool{}
	for _, r := range f.Requests {
		if seen[r.Key()] {
			t.Errorf("duplicate request key %q", r.Key())
		}
		seen[r.Key()] = true
	}
	if !seen["duplicate"] || !seen["duplicate 2"] {
		t.Errorf("want both duplicate blocks, got %v", seen)
	}
}

// TestGenerateFallbackHost (#1939): a document with no server still yields
// dispatchable blocks, and says that the host is a guess.
func TestGenerateFallbackHost(t *testing.T) {
	_, pub, _ := generated(t, "edge.yaml")
	if pub[HostVar] != FallbackHost {
		t.Errorf("host = %q, want the fallback %q", pub[HostVar], FallbackHost)
	}
	res := Generate(load(t, "edge.yaml"), Options{})
	if !strings.Contains(strings.Join(res.Skipped, "\n"), "declares no server") {
		t.Error("the missing server must be reported")
	}
}

// TestVarNameSanitizes (#1939): a spec name that is not a legal placeholder
// name is rewritten into one rather than producing a file the parser cannot
// resolve.
func TestVarNameSanitizes(t *testing.T) {
	cases := map[string]string{
		"petId":     "petId",
		"pet-id":    "pet-id",
		"pet id":    "pet_id",
		"2fa":       "_2fa",
		"":          "var",
		"filter[x]": "filter_x_",
	}
	for in, want := range cases {
		if got := varName(in); got != want {
			t.Errorf("varName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestGenerateVariableCollision (#1939): a parameter named like the host
// variable gets its own name instead of rewriting the origin.
func TestGenerateVariableCollision(t *testing.T) {
	spec := &Spec{Version: "3.0.0", Servers: []string{"https://example.com"}, Schemes: map[string]*SecurityScheme{}}
	spec.Operations = []*Operation{{
		Path: "/x", Method: "GET", OperationID: "x",
		Params: []Param{{Name: "host", In: "query", Required: true, Example: "other"}},
	}}
	res := Generate(spec, Options{SpecName: "collide.yaml"})
	if !strings.Contains(res.HTTP, "GET {{host}}/x\n") {
		t.Errorf("the origin must stay {{host}}:\n%s", res.HTTP)
	}
	if !strings.Contains(res.HTTP, "? host = {{host2}}\n") {
		t.Errorf("the colliding parameter must get its own name:\n%s", res.HTTP)
	}
	vars := envVars(t, res.Env)
	if vars["host"] != "https://example.com" || vars["host2"] != "other" {
		t.Errorf("environment = %v", vars)
	}
}

// blockOf returns the text of one named block of a generated file.
func blockOf(t *testing.T, src, name string) string {
	t.Helper()
	f := httpfile.Parse(src)
	lines := strings.Split(src, "\n")
	for _, r := range f.Requests {
		if r.Name != name {
			continue
		}
		return strings.Join(lines[r.BlockStart-1:r.BlockEnd], "\n")
	}
	t.Fatalf("no block named %q", name)
	return ""
}

// TestGeneratedRequestsDispatch (#1939) is the acceptance case: the file a
// representative spec generates is parsed, resolved against the generated
// environment and dispatched against a matching server — placeholders and all.
func TestGeneratedRequestsDispatch(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	res := Generate(load(t, "petstore.yaml"), Options{SpecName: "petstore.yaml"})
	file := httpfile.Parse(res.HTTP)
	if len(file.Errors) != 0 {
		t.Fatalf("generated file does not parse: %v", file.Errors)
	}

	// The environment the import writes, with the host pointed at the test
	// server and the credentials filled in the way a user would.
	env := envVars(t, res.Env)
	env[HostVar] = srv.URL
	for k, v := range envVars(t, res.PrivateEnv) {
		if v == "" {
			v = "test-credential"
		}
		env[k] = v
	}

	opts := httpclient.Options{
		Vars:          &httpfile.Vars{Env: env},
		Lookup:        func(string) (string, bool) { return "", false },
		DisableConfig: true,
	}
	for _, req := range file.Requests {
		resp, err := httpclient.Dispatch(context.Background(), req, opts)
		if err != nil {
			t.Fatalf("%s: %v", req.Key(), err)
		}
		if resp.Status != "200 OK" {
			t.Errorf("%s: status %s", req.Key(), resp.Status)
		}
	}
	if len(seen) != len(file.Requests) {
		t.Fatalf("server saw %d requests, want %d", len(seen), len(file.Requests))
	}
	// The placeholders really were substituted: no braces reach the wire.
	for _, s := range seen {
		if strings.Contains(s, "{") || strings.Contains(s, "%7B") {
			t.Errorf("unsubstituted placeholder on the wire: %s", s)
		}
	}
	want := "GET /pets?status=available"
	if seen[0] != want {
		t.Errorf("first request = %q, want %q", seen[0], want)
	}
	if seen[2] != "GET /pets/7" {
		t.Errorf("path parameter did not substitute: %q", seen[2])
	}
}
