package manager

import (
	"context"
	"path/filepath"
	"testing"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestManagerInlayHints converts the server's UTF-16 hint positions to editor
// rune coordinates, flattens both label shapes, keeps kind/padding, and sorts
// the (deliberately unordered) server reply by position (#171).
func TestManagerInlayHints(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	// "a🙂bcdefghij": the emoji is 2 UTF-16 units, so unit offset 3 is rune
	// column 2 and unit 7 is rune 6.
	if err := m.Open(path, "go", "a🙂bcdefghij"); err != nil {
		t.Fatal(err)
	}
	hints, err := m.InlayHints(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 2 {
		t.Fatalf("hints = %+v, want 2", hints)
	}
	want := []lsp.InlayHint{
		{Line: 0, Col: 2, Label: "int", Kind: protocol.InlayHintType, PadLeft: true},
		{Line: 0, Col: 6, Label: "n:", Kind: protocol.InlayHintParameter, PadRight: true},
	}
	for i, w := range want {
		if hints[i] != w {
			t.Errorf("hints[%d] = %+v, want %+v", i, hints[i], w)
		}
	}
}

// TestManagerInlayHintsGated yields nothing when the server lacks the
// capability — no request, no error.
func TestManagerInlayHintsGated(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, noInlayHint: true}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	hints, err := m.InlayHints(context.Background(), path)
	if err != nil || len(hints) != 0 {
		t.Fatalf("gated request should be a no-op, got %+v, %v", hints, err)
	}
}
