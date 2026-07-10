package manager

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lsp"
)

// TestStopLangStopsOnlyThatLanguage guards the per-server restart seam (#130):
// stopping one language leaves other languages' servers running, drops the
// stopped language's documents, and reports the survivors via RunningLangs.
func TestStopLangStopsOnlyThatLanguage(t *testing.T) {
	resolve := func(id string) (lsp.ServerSpec, bool) {
		return lsp.ServerSpec{Language: id, Command: "fake", RootMarkers: []string{"go.mod"}}, true
	}
	m := New(resolve, fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	a := filepath.Join(dir, "main.go")
	b := filepath.Join(dir, "main.py")
	_ = os.WriteFile(a, []byte("package main"), 0o644)
	_ = os.WriteFile(b, []byte("x = 1"), 0o644)
	if err := m.Open(a, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(b, "python", "x = 1"); err != nil {
		t.Fatal(err)
	}
	if got := m.RunningLangs(); len(got) != 2 {
		t.Fatalf("setup: want 2 running languages, got %v", got)
	}

	m.StopLang("go")

	if got := m.RunningLangs(); len(got) != 1 || got[0] != "python" {
		t.Fatalf("only python must survive a go stop, got %v", got)
	}
	// The stopped language's document is gone; the survivor's stays.
	m.mu.Lock()
	_, goDoc := m.docs[a]
	_, pyDoc := m.docs[b]
	m.mu.Unlock()
	if goDoc || !pyDoc {
		t.Fatalf("docs after StopLang: go=%v py=%v, want false/true", goDoc, pyDoc)
	}
}
