//go:build ike_complexity_only

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/complexity"
	"ike/internal/lang"
)

// TestComplexityOnlyRefusesTheIDE: every invocation the full build would
// hand to the IDE, the agent hook or the deep-link socket exits 2 with the
// one documented line (#2939).
func TestComplexityOnlyRefusesTheIDE(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"somefile.go"},
		{"somefile.go:12"},
		{"agent-hook", "SessionStart"},
		{"agent-hook"},
		{"ike://open?path=x.go"},
		{"--url-send-only", "ike://open?path=x.go"},
		{"-"},
		{"--bogus"},
	} {
		var stdout, stderr bytes.Buffer
		code := runComplexityOnly(args, &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || stderr.String() != complexityOnlyRefusal+"\n" {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
}

// TestComplexityOnlyVersionBanner: --version / -v keep working and say which
// build answered.
func TestComplexityOnlyVersionBanner(t *testing.T) {
	for _, flag := range []string{"--version", "-v"} {
		var stdout, stderr bytes.Buffer
		if code := runComplexityOnly([]string{flag}, &stdout, &stderr); code != 0 {
			t.Fatalf("%s: exit %d, stderr %q", flag, code, stderr.String())
		}
		if got := stdout.String(); !strings.HasPrefix(got, "ike ") || !strings.HasSuffix(got, " (complexity-only)\n") {
			t.Fatalf("%s: banner %q, want the complexity-only marker", flag, got)
		}
	}
}

// TestComplexityOnlyUsageError: a malformed `ike complexity` fails with the
// subcommand usage, exactly as in the full build.
func TestComplexityOnlyUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runComplexityOnly([]string{"complexity", "--bogus"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "usage: ike complexity") {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
}

// TestComplexityOnlyLanguages: every language with a spec in the full build
// is linked here with its spec, and every other language is known by its
// file patterns, so the skipped line matches the full build.
func TestComplexityOnlyLanguages(t *testing.T) {
	for _, id := range complexitySpecLanguages {
		if l, ok := lang.ByID(id); !ok || l.Complexity == nil {
			t.Errorf("spec'd language %q is not linked into the complexity-only build", id)
		}
	}
	for _, d := range detectionOnlyLanguages {
		if _, ok := lang.ByID(d.ID); !ok {
			t.Errorf("language %q is not registered", d.ID)
		}
		for _, e := range d.Extensions {
			if l, ok := lang.ByExt(e); !ok || l.ID != d.ID {
				t.Errorf(".%s resolves to %q, want %q", e, l.ID, d.ID)
			}
		}
		for _, n := range d.Filenames {
			if l, ok := lang.ByPath(n); !ok || l.ID != d.ID {
				t.Errorf("%s resolves to %q, want %q", n, l.ID, d.ID)
			}
		}
	}
}

// TestComplexityOnlyMeasures runs the report end to end on a small tree: the
// Go file is measured, the YAML file is skipped by name.
func TestComplexityOnlyMeasures(t *testing.T) {
	if !complexity.Available() {
		t.Skip("no Tree-sitter grammars in a CGo-disabled build")
	}
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package p\n\nfunc f(a int) int {\n\tif a > 0 && a < 9 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n")
	write("ci.yaml", "a: 1\n")
	var stdout, stderr bytes.Buffer
	if code := runComplexityOnly([]string{"complexity", "--json", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	var rep struct {
		Functions []struct {
			Name string `json:"name"`
			CCN  int    `json:"ccn"`
		} `json:"functions"`
		Skipped struct {
			Files     int      `json:"files"`
			Languages []string `json:"languages"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("%v\n%s", err, stdout.String())
	}
	if len(rep.Functions) != 1 || rep.Functions[0].Name != "f" || rep.Functions[0].CCN != 3 {
		t.Errorf("functions = %+v, want f at CCN 3", rep.Functions)
	}
	if rep.Skipped.Files != 1 || len(rep.Skipped.Languages) != 1 || rep.Skipped.Languages[0] != "yaml" {
		t.Errorf("skipped = %+v, want the yaml file", rep.Skipped)
	}
}
