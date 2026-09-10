package langjson

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/format"
)

// writeTool drops an executable printing the given payload into dir.
func writeTool(t *testing.T, dir, name, payload string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '" + payload + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runJSON(t *testing.T) string {
	t.Helper()
	prov, ok := format.Resolve("json", "a.json")
	if !ok {
		t.Fatal("json default must resolve")
	}
	res, err := prov.Format(context.Background(), format.Request{
		Path: "a.json", Language: "json", Lines: []string{"{}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return *res.Text
}

// TestJSONPrettierBeforeBiome: prettier wins, biome serves without it, and
// the project-local install beats both PATH installs (#2596).
func TestJSONPrettierBeforeBiome(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "prettier", "prettier")
	writeTool(t, dir, "biome", "biome")
	t.Setenv("PATH", dir)
	t.Chdir(t.TempDir())

	if got := runJSON(t); got != "prettier" {
		t.Fatalf("prettier must win, got %q", got)
	}

	biomeOnly := t.TempDir()
	writeTool(t, biomeOnly, "biome", "biome")
	t.Setenv("PATH", biomeOnly)
	if got := runJSON(t); got != "biome" {
		t.Fatalf("biome must serve without prettier, got %q", got)
	}

	project := t.TempDir()
	writeTool(t, filepath.Join(project, "node_modules", ".bin"), "prettier", "local-prettier")
	t.Chdir(project)
	if got := runJSON(t); got != "local-prettier" {
		t.Fatalf("node_modules prettier must win, got %q", got)
	}
}

// TestJSONSpecArgs asserts the recorded stdin-mode argument list.
func TestJSONSpecArgs(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "prettier", "prettier")
	t.Setenv("PATH", dir)
	t.Chdir(t.TempDir())

	spec, ok := format.ExternalDefault("json")
	if !ok {
		t.Fatal("json must record a default spec")
	}
	if spec.Command != "prettier" {
		t.Fatalf("want prettier, got %q", spec.Command)
	}
	if strings.Join(spec.Args, " ") != "--stdin-filepath ${FILE}" {
		t.Fatalf("unexpected args %v", spec.Args)
	}
	if spec.Install != "npm install -g prettier" {
		t.Fatalf("unexpected install hint %q", spec.Install)
	}
}

// TestNDJSONStaysUnformatted: one JSON document per line is the point of
// ndjson, so it deliberately keeps no external default.
func TestNDJSONStaysUnformatted(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "prettier", "prettier")
	t.Setenv("PATH", dir)
	t.Chdir(t.TempDir())

	if _, ok := format.ExternalDefault("ndjson"); ok {
		t.Fatal("ndjson must not register an external default")
	}
	if _, ok := format.Resolve("ndjson", "a.ndjson"); ok {
		t.Fatal("ndjson must not resolve to a formatter")
	}
}

// TestJSONNoToolHintOnce: nothing installed — no resolution, one hint.
func TestJSONNoToolHintOnce(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())
	var hints []string
	format.SetNotifier(func(text string) { hints = append(hints, text) })
	t.Cleanup(func() { format.SetNotifier(nil) })

	for i := 0; i < 2; i++ {
		if _, ok := format.Resolve("json", "a.json"); ok {
			t.Fatal("must not resolve without any binary")
		}
	}
	if len(hints) != 1 || !strings.Contains(hints[0], "npm install -g prettier") {
		t.Fatalf("want one prettier hint, got %v", hints)
	}
}
