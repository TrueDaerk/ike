package langweb

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

// runLang resolves the language's chain and reports what the winning tool
// printed.
func runLang(t *testing.T, langID, path string) string {
	t.Helper()
	prov, ok := format.Resolve(langID, path)
	if !ok {
		t.Fatalf("%s default must resolve", langID)
	}
	res, err := prov.Format(context.Background(), format.Request{
		Path: path, Language: langID, Lines: []string{"x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return *res.Text
}

// TestWebPrettierBeforeBiome: prettier wins for every web language; biome
// serves JS/TS and CSS when prettier is missing (#2596).
func TestWebPrettierBeforeBiome(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "prettier", "prettier")
	writeTool(t, dir, "biome", "biome")
	t.Setenv("PATH", dir)
	t.Chdir(t.TempDir()) // no node_modules candidates

	for _, tc := range []struct{ langID, path string }{
		{"typescript", "a.js"}, {"css", "a.css"}, {"html", "a.html"},
	} {
		if got := runLang(t, tc.langID, tc.path); got != "prettier" {
			t.Fatalf("%s: prettier must win, got %q", tc.langID, got)
		}
	}

	biomeOnly := t.TempDir()
	writeTool(t, biomeOnly, "biome", "biome")
	t.Setenv("PATH", biomeOnly)
	for _, tc := range []struct{ langID, path string }{
		{"typescript", "a.ts"}, {"css", "a.scss"},
	} {
		if got := runLang(t, tc.langID, tc.path); got != "biome" {
			t.Fatalf("%s: biome must serve without prettier, got %q", tc.langID, got)
		}
	}
	// HTML has no biome entry — biome's HTML formatter is experimental.
	if _, ok := format.Resolve("html", "a.html"); ok {
		t.Fatal("html must not resolve to biome")
	}
}

// TestWebNodeModulesWins: a project-local node_modules/.bin install beats the
// PATH install, mirroring Python's venv-first chain.
func TestWebNodeModulesWins(t *testing.T) {
	path := t.TempDir()
	writeTool(t, path, "prettier", "path-prettier")
	t.Setenv("PATH", path)
	project := t.TempDir()
	writeTool(t, filepath.Join(project, "node_modules", ".bin"), "prettier", "local-prettier")
	t.Chdir(project)

	if got := runLang(t, "typescript", "a.js"); got != "local-prettier" {
		t.Fatalf("node_modules prettier must win, got %q", got)
	}
}

// TestWebLocalBiomeBeatsPathPrettier: the whole project-local pair is probed
// before either PATH install (the issue's candidate order).
func TestWebLocalBiomeBeatsPathPrettier(t *testing.T) {
	path := t.TempDir()
	writeTool(t, path, "prettier", "path-prettier")
	t.Setenv("PATH", path)
	project := t.TempDir()
	writeTool(t, filepath.Join(project, "node_modules", ".bin"), "biome", "local-biome")
	t.Chdir(project)

	if got := runLang(t, "css", "a.css"); got != "local-biome" {
		t.Fatalf("node_modules biome must win over PATH prettier, got %q", got)
	}
}

// TestWebSpecArgs asserts the recorded stdin-mode argument lists.
func TestWebSpecArgs(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "prettier", "prettier")
	t.Setenv("PATH", dir)
	t.Chdir(t.TempDir())

	for _, langID := range []string{"typescript", "css", "html"} {
		spec, ok := format.ExternalDefault(langID)
		if !ok {
			t.Fatalf("%s must record a default spec", langID)
		}
		if spec.Command != "prettier" {
			t.Fatalf("%s: want prettier, got %q", langID, spec.Command)
		}
		if strings.Join(spec.Args, " ") != "--stdin-filepath ${FILE}" {
			t.Fatalf("%s: unexpected args %v", langID, spec.Args)
		}
		if len(spec.RangeArgs) != 0 {
			t.Fatalf("%s: prettier takes no line range, got %v", langID, spec.RangeArgs)
		}
		if spec.Install != "npm install -g prettier" {
			t.Fatalf("%s: unexpected install hint %q", langID, spec.Install)
		}
	}
}

// TestWebNoToolHintOnce: nothing installed — no resolution, one hint per
// language naming the prettier install.
func TestWebNoToolHintOnce(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())
	var hints []string
	format.SetNotifier(func(text string) { hints = append(hints, text) })
	t.Cleanup(func() { format.SetNotifier(nil) })

	for i := 0; i < 2; i++ {
		if _, ok := format.Resolve("typescript", "a.js"); ok {
			t.Fatal("must not resolve without any binary")
		}
	}
	if len(hints) != 1 || !strings.Contains(hints[0], "npm install -g prettier") {
		t.Fatalf("want one prettier hint, got %v", hints)
	}
}
