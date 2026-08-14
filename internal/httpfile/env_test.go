package httpfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeEnvFiles drops the given file contents into a fresh directory.
func writeEnvFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLoadEnvironments: named environments load with their variables, sorted
// by name so a picker lists them stably (#1867).
func TestLoadEnvironments(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		EnvFileName: `{"prod": {"host": "https://example.com"}, "dev": {"host": "https://dev.example.com", "retries": 3}}`,
	})
	envs, err := LoadEnvironments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := envs.Names(); len(got) != 2 || got[0] != "dev" || got[1] != "prod" {
		t.Fatalf("names: %v, want [dev prod]", got)
	}
	if got := envs.Vars("dev")["host"]; got != "https://dev.example.com" {
		t.Errorf("dev host: %q", got)
	}
	if got := envs.Vars("dev")["retries"]; got != "3" {
		t.Errorf("a number must keep its written spelling, got %q", got)
	}
	if !envs.Has("prod") || envs.Has("stage") {
		t.Error("Has must answer for known and unknown names")
	}
	if envs.Vars("stage") != nil {
		t.Error("an unknown environment has no variables")
	}
}

// TestLoadEnvironmentsPrivateOverrides: http-client.private.env.json wins over
// the public file per variable, and may add environments of its own (#1867).
func TestLoadEnvironmentsPrivateOverrides(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		EnvFileName:        `{"dev": {"host": "https://dev.example.com", "token": "public"}}`,
		PrivateEnvFileName: `{"dev": {"token": "s3cret"}, "local": {"host": "http://localhost:8080"}}`,
	})
	envs, err := LoadEnvironments(dir)
	if err != nil {
		t.Fatal(err)
	}
	dev := envs.Vars("dev")
	if dev["token"] != "s3cret" {
		t.Errorf("private value must win: %q", dev["token"])
	}
	if dev["host"] != "https://dev.example.com" {
		t.Errorf("public value must survive: %q", dev["host"])
	}
	if envs.Vars("local")["host"] != "http://localhost:8080" {
		t.Errorf("private-only environment missing: %+v", envs.Names())
	}
}

// TestLoadEnvironmentsMissing: a directory without environment files is the
// normal case — no environments, no error.
func TestLoadEnvironmentsMissing(t *testing.T) {
	envs, err := LoadEnvironments(t.TempDir())
	if err != nil {
		t.Fatalf("a missing file must not error: %v", err)
	}
	if envs.Len() != 0 || envs.Names() != nil {
		t.Errorf("want no environments, got %v", envs.Names())
	}
	// The nil receiver answers like an empty set (no file, no picker).
	var none *Environments
	if none.Len() != 0 || none.Has("dev") || none.Vars("dev") != nil || none.Names() != nil {
		t.Error("the nil set must answer empty")
	}
}

// TestLoadEnvironmentsMalformed: a file that exists but does not parse is an
// error naming it — silently ignoring a typo would send requests with
// unresolved variables (#1867).
func TestLoadEnvironmentsMalformed(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{EnvFileName: `{"dev": {`})
	_, err := LoadEnvironments(dir)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), EnvFileName) {
		t.Errorf("error must name the file: %q", err.Error())
	}
	// A malformed private file does not lose the public environments.
	dir = writeEnvFiles(t, map[string]string{
		EnvFileName:        `{"dev": {"host": "h"}}`,
		PrivateEnvFileName: "nonsense",
	})
	envs, err := LoadEnvironments(dir)
	if err == nil {
		t.Fatal("want an error for the private file")
	}
	if envs.Vars("dev")["host"] != "h" {
		t.Errorf("the readable file must still load: %+v", envs.Names())
	}
}

// TestLoadEnvironmentsValueKinds: JSON scalars become the strings a
// placeholder is replaced with.
func TestLoadEnvironmentsValueKinds(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		EnvFileName: `{"dev": {"s": "text", "n": 1.50, "b": true, "z": null, "o": {"a": 1}}}`,
	})
	envs, err := LoadEnvironments(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"s": "text", "n": "1.50", "b": "true", "z": "", "o": `{"a":1}`}
	for k, v := range want {
		if got := envs.Vars("dev")[k]; got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

// TestEnvironmentVarsResolve: the loaded environment feeds {{name}} through
// the chain, the end-to-end acceptance case for env files (#1867).
func TestEnvironmentVarsResolve(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		EnvFileName: `{"prod": {"host": "https://example.com"}}`,
	})
	envs, err := LoadEnvironments(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := Parse("GET {{host}}/my/path\n")
	r, err := f.Requests[0].ResolveVars(&Vars{File: f.VarMap(), Env: envs.Vars("prod")})
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != "https://example.com/my/path" {
		t.Errorf("target: %q", r.Target)
	}
}
