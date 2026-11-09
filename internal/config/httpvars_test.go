package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// httpVarsOpts returns Options with a user and a project layer in temp dirs.
func httpVarsOpts(t *testing.T, user, project string) Options {
	t.Helper()
	opts := Options{UserPath: filepath.Join(t.TempDir(), "settings.toml"), ProjectRoot: t.TempDir()}
	if user != "" {
		if err := os.WriteFile(opts.UserPath, []byte(user), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if project != "" {
		dir := filepath.Join(opts.ProjectRoot, ".ike")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.toml"), []byte(project), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return opts
}

// TestHTTPVariablesKeepBothLayers: unlike other lists, the project layer's
// [[http.variables]] does not hide the user layer's — both load, each
// tagged with its scope, and the project value wins in the chain maps.
func TestHTTPVariablesKeepBothLayers(t *testing.T) {
	opts := httpVarsOpts(t,
		"[[http.variables]]\nname = \"host\"\nvalue = \"https://user.test\"\n\n[[http.variables]]\nname = \"token\"\nvalue = \"u\"\n",
		"[http]\ntimeout_ms = 5000\n\n[[http.variables]]\nname = \"host\"\nvalue = \"https://project.test\"\n",
	)
	c, diags := Load(opts)
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %+v", d)
	}
	want := []HTTPVariable{
		{Name: "host", Value: "https://project.test", Scope: ProjectScope},
		{Name: "host", Value: "https://user.test", Scope: UserScope},
		{Name: "token", Value: "u", Scope: UserScope},
	}
	if len(c.HTTP.Variables) != len(want) {
		t.Fatalf("variables = %+v", c.HTTP.Variables)
	}
	for i, w := range want {
		if c.HTTP.Variables[i] != w {
			t.Errorf("variable %d = %+v, want %+v", i, c.HTTP.Variables[i], w)
		}
	}
	if c.HTTP.TimeoutMs != 5000 {
		t.Errorf("the rest of the [http] table must still merge: timeout_ms = %d", c.HTTP.TimeoutMs)
	}
	project, user := c.HTTP.HTTPVariableMaps()
	if project["host"] != "https://project.test" || user["host"] != "https://user.test" || user["token"] != "u" {
		t.Errorf("maps = %v / %v", project, user)
	}
}

// TestHTTPVariablesRoundTrip: what WriteKey persists per scope loads back
// into the same scope.
func TestHTTPVariablesRoundTrip(t *testing.T) {
	opts := httpVarsOpts(t, "", "")
	if err := WriteKey(opts, UserScope, "http.variables", []map[string]any{{"name": "a", "value": "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteKey(opts, ProjectScope, "http.variables", []map[string]any{{"name": "b", "value": "{{a}}/x"}}); err != nil {
		t.Fatal(err)
	}
	c, diags := Load(opts)
	if len(diags) != 0 {
		t.Fatalf("diags: %+v", diags)
	}
	want := []HTTPVariable{{Name: "b", Value: "{{a}}/x", Scope: ProjectScope}, {Name: "a", Value: "1", Scope: UserScope}}
	if len(c.HTTP.Variables) != 2 || c.HTTP.Variables[0] != want[0] || c.HTTP.Variables[1] != want[1] {
		t.Fatalf("variables = %+v", c.HTTP.Variables)
	}
}

// TestHTTPVariablesDropInvalid: an invalid name, a duplicate in the same
// scope and a non-scalar value are dropped with a diagnostic; the same name
// in the other scope is not a duplicate.
func TestHTTPVariablesDropInvalid(t *testing.T) {
	opts := httpVarsOpts(t,
		"[[http.variables]]\nname = \"ok\"\nvalue = \"1\"\n\n"+
			"[[http.variables]]\nname = \"1bad\"\nvalue = \"x\"\n\n"+
			"[[http.variables]]\nname = \"ok\"\nvalue = \"2\"\n\n"+
			"[[http.variables]]\nname = \"port\"\nvalue = 8080\n\n"+
			"[[http.variables]]\nname = \"list\"\nvalue = [1]\n",
		"[[http.variables]]\nname = \"ok\"\nvalue = \"p\"\n\n[[http.variables]]\nname = \"has space\"\nvalue = \"x\"\n",
	)
	c, diags := Load(opts)
	var got []string
	for _, v := range c.HTTP.Variables {
		got = append(got, scopeName(v.Scope)+":"+v.Name+"="+v.Value)
	}
	if strings.Join(got, " ") != "project:ok=p user:ok=1 user:port=8080" {
		t.Errorf("kept %v", got)
	}
	n := 0
	for _, d := range diags {
		if d.Field == "http.variables" {
			n++
		} else {
			t.Errorf("unexpected diagnostic: %+v", d)
		}
	}
	if n != 4 {
		t.Errorf("got %d http.variables diagnostics, want 4: %+v", n, diags)
	}
}

func TestHTTPVariableNameError(t *testing.T) {
	for _, name := range []string{"host", "_x", "api.base-url", "A1"} {
		if msg := HTTPVariableNameError(name); msg != "" {
			t.Errorf("%q: %s", name, msg)
		}
	}
	for _, name := range []string{"", "1a", "a b", "-x", "a{b}", "$env"} {
		if HTTPVariableNameError(name) == "" {
			t.Errorf("%q must be rejected", name)
		}
	}
}
