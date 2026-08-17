//go:build goplssmoke

// Real-gopls smoke coverage for the #1912 features (code lens, folding
// ranges, selection ranges, willRenameFiles), gated like the base smoke test:
// go test -tags goplssmoke ./internal/lsp/manager/ -run Smoke -v
package manager

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/lsp"
)

// smoke1912Manager builds a manager against a real gopls with the test code
// lens enabled (off by default in gopls; IKE's Go plugin baseline mirrors
// this in its own settings).
func smoke1912Manager(t *testing.T) *Manager {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH")
	}
	spec := lsp.ServerSpec{
		Language: "go", Command: "gopls", RootMarkers: []string{"go.mod"},
		Settings: map[string]any{"codelenses": map[string]any{"test": true}},
	}
	m := New(func(string) (lsp.ServerSpec, bool) { return spec, true }, nil, Callbacks{
		Status: func(lang, text string, kind lsp.ServerStatusKind) { t.Logf("status: %s", text) },
	})
	t.Cleanup(m.Shutdown)
	return m
}

func write1912(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSmokeGopls1912FoldingCodeLensSelection(t *testing.T) {
	m := smoke1912Manager(t)
	dir := t.TempDir()
	write1912(t, filepath.Join(dir, "go.mod"), "module smoke\n\ngo 1.21\n")

	path := filepath.Join(dir, "main_test.go")
	src := "package main\n\nimport (\n\t\"strings\"\n\t\"testing\"\n)\n\nfunc TestUpper(t *testing.T) {\n\tif strings.ToUpper(\"a\") != \"A\" {\n\t\tt.Fatal(\"nope\")\n\t}\n}\n"
	write1912(t, path, src)
	if err := m.Open(path, "go", src); err != nil {
		t.Fatalf("open: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Folding: the import block (lines 2-5) must fold with the imports kind.
	folds, err := m.FoldingRanges(ctx, path)
	if err != nil {
		t.Fatalf("foldingRanges: %v", err)
	}
	t.Logf("folds: %+v", folds)
	foundImports := false
	for _, f := range folds {
		if f.HeaderLine == 2 && f.EndLine >= 3 {
			foundImports = true
			t.Logf("import fold kind=%q [%d,%d]", f.Kind, f.HeaderLine, f.EndLine)
		}
	}
	if !foundImports {
		t.Errorf("expected an import-block fold starting at line 2, got %+v", folds)
	}

	// Code lens: the "run test" lens on the TestUpper line.
	lenses, err := m.CodeLenses(ctx, path)
	if err != nil {
		t.Fatalf("codeLenses: %v", err)
	}
	t.Logf("lenses: %+v", lenses)
	foundTest := false
	for _, l := range lenses {
		if l.Line == 7 && strings.Contains(strings.ToLower(l.Title), "test") {
			foundTest = true
		}
	}
	if !foundTest {
		t.Errorf("expected a run-test lens on line 7, got %+v", lenses)
	}

	// Selection ranges: at "ToUpper" (line 8, inside the condition) the
	// ladder must be non-trivial and strictly widening.
	ranges, err := m.SelectionRanges(ctx, path, 8, 15)
	if err != nil {
		t.Fatalf("selectionRanges: %v", err)
	}
	t.Logf("selection ladder: %+v", ranges)
	if len(ranges) < 3 {
		t.Errorf("expected a ladder of >=3 ranges, got %d", len(ranges))
	}
}

func TestSmokeGopls1912WillRenameFiles(t *testing.T) {
	m := smoke1912Manager(t)
	dir := t.TempDir()
	write1912(t, filepath.Join(dir, "go.mod"), "module smoke\n\ngo 1.21\n")
	utilPath := filepath.Join(dir, "util", "util.go")
	write1912(t, utilPath, "package util\n\nfunc Answer() int { return 42 }\n")
	mainPath := filepath.Join(dir, "main.go")
	mainSrc := "package main\n\nimport \"smoke/util\"\n\nfunc main() { _ = util.Answer() }\n"
	write1912(t, mainPath, mainSrc)

	if err := m.Open(mainPath, "go", mainSrc); err != nil {
		t.Fatalf("open: %v", err)
	}
	// Give gopls a moment to index the workspace before asking for edits.
	time.Sleep(2 * time.Second)

	// gopls up to at least v0.23 does not implement workspace/willRenameFiles
	// (its internal/server/unimplemented.go) and advertises no
	// fileOperations, so the correct end-to-end behavior is the graceful
	// no-op: no error, no edits, the rename proceeds. Servers that do
	// advertise the capability (rust-analyzer, typescript-language-server)
	// must produce edits — the fake-server tests (willrename_test.go) cover
	// that path; this assertion upgrades automatically the day gopls gains
	// support.
	advertises := false
	m.mu.Lock()
	for k, srv := range m.servers {
		caps := srv.cl.Caps()
		t.Logf("server %s willRename=%v filters=%+v", k, caps.WillRename, caps.WillRenameFilters)
		advertises = advertises || caps.WillRename
	}
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	files, err := m.WillRenameFiles(ctx, filepath.Join(dir, "util"), filepath.Join(dir, "helper"))
	if err != nil {
		t.Fatalf("willRenameFiles: %v", err)
	}
	t.Logf("edit files: %+v", files)
	touched := false
	for _, f := range files {
		if f.Path == mainPath {
			touched = true
			for _, e := range f.Edits {
				t.Logf("  edit [%d:%d-%d:%d] -> %q", e.StartLine, e.StartCol, e.EndLine, e.EndCol, e.Text)
			}
		}
	}
	if advertises && !touched {
		t.Errorf("server advertises willRename but produced no edits for %s: %+v", mainPath, files)
	}
	if !advertises && len(files) != 0 {
		t.Errorf("no server advertises willRename, expected the no-op, got %+v", files)
	}
}
