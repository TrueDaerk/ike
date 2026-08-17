package manager

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestManagerRefreshCallback answers a server's workspace/codeLens/refresh and
// hands the kind to Callbacks.Refresh (#1912). The fake fires the request when
// it sees didSave.
func TestManagerRefreshCallback(t *testing.T) {
	kinds := make(chan string, 4)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{
		syncKind:      protocol.SyncFull,
		refreshOnSave: []string{"workspace/codeLens/refresh", "workspace/inlayHint/refresh"},
	}), Callbacks{Refresh: func(kind string) { kinds <- kind }})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for range 2 {
		select {
		case kind := <-kinds:
			got[kind] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("refresh callback missing, got %v", got)
		}
	}
	if !got["codeLens"] || !got["inlayHint"] {
		t.Fatalf("kinds = %v, want codeLens and inlayHint", got)
	}
}

// TestManagerSemanticTokensRefreshClearsCache drops every document's semantic
// delta cache on workspace/semanticTokens/refresh (#1912), so the next request
// is a full one instead of a delta against a result id the server no longer
// honours.
func TestManagerSemanticTokensRefreshClearsCache(t *testing.T) {
	kinds := make(chan string, 4)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{
		syncKind:      protocol.SyncFull,
		refreshOnSave: []string{"workspace/semanticTokens/refresh"},
	}), Callbacks{Refresh: func(kind string) { kinds <- kind }})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SemanticTokens(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	resultID := m.docs[path].semResultID
	m.mu.Unlock()
	if resultID != "r1" {
		t.Fatalf("semResultID = %q, want the fake's r1 before the refresh", resultID)
	}

	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	select {
	case kind := <-kinds:
		if kind != "semanticTokens" {
			t.Fatalf("kind = %q, want semanticTokens", kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("refresh callback missing")
	}
	// The cache clears before the request is answered, so the callback's
	// arrival orders after it.
	m.mu.Lock()
	resultID = m.docs[path].semResultID
	data := m.docs[path].semData
	m.mu.Unlock()
	if resultID != "" || data != nil {
		t.Fatalf("semantic cache survived the refresh: id=%q data=%v", resultID, data)
	}
}
