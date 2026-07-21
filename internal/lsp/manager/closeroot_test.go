package manager

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lsp"
)

// TestCloseRootStopsRootServers guards #825: CloseRoot drops every document
// under the closed workspace root and stops the servers rooted there, while a
// sibling project's server and documents stay untouched.
func TestCloseRootStopsRootServers(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pa, pb := filepath.Join(a, "main.go"), filepath.Join(b, "main.go")
	if err := m.Open(pa, "go", "package a"); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(pb, "go", "package b"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	servers := len(m.servers)
	m.mu.Unlock()
	if servers != 2 {
		t.Fatalf("servers = %d, want one per root", servers)
	}

	m.CloseRoot(a)

	if _, ok := m.DocLines(pa); ok {
		t.Fatal("the closed root's document must be dropped")
	}
	if _, ok := m.DocLines(pb); !ok {
		t.Fatal("the sibling root's document must survive")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.servers) != 1 {
		t.Fatalf("servers after CloseRoot = %d, want just b's", len(m.servers))
	}
	for _, srv := range m.servers {
		if srv.root != b {
			t.Fatalf("surviving server root = %q, want %q", srv.root, b)
		}
	}
	if len(m.docs) != 1 || len(m.hostDiags) > 1 {
		t.Fatalf("docs = %d, hostDiags = %d — closed-root state must be gone", len(m.docs), len(m.hostDiags))
	}
}

// TestUnderRoot pins the path containment rule CloseRoot filters by.
func TestUnderRoot(t *testing.T) {
	cases := []struct {
		path, root string
		want       bool
	}{
		{"/p/a/x.go", "/p/a", true},
		{"/p/a", "/p/a", true},
		{"/p/ab/x.go", "/p/a", false},
		{"/p/b/x.go", "/p/a", false},
		{"/p/a/x.go", "", false},
		{"", "/p/a", false},
	}
	for _, c := range cases {
		if got := underRoot(c.path, c.root); got != c.want {
			t.Errorf("underRoot(%q, %q) = %v, want %v", c.path, c.root, got, c.want)
		}
	}
}
