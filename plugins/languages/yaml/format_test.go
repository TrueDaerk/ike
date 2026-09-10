package langyaml

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

func runYAML(t *testing.T) string {
	t.Helper()
	prov, ok := format.Resolve("yaml", "a.yaml")
	if !ok {
		t.Fatal("yaml default must resolve")
	}
	res, err := prov.Format(context.Background(), format.Request{
		Path: "a.yaml", Language: "yaml", Lines: []string{"a: 1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return *res.Text
}

// TestYAMLPrettierBeforeYamlfmt: prettier wins, yamlfmt serves without it,
// and node_modules beats the PATH install (#2596).
func TestYAMLPrettierBeforeYamlfmt(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "prettier", "prettier")
	writeTool(t, dir, "yamlfmt", "yamlfmt")
	t.Setenv("PATH", dir)
	t.Chdir(t.TempDir())

	if got := runYAML(t); got != "prettier" {
		t.Fatalf("prettier must win, got %q", got)
	}

	fmtOnly := t.TempDir()
	writeTool(t, fmtOnly, "yamlfmt", "yamlfmt")
	t.Setenv("PATH", fmtOnly)
	if got := runYAML(t); got != "yamlfmt" {
		t.Fatalf("yamlfmt must serve without prettier, got %q", got)
	}

	project := t.TempDir()
	writeTool(t, filepath.Join(project, "node_modules", ".bin"), "prettier", "local-prettier")
	t.Chdir(project)
	if got := runYAML(t); got != "local-prettier" {
		t.Fatalf("node_modules prettier must win, got %q", got)
	}
}

// TestYAMLSpecArgs asserts the recorded argument list: prettier's YAML parser
// is named explicitly, because .yml files carry no unambiguous extension for
// every prettier version.
func TestYAMLSpecArgs(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "prettier", "prettier")
	t.Setenv("PATH", dir)
	t.Chdir(t.TempDir())

	spec, ok := format.ExternalDefault("yaml")
	if !ok {
		t.Fatal("yaml must record a default spec")
	}
	if spec.Command != "prettier" {
		t.Fatalf("want prettier, got %q", spec.Command)
	}
	if strings.Join(spec.Args, " ") != "--parser yaml --stdin-filepath ${FILE}" {
		t.Fatalf("unexpected args %v", spec.Args)
	}
	if spec.Install != "npm install -g prettier" {
		t.Fatalf("unexpected install hint %q", spec.Install)
	}
}

// TestYAMLNoToolHintOnce: nothing installed — no resolution, one hint.
func TestYAMLNoToolHintOnce(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())
	var hints []string
	format.SetNotifier(func(text string) { hints = append(hints, text) })
	t.Cleanup(func() { format.SetNotifier(nil) })

	for i := 0; i < 2; i++ {
		if _, ok := format.Resolve("yaml", "a.yaml"); ok {
			t.Fatal("must not resolve without any binary")
		}
	}
	if len(hints) != 1 || !strings.Contains(hints[0], "npm install -g prettier") {
		t.Fatalf("want one prettier hint, got %v", hints)
	}
}
