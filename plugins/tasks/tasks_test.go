package tasks

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lang"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(ts []lang.Task) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

func TestMakeProviderEnumeratesTargets(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile", `
# comment
VERSION = 1.0
.PHONY: all build
all: build
	@echo all
build:
	go build ./...
test lint: build
	go test ./...
$(GEN): input
%.o: %.c
	cc -c $<
`)
	got := names(makeProvider{}.Tasks(dir))
	want := []string{"all", "build", "test", "lint"}
	if len(got) != len(want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("targets = %v, want %v", got, want)
		}
	}
	ts := makeProvider{}.Tasks(dir)
	if a := ts[1].Argv; len(a) != 2 || a[0] != "make" || a[1] != "build" {
		t.Errorf("build argv = %v, want [make build]", a)
	}
	if len(ts[0].Matchers) == 0 {
		t.Error("tasks should carry default matchers")
	}
}

func TestMakeProviderNoMakefile(t *testing.T) {
	if ts := (makeProvider{}).Tasks(t.TempDir()); ts != nil {
		t.Fatalf("expected nil without a Makefile, got %v", ts)
	}
}

func TestNpmProviderEnumeratesScripts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{
  "name": "demo",
  "scripts": {"build": "tsc", "test": "vitest run"}
}`)
	ts := npmProvider{}.Tasks(dir)
	if len(ts) != 2 {
		t.Fatalf("scripts = %v, want 2", names(ts))
	}
	for _, task := range ts {
		if task.Source != "npm" || len(task.Argv) != 3 || task.Argv[0] != "npm" || task.Argv[1] != "run" || task.Argv[2] != task.Name {
			t.Errorf("task %+v malformed", task)
		}
	}
}

func TestNpmProviderMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{not json`)
	if ts := (npmProvider{}).Tasks(dir); ts != nil {
		t.Fatalf("expected nil on malformed package.json, got %v", ts)
	}
}

func TestJustProviderEnumeratesRecipes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "justfile", `
set shell := ["bash", "-c"]
version := "1.0"
# comment
build:
    go build ./...
test filter='': build
    go test ./...
_private:
    echo hidden
@quiet:
    echo shh
`)
	got := names(justProvider{}.Tasks(dir))
	want := []string{"build", "test", "quiet"}
	if len(got) != len(want) {
		t.Fatalf("recipes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recipes = %v, want %v", got, want)
		}
	}
}

func TestProvidersRegisteredAndAggregated(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile", "zeta:\n\techo z\nalpha:\n\techo a\n")
	write(t, dir, "package.json", `{"scripts": {"start": "node ."}}`)
	got := lang.Tasks(dir)
	// Per-provider name sort: make's targets alphabetical, then npm's.
	want := []string{"alpha", "zeta", "start"}
	if len(got) != len(want) {
		t.Fatalf("aggregate = %v, want %v", names(got), want)
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("aggregate = %v, want %v", names(got), want)
		}
	}
}
