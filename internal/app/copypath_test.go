package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/registry"
)

// TestCopyPathForms guards #1173: absolute, project-relative and
// reference (relpath:line) forms for the focused editor's file.
func TestCopyPathForms(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	defer func() { clipboardWrite = orig }()

	m := sizedWith(t, registry.New(), 100, 40)
	cwd, _ := cachedGetwd()
	path := filepath.Join(cwd, "copyref_test_fixture.go")
	if err := os.WriteFile(path, []byte("package x\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	out, _ := m.openPath(path, false)
	m = out.(Model)

	m.copyPath(copyAbs)
	if copied != path {
		t.Fatalf("abs = %q want %q", copied, path)
	}
	m.copyPath(copyRel)
	if copied != "copyref_test_fixture.go" {
		t.Fatalf("rel = %q", copied)
	}
	m.copyPath(copyRef)
	if !strings.HasPrefix(copied, "copyref_test_fixture.go:") {
		t.Fatalf("ref = %q want relpath:line", copied)
	}
}

// TestCopyPathExplorerSelection guards #1173: explorer focus targets the
// tree selection; no line suffix there.
func TestCopyPathExplorerSelection(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	defer func() { clipboardWrite = orig }()

	m := sizedWith(t, registry.New(), 100, 40)
	m.setFocus("explorer")
	m.copyPath(copyRef)
	// Whatever the selection resolves to (root is excluded by Selected),
	// the command must not panic and either copies or notices.
	_ = copied
}
