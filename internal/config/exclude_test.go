package config

// exclude_test.go covers the explorer.exclude schema plumbing (#1139): the
// documented default list, the comma-joined Flat form, glob validation, and
// the TOML-array write-back round trip the settings List control uses.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplorerExcludeDefaultAndFlat(t *testing.T) {
	c, _ := Load(Options{})
	want := []string{".git", ".idea", ".DS_Store"}
	if len(c.Explorer.Exclude) != len(want) {
		t.Fatalf("default exclude = %v, want %v", c.Explorer.Exclude, want)
	}
	for i := range want {
		if c.Explorer.Exclude[i] != want[i] {
			t.Fatalf("default exclude = %v, want %v", c.Explorer.Exclude, want)
		}
	}
	if got := c.Flat()["explorer.exclude"]; got != ".git,.idea,.DS_Store" {
		t.Fatalf("Flat explorer.exclude = %q, want comma-joined default", got)
	}
}

func TestExplorerExcludeValidateDropsBadGlobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(path, []byte("[explorer]\nexclude = [\"[\", \"*.pyc\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, diags := Load(Options{UserPath: path})
	if len(c.Explorer.Exclude) != 1 || c.Explorer.Exclude[0] != "*.pyc" {
		t.Fatalf("exclude after validation = %v, want [*.pyc]", c.Explorer.Exclude)
	}
	found := false
	for _, d := range diags {
		if d.Field == "explorer.exclude" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an explorer.exclude diagnostic, got %v", diags)
	}
}

// TestExplorerExcludeWriteRoundTrip: WriteKey persists a []string as a TOML
// array that loads back typed — the settings panel's List commit path.
func TestExplorerExcludeWriteRoundTrip(t *testing.T) {
	opts := Options{UserPath: filepath.Join(t.TempDir(), "settings.toml")}
	if err := WriteKey(opts, UserScope, "explorer.exclude", []string{".git", "*.pyc"}); err != nil {
		t.Fatal(err)
	}
	c, diags := Load(opts)
	for _, d := range diags {
		if d.Field == "explorer.exclude" || d.Field == "(merge)" {
			t.Fatalf("unexpected diagnostic: %v", d)
		}
	}
	if len(c.Explorer.Exclude) != 2 || c.Explorer.Exclude[0] != ".git" || c.Explorer.Exclude[1] != "*.pyc" {
		t.Fatalf("round-tripped exclude = %v, want [.git *.pyc]", c.Explorer.Exclude)
	}
	// An explicitly empty user list overrides the default (no exclusions).
	if err := WriteKey(opts, UserScope, "explorer.exclude", []string{}); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(opts)
	if len(c.Explorer.Exclude) != 0 {
		t.Fatalf("empty user list should override the default, got %v", c.Explorer.Exclude)
	}
}
