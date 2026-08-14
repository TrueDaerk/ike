package httpfile

import (
	"strings"
	"testing"
)

// TestParseVarDefinitions: `@name=value` lines are collected in file order,
// with whitespace around the "=" and the value stripped, and never mistaken
// for a request line (#1867).
func TestParseVarDefinitions(t *testing.T) {
	f := Parse(strings.Join([]string{
		"@host=https://example.com",
		"@token  =  s3cret  ",
		"@empty=",
		"",
		"### thing",
		"@path=/things",
		"GET {{host}}{{path}}",
	}, "\n"))
	if len(f.Errors) != 0 {
		t.Fatalf("definitions must not parse as request lines: %v", f.Errors)
	}
	want := []Variable{
		{Name: "host", Value: "https://example.com", Line: 1},
		{Name: "token", Value: "s3cret", Line: 2},
		{Name: "empty", Value: "", Line: 3},
		{Name: "path", Value: "/things", Line: 6},
	}
	if len(f.Vars) != len(want) {
		t.Fatalf("vars: %+v, want %+v", f.Vars, want)
	}
	for i, v := range want {
		if f.Vars[i] != v {
			t.Errorf("var %d: %+v, want %+v", i, f.Vars[i], v)
		}
	}
	if len(f.Requests) != 1 || f.Requests[0].Target != "{{host}}{{path}}" {
		t.Fatalf("requests: %+v", f.Requests)
	}
}

// TestVarMapLastDefinitionWins: a name defined twice reads as a
// re-assignment.
func TestVarMapLastDefinitionWins(t *testing.T) {
	f := Parse("@host=a\n@host=b\n\nGET https://{{host}}/\n")
	if got := f.VarMap()["host"]; got != "b" {
		t.Errorf("host=%q, want b", got)
	}
	if Parse("GET https://x/").VarMap() != nil {
		t.Error("a file without definitions must yield no map")
	}
}

// TestVarDefinitionRejectsNonDefinitions: only the `@name=value` shape is a
// definition; everything else stays whatever it was (a request line, and
// therefore an error when malformed).
func TestVarDefinitionRejectsNonDefinitions(t *testing.T) {
	for _, line := range []string{"@no-value", "@1bad=x", "GET https://x/", "  @ =v", "#@host=x"} {
		if _, _, ok := VarDefinition(line); ok {
			t.Errorf("%q must not be a definition", line)
		}
	}
	if f := Parse("@no-value\n"); len(f.Errors) != 1 {
		t.Errorf("a bare @ line stays an invalid request line: %v", f.Errors)
	}
}

// TestResolveFileVariables: the acceptance case — `@host=…` plus
// `GET {{host}}/path` substitutes the target (#1867).
func TestResolveFileVariables(t *testing.T) {
	f := Parse(strings.Join([]string{
		"@host=https://example.com",
		"@token=abc",
		"",
		"### thing",
		"GET {{host}}/my/path",
		"Authorization: Bearer {{ token }}",
		"",
		`{"at": "{{host}}"}`,
	}, "\n"))
	r, err := f.Requests[0].ResolveVars(&Vars{File: f.VarMap()})
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != "https://example.com/my/path" {
		t.Errorf("target: %q", r.Target)
	}
	if v, _ := r.Header("Authorization"); v != "Bearer abc" {
		t.Errorf("header: %q", v)
	}
	if r.Body != `{"at": "https://example.com"}` {
		t.Errorf("body: %q", r.Body)
	}
	if f.Requests[0].Target != "{{host}}/my/path" {
		t.Errorf("original mutated: %q", f.Requests[0].Target)
	}
}

// TestVarsPrecedence: in-file definitions override the environment file,
// which overrides the process environment; a name defined nowhere falls all
// the way through (#1867).
func TestVarsPrecedence(t *testing.T) {
	vars := &Vars{
		File:   map[string]string{"host": "file"},
		Env:    map[string]string{"host": "env", "port": "8080"},
		Lookup: lookupMap(map[string]string{"host": "os", "port": "1", "user": "root"}),
	}
	for _, c := range []struct{ in, want string }{
		{"{{host}}", "file"},
		{"{{port}}", "8080"},
		{"{{user}}", "root"},
	} {
		got, err := SubstituteVars(c.in, vars)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLegacyFormsStayEnvironmentOnly: ${NAME} and {{$env NAME}} keep meaning
// "the process environment" — user variables never shadow them (#1867).
func TestLegacyFormsStayEnvironmentOnly(t *testing.T) {
	vars := &Vars{
		File:   map[string]string{"HOST": "file"},
		Env:    map[string]string{"HOST": "env"},
		Lookup: lookupMap(map[string]string{"HOST": "os"}),
	}
	for _, in := range []string{"${HOST}", "{{$env HOST}}"} {
		got, err := SubstituteVars(in, vars)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != "os" {
			t.Errorf("%s = %q, want os", in, got)
		}
	}
	// And without a process environment they stay unresolved rather than
	// silently taking a same-named file variable.
	if _, err := SubstituteVars("${HOST}", &Vars{File: map[string]string{"HOST": "file"}}); err == nil {
		t.Error("want an unresolved-placeholder error")
	}
}

// TestVarsNested: a definition may refer to another variable, and a cycle
// reports the placeholder as unresolved instead of recursing forever.
func TestVarsNested(t *testing.T) {
	vars := &Vars{
		File:   map[string]string{"api": "{{host}}/api", "host": "https://{{domain}}", "loop": "{{loop2}}", "loop2": "{{loop}}"},
		Env:    map[string]string{"domain": "example.com"},
		Lookup: lookupMap(map[string]string{}),
	}
	got, err := SubstituteVars("{{api}}/v1", vars)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/api/v1" {
		t.Errorf("got %q", got)
	}
	if _, err := SubstituteVars("{{loop}}", vars); err == nil {
		t.Error("a cycle must not resolve")
	}
}

// TestUnresolvedUserVariableAborts: an undefined {{name}} fails the request
// and names the variable, exactly like the older forms (#1867).
func TestUnresolvedUserVariableAborts(t *testing.T) {
	f := Parse("@host=h\nGET {{host}}/{{missing}}\nX-A: {{alsoMissing}}\n")
	r, err := f.Requests[0].ResolveVars(&Vars{File: f.VarMap()})
	if err == nil {
		t.Fatal("want error")
	}
	if r != nil {
		t.Error("resolved request must be nil on error")
	}
	if !strings.Contains(err.Error(), "alsoMissing") || !strings.Contains(err.Error(), "missing") {
		t.Errorf("error must name the variables: %q", err.Error())
	}
}

// TestSubstituteResolvesUserFormFromLookup: the single-lookup Substitute —
// what a caller without user variables uses — serves {{name}} from that same
// lookup, so nothing needs the chain to keep working.
func TestSubstituteResolvesUserFormFromLookup(t *testing.T) {
	got, err := Substitute("{{host}}/x", lookupMap(map[string]string{"host": "h"}))
	if err != nil {
		t.Fatal(err)
	}
	if got != "h/x" {
		t.Errorf("got %q", got)
	}
}

// TestNilVarsResolveNothing: the zero chain resolves no user variable and
// reports it, rather than panicking on nil maps.
func TestNilVarsResolveNothing(t *testing.T) {
	if _, err := SubstituteVars("{{host}}", nil); err == nil {
		t.Error("want an unresolved-placeholder error")
	}
	if got, err := SubstituteVars("plain", &Vars{}); err != nil || got != "plain" {
		t.Errorf("got %q, %v", got, err)
	}
}
