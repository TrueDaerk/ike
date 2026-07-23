package manager

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	langreg "ike/internal/lang"
	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestOpenDelegatingLanguage covers the ServerLanguage seam (#1063): a
// document of a delegating language ("modaux", standing in for go.mod)
// attaches to its delegate's server — same spec resolution, same instance —
// while the didOpen languageId stays the delegating language's own id.
func TestOpenDelegatingLanguage(t *testing.T) {
	langreg.Register(langreg.Language{ID: "modhost"})
	langreg.Register(langreg.Language{ID: "modaux", Filenames: []string{"go.mod"}, ServerLanguage: "modhost"})

	opens := make(chan protocol.DidOpenTextDocumentParams, 2)
	spec := lsp.ServerSpec{Language: "modhost", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.Open(filepath.Join(dir, "main.go"), "modhost", "package main"); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(filepath.Join(dir, "go.mod"), "modaux", "module x"); err != nil {
		t.Fatal(err)
	}

	got := map[string]string{} // uri base -> languageId
	for i := 0; i < 2; i++ {
		select {
		case p := <-opens:
			got[filepath.Base(string(p.TextDocument.URI))] = p.TextDocument.LanguageID
		case <-time.After(3 * time.Second):
			t.Fatal("missing didOpen")
		}
	}
	if got["main.go"] != "modhost" {
		t.Errorf("main.go languageId = %q, want modhost", got["main.go"])
	}
	if got["go.mod"] != "modaux" {
		t.Errorf("go.mod languageId = %q, want modaux", got["go.mod"])
	}

	// Both documents share one server instance under the delegate's key.
	if langs := m.RunningLangs(); len(langs) != 1 || langs[0] != "modhost" {
		t.Errorf("RunningLangs = %v, want [modhost]", langs)
	}
	m.mu.Lock()
	nServers := len(m.servers)
	auxDoc := m.docs[filepath.Join(dir, "go.mod")]
	m.mu.Unlock()
	if nServers != 1 {
		t.Errorf("servers = %d, want 1 (delegation must not spawn a second instance)", nServers)
	}
	if auxDoc == nil || auxDoc.lang != "modhost" || auxDoc.langID != "modaux" {
		t.Errorf("aux document = %+v, want lang modhost / langID modaux", auxDoc)
	}

	// StopLang on the delegate drops the delegating document too.
	m.StopLang("modhost")
	m.mu.Lock()
	remaining := len(m.docs)
	m.mu.Unlock()
	if remaining != 0 {
		t.Errorf("docs after StopLang = %d, want 0", remaining)
	}
}
