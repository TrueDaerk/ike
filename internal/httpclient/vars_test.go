package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ike/internal/httpfile"
)

// TestDispatchUserVariables: an .http file's own `@name=value` definitions and
// the selected environment resolve `{{name}}` on the way out, the in-file
// definition winning (#1867).
func TestDispatchUserVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path + " " + r.Header.Get("Authorization")))
	}))
	defer srv.Close()

	f := httpfile.Parse("@path=/my/path\n\nGET {{host}}{{path}}\nAuthorization: Bearer {{token}}\n")
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("parse: errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	opts := noConfig
	opts.Vars = &httpfile.Vars{
		File: f.VarMap(),
		Env:  map[string]string{"host": srv.URL, "token": "s3cret", "path": "/ignored"},
	}
	resp, err := Dispatch(context.Background(), f.Requests[0], opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "/my/path Bearer s3cret" {
		t.Errorf("server saw %q", resp.Body)
	}
}

// TestDispatchUnresolvedUserVariableAborts: a `{{name}}` nothing defines fails
// the dispatch, naming it — nothing broken is ever sent (#1867).
func TestDispatchUnresolvedUserVariableAborts(t *testing.T) {
	req := parseOne(t, "GET https://example.invalid/{{missing}}\n")
	opts := noConfig
	opts.Lookup = func(string) (string, bool) { return "", false }
	_, err := Dispatch(context.Background(), req, opts)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("want unresolved-placeholder error, got %v", err)
	}
}

// TestDispatchVarsCloseOverEnvironment: Options.Lookup still closes the chain,
// so `{{name}}` reaches the process environment when no user variable defines
// it, while `${NAME}` keeps resolving from it alone.
func TestDispatchVarsCloseOverEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()

	req := parseOne(t, "GET ${BASE}/{{SUFFIX}}\n")
	opts := noConfig
	opts.Vars = &httpfile.Vars{File: map[string]string{"other": "x"}}
	opts.Lookup = func(k string) (string, bool) {
		switch k {
		case "BASE":
			return srv.URL, true
		case "SUFFIX":
			return "tail", true
		}
		return "", false
	}
	resp, err := Dispatch(context.Background(), req, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "/tail" {
		t.Errorf("server saw %q", resp.Body)
	}
	if opts.Vars.Lookup != nil {
		t.Error("the caller's Vars must not be mutated by a dispatch")
	}
}

// TestSubstitutedBodyFileUsesUserVariables: the `<@ file` body form (#1305)
// substitutes the file's own placeholders through the same chain (#1867).
func TestSubstitutedBodyFileUsesUserVariables(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "payload.json", `{"user": "{{user}}"}`)
	req := parseOne(t, "POST "+srv.URL+"/x\n\n<@ ./payload.json\n")
	opts := noConfig
	opts.BaseDir = dir
	opts.Vars = &httpfile.Vars{File: map[string]string{"user": "ada"}}
	if _, err := Dispatch(context.Background(), req, opts); err != nil {
		t.Fatal(err)
	}
	if gotBody != `{"user": "ada"}` {
		t.Errorf("body: %q", gotBody)
	}
}
