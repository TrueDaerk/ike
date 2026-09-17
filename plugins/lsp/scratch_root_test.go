package lsp

import (
	"os"
	"path/filepath"
	"testing"

	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// TestCloseRootPrunesAttachedScratchState guards #2612 on the bridge side: a
// scratch attached to the project root is a document of that root, so the
// idle/close teardown must drop its per-path bridge caches too — its path lies
// outside the tree, where the plain containment check does not reach.
func TestCloseRootPrunesAttachedScratchState(t *testing.T) {
	base := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", filepath.Join(base, "config"))
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scratchDir := filepath.Join(base, "config", "scratches")
	if err := os.MkdirAll(scratchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scratchPath := filepath.Join(scratchDir, "scratch-1.go")
	if err := os.WriteFile(scratchPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := ilsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) {
		if lang == spec.Language {
			return spec, true
		}
		return ilsp.ServerSpec{}, false
	}
	mgr := manager.New(resolve, renameConnector(renameCaps(false)), manager.Callbacks{})
	defer mgr.Shutdown()
	mgr.SetProjectRoot(root)
	if err := mgr.Open(scratchPath, "go", "package main\n"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := mgr.RootDocs(root); len(got) != 1 || got[0] != scratchPath {
		t.Fatalf("RootDocs = %v, want the scratch attached to %q", got, root)
	}

	elsewhere := filepath.Join(base, "other", "main.go")
	b := &bridge{mgr: mgr, diags: map[string][]protocol.Diagnostic{
		scratchPath: {{Message: "x"}},
		elsewhere:   {{Message: "y"}},
	}}
	if cmd := b.workspaceIdle(root); cmd != nil {
		cmd() // runs CloseRoot
	}
	if _, ok := b.diags[scratchPath]; ok {
		t.Fatal("the attached scratch's diagnostics must be pruned with its root")
	}
	if _, ok := b.diags[elsewhere]; !ok {
		t.Fatal("an unrelated out-of-tree path must survive")
	}
	if _, ok := mgr.DocLines(scratchPath); ok {
		t.Fatal("the scratch document must close with its root")
	}
}
