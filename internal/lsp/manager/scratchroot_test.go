package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	langreg "ike/internal/lang"
	"ike/internal/lsp"
)

// scratchroot_test.go covers #2612: a scratch file is served by the active
// project's server (and therefore its toolchain), while every other file keeps
// the root detected from its own location.

// scratchProject sets up an isolated scratch store plus a project with a
// go.mod marker and returns (project root, an allocated scratch path).
func scratchProject(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", filepath.Join(base, "config"))
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := scratchFile(t)
	if err != nil {
		t.Fatal(err)
	}
	return root, path
}

// scratchFile creates one file inside the (env-overridden) scratch store.
func scratchFile(t *testing.T) (string, error) {
	t.Helper()
	dir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "scratches")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "scratch-1.go")
	return path, os.WriteFile(path, []byte("package main\n"), 0o644)
}

// A scratch opens on the project's server: same root, same server key, one
// server for project file and scratch together.
func TestScratchAttachesToProjectRoot(t *testing.T) {
	root, scratchPath := scratchProject(t)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()
	m.SetProjectRoot(root)

	projPath := filepath.Join(root, "main.go")
	if err := m.Open(projPath, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(scratchPath, "go", "package main"); err != nil {
		t.Fatal(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.servers) != 1 {
		t.Fatalf("servers = %d, want one shared by project and scratch", len(m.servers))
	}
	doc, proj := m.docs[scratchPath], m.docs[projPath]
	if doc == nil || proj == nil {
		t.Fatal("both documents must be open")
	}
	if doc.root != root {
		t.Fatalf("scratch root = %q, want the project root %q", doc.root, root)
	}
	if doc.srvKey != proj.srvKey {
		t.Fatalf("scratch server key = %q, want the project's %q", doc.srvKey, proj.srvKey)
	}
}

// A regular file outside the project keeps its own detected root: the rule is
// for the scratch store, not for everything opened from elsewhere on disk.
func TestForeignFileKeepsDetectedRoot(t *testing.T) {
	root, _ := scratchProject(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "go.mod"), []byte("module y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()
	m.SetProjectRoot(root)

	foreign := filepath.Join(other, "main.go")
	if err := m.Open(foreign, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if got := m.docs[foreign].root; got != other {
		t.Fatalf("foreign file root = %q, want its own detected root %q", got, other)
	}
}

// Without a project root (the manager was never told), a scratch behaves as
// before: the root is detected from its own location.
func TestScratchWithoutProjectRootDetects(t *testing.T) {
	_, scratchPath := scratchProject(t)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	if err := m.Open(scratchPath, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if got, want := m.docs[scratchPath].root, filepath.Dir(scratchPath); got != want {
		t.Fatalf("scratch root = %q, want the detected %q", got, want)
	}
}

// A scratch inside the project tree (a store relocated under the project) is
// already under the root and needs no redirect — detectRoot finds the project.
func TestScratchUnderProjectRootUnaffected(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	t.Setenv("IKE_CONFIG_DIR", root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scratchPath, err := scratchFile(t)
	if err != nil {
		t.Fatal(err)
	}
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()
	m.SetProjectRoot(root)

	if err := m.Open(scratchPath, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if got := m.docs[scratchPath].root; got != root {
		t.Fatalf("in-tree scratch root = %q, want %q", got, root)
	}
}

// Idle handling closes the root's scratch documents too (#1521 + #2612): the
// scratch lies outside the tree but belongs to the stopped server, and the
// next open respawns it.
func TestCloseRootClosesScratchDocs(t *testing.T) {
	root, scratchPath := scratchProject(t)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()
	m.SetProjectRoot(root)

	projPath := filepath.Join(root, "main.go")
	if err := m.Open(projPath, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(scratchPath, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	if got := m.RootDocs(root); len(got) != 2 {
		t.Fatalf("RootDocs = %v, want both documents", got)
	}

	m.CloseRoot(root)

	if _, ok := m.DocLines(scratchPath); ok {
		t.Fatal("the scratch document must close with its root")
	}
	m.mu.Lock()
	servers, docs := len(m.servers), len(m.docs)
	m.mu.Unlock()
	if servers != 0 || docs != 0 {
		t.Fatalf("after CloseRoot: servers = %d, docs = %d, want none", servers, docs)
	}

	// Reopening works: the lazy respawn puts the scratch back on the project's
	// fresh server.
	if err := m.Open(scratchPath, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.servers) != 1 {
		t.Fatalf("servers after reopen = %d, want one", len(m.servers))
	}
	if got := m.docs[scratchPath].root; got != root {
		t.Fatalf("reopened scratch root = %q, want %q", got, root)
	}
}

// rootToolchain reports the root it was detected against, so a test can assert
// which root the settings a server received were built from.
type rootToolchain struct{}

func (rootToolchain) Detect(root string) (map[string]any, bool) {
	return map[string]any{"marker": map[string]any{"root": root}}, true
}

// The project's toolchain settings reach the scratch's server: a scratch gets
// the project interpreter/module setup, not the system one (#2612).
func TestScratchServerGetsProjectToolchain(t *testing.T) {
	langreg.Register(langreg.Language{ID: "toolscratch", Toolchain: rootToolchain{}})
	root, scratchPath := scratchProject(t)
	initOpts := make(chan string, 1)
	spec := lsp.ServerSpec{Language: "toolscratch", Command: "fake"}
	m := New(resolver(spec), capturingConnector(initOpts), Callbacks{})
	defer m.Shutdown()
	m.SetProjectRoot(root)

	if err := m.Open(scratchPath, "toolscratch", "x"); err != nil {
		t.Fatal(err)
	}
	select {
	case opts := <-initOpts:
		if !strings.Contains(opts, root) {
			t.Fatalf("initializationOptions = %s, want the project root %q detected", opts, root)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server never initialized")
	}
}
