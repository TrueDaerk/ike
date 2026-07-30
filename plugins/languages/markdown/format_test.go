package langmarkdown

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ike/internal/format"
)

// TestMarkdownDefaults (#1405): prettier first, mdformat fallback.
func TestMarkdownDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mdformat"), []byte("#!/bin/sh\nprintf 'mdformat'\n"), 0o755)
	t.Setenv("PATH", dir)
	prov, ok := format.Resolve("markdown", "x.md")
	if !ok || prov.DisplayName("x.md") != "mdformat" {
		t.Fatalf("mdformat fallback expected, got ok=%v name=%q", ok, prov.DisplayName("x.md"))
	}
	os.WriteFile(filepath.Join(dir, "prettier"), []byte("#!/bin/sh\nprintf 'prettier'\n"), 0o755)
	prov, _ = format.Resolve("markdown", "x.md")
	if prov.DisplayName("x.md") != "prettier" {
		t.Fatalf("prettier must win when installed, got %q", prov.DisplayName("x.md"))
	}
	res, err := prov.Format(context.Background(), format.Request{Path: "x.md", Language: "markdown", Lines: []string{"# hi"}})
	if err != nil || *res.Text != "prettier" {
		t.Fatalf("err=%v text=%v", err, res.Text)
	}
}
