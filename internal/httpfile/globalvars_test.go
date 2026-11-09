package httpfile

import "testing"

// TestGlobalVarsSitBelowTheProcessEnvironment: global variables are the
// chain's defaults — captured values, file definitions, the environment file
// and the process environment all win over them, and a project variable wins
// over a user one.
func TestGlobalVarsSitBelowTheProcessEnvironment(t *testing.T) {
	vars := &Vars{
		Captured: map[string]string{"captured": "captured"},
		File:     map[string]string{"file": "file", "captured": "file"},
		Env:      map[string]string{"env": "env", "file": "env"},
		Lookup:   lookupMap(map[string]string{"proc": "os", "env": "os"}),
		Project:  map[string]string{"proc": "project", "project": "project", "both": "project", "env": "project", "file": "project", "captured": "project"},
		User:     map[string]string{"proc": "user", "both": "user", "user": "user", "file": "user"},
	}
	for _, c := range []struct{ in, want string }{
		{"{{captured}}", "captured"},
		{"{{file}}", "file"},
		{"{{env}}", "env"},
		{"{{proc}}", "os"},
		{"{{both}}", "project"},
		{"{{project}}", "project"},
		{"{{user}}", "user"},
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

// TestGlobalVarsExpandNested: a global value may hold placeholders, resolved
// through the whole chain; a cycle leaves the variable unresolved.
func TestGlobalVarsExpandNested(t *testing.T) {
	vars := &Vars{
		File:    map[string]string{"version": "v2"},
		Project: map[string]string{"api": "{{base}}/api/{{version}}", "loop": "{{loop2}}"},
		User:    map[string]string{"base": "https://{{domain}}", "domain": "example.com", "loop2": "{{loop}}"},
		Lookup:  lookupMap(map[string]string{}),
	}
	got, err := SubstituteVars("{{api}}/x", vars)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/api/v2/x" {
		t.Errorf("got %q", got)
	}
	if _, err := SubstituteVars("{{loop}}", vars); err == nil {
		t.Error("a cycle must not resolve")
	}
}

// TestGlobalVarsLegacyFormsUnaffected: ${NAME} and {{$env NAME}} keep
// meaning the process environment, never a global variable.
func TestGlobalVarsLegacyFormsUnaffected(t *testing.T) {
	vars := &Vars{User: map[string]string{"HOST": "user"}, Lookup: lookupMap(map[string]string{})}
	for _, in := range []string{"${HOST}", "{{$env HOST}}"} {
		if _, err := SubstituteVars(in, vars); err == nil {
			t.Errorf("%s must not resolve from a global variable", in)
		}
	}
}

// TestDefinitionsTagsGlobalOrigins: global variables are enumerated with
// their own origins; a higher rung keeps a shared name, and a name the
// process environment answers is tagged with it and its value.
func TestDefinitionsTagsGlobalOrigins(t *testing.T) {
	v := &Vars{
		File:    map[string]string{"host": "file"},
		Lookup:  lookupMap(map[string]string{"HOME": "/home/me"}),
		Project: map[string]string{"host": "project", "base": "project"},
		User:    map[string]string{"base": "user", "token": "user", "HOME": "user"},
	}
	got := map[string]Definition{}
	for _, d := range v.Definitions() {
		got[d.Name] = d
	}
	want := map[string]Definition{
		"host":  {Name: "host", Value: "file", Origin: OriginFile},
		"base":  {Name: "base", Value: "project", Origin: OriginProject},
		"token": {Name: "token", Value: "user", Origin: OriginUser},
		"HOME":  {Name: "HOME", Value: "/home/me", Origin: OriginProcess},
	}
	if len(got) != len(want) {
		t.Fatalf("definitions: %+v", got)
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %+v, want %+v", name, got[name], w)
		}
	}
}

// TestDefinesCountsGlobalVars: a name only a global variable defines is a
// known reference.
func TestDefinesCountsGlobalVars(t *testing.T) {
	v := &Vars{Project: map[string]string{"a": "1"}, User: map[string]string{"b": "2"}}
	if !v.Defines("a") || !v.Defines("b") {
		t.Error("global variables must count as defined")
	}
	if v.Defines("c") {
		t.Error("an undefined name must not count as defined")
	}
}
