package langhttp

import (
	"os"
	"path/filepath"
	"testing"
)

// placeholder_complete_test.go covers "{{" completion (#2135): the file's
// own @name=value definitions and a sibling http-client.env.json's active
// environment, offered wherever a placeholder can appear — the request line,
// a header value and a body.

// TestCompletePlaceholderFileVar: an unclosed "{{" on the request line
// offers the file's own @-defined variables and closes the braces on accept.
func TestCompletePlaceholderFileVar(t *testing.T) {
	items := completeAt(t, "@host=https://example.com\nGET {{ho|\n")
	if !has(items, "host") {
		t.Fatalf("want host offered, got %v", labels(items))
	}
	if got := insertFor(items, "host"); got != "host}}" {
		t.Errorf("insert text = %q, want %q", got, "host}}")
	}
	if has(items, "GET") {
		t.Error("method items must not leak into a placeholder")
	}
}

// TestCompletePlaceholderHeaderValue: the same offering inside an unclosed
// "{{" in a header value.
func TestCompletePlaceholderHeaderValue(t *testing.T) {
	items := completeAt(t, "@token=s3cret\nGET https://x.test/\nAuthorization: Bearer {{tok|\n")
	if !has(items, "token") {
		t.Fatalf("want token offered, got %v", labels(items))
	}
	if got := insertFor(items, "token"); got != "token}}" {
		t.Errorf("insert text = %q, want %q", got, "token}}")
	}
}

// TestCompletePlaceholderBody: the same offering inside an unclosed "{{" in
// a request body — the completion is not limited to bodies with a mapped
// Content-Type, since a placeholder resolves the same way regardless.
func TestCompletePlaceholderBody(t *testing.T) {
	items := completeAt(t, "@host=https://example.com\n"+
		"POST https://x.test/\nContent-Type: application/json\n\n"+
		`{"target": "{{ho|"}`+"\n")
	if !has(items, "host") {
		t.Fatalf("want host offered in body, got %v", labels(items))
	}
}

// TestCompletePlaceholderClosedDoesNotReopen: a cursor merely parked inside
// an already-closed "{{host}}" (not typing) still offers completion, since
// the closing braces sit past the cursor and out of `before`; a cursor after
// the closing braces must not.
func TestCompletePlaceholderClosedDoesNotReopen(t *testing.T) {
	items := completeAt(t, "@host=https://example.com\nGET {{ho|st}}\n")
	if !has(items, "host") {
		t.Fatalf("cursor inside a closed placeholder must still complete, got %v", labels(items))
	}
	if items := completeAt(t, "@host=https://example.com\nGET {{host}}|\n"); len(items) != 0 {
		t.Errorf("cursor past the closing braces must complete nothing, got %v", labels(items))
	}
}

// TestCompletePlaceholderEnvVar: a lone environment in a sibling
// http-client.env.json needs no explicit selection (#1867's "only one
// choice" default) and its variables complete too.
func TestCompletePlaceholderEnvVar(t *testing.T) {
	dir := t.TempDir()
	env := `{"dev": {"host": "https://dev.example.com", "token": "d3v"}}`
	if err := os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	items := completeAtIn(t, dir, "GET {{ho|\n")
	if !has(items, "host") {
		t.Fatalf("want env var host offered, got %v", labels(items))
	}
	if got := insertFor(items, "host"); got != "host}}" {
		t.Errorf("insert text = %q, want %q", got, "host}}")
	}
}

// TestCompletePlaceholderActiveEnvironment: with several environments
// defined, only the persisted selection's variables complete — mirroring
// the choice internal/app's http.selectEnvironment persists.
func TestCompletePlaceholderActiveEnvironment(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", configDir)

	dir := t.TempDir()
	env := `{"dev": {"host": "dev.example.com"}, "prod": {"host": "prod.example.com"}}`
	if err := os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}

	// Nothing selected yet, and more than one environment exists: neither
	// contributes (an ambiguous default would be worse than none).
	items := completeAtIn(t, dir, "GET {{ho|\n")
	if has(items, "host") {
		t.Fatalf("no selection among several environments must offer nothing, got %v", labels(items))
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := `{"selected": {"` + filepath.ToSlash(abs) + `": "prod"}}`
	if err := os.WriteFile(filepath.Join(configDir, "httpenv.json"), []byte(store), 0o644); err != nil {
		t.Fatal(err)
	}
	items = completeAtIn(t, dir, "GET {{ho|\n")
	if !has(items, "host") {
		t.Fatalf("want the selected environment's host offered, got %v", labels(items))
	}
}
