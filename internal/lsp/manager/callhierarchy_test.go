package manager

import (
	"context"
	"path/filepath"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

func TestManagerCallHierarchy(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}

	items, err := m.PrepareCallHierarchy(context.Background(), path, buffer.Position{Line: 0, Col: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "sym@0:4" {
		t.Fatalf("prepare should round-trip the position, items = %+v", items)
	}

	in, err := m.IncomingCalls(context.Background(), path, items[0])
	if err != nil {
		t.Fatal(err)
	}
	// The fake echoes the item's name and opaque data token, proving the
	// prepared item round-trips verbatim into the follow-up request.
	if len(in) != 1 || in[0].From.Name != `caller-of-sym@0:4-"token"` {
		t.Fatalf("incoming calls wrong: %+v", in)
	}
	if len(in[0].FromRanges) != 1 || in[0].FromRanges[0].Start.Line != 5 {
		t.Fatalf("fromRanges wrong: %+v", in[0].FromRanges)
	}

	out, err := m.OutgoingCalls(context.Background(), path, items[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].To.Name != "callee" {
		t.Fatalf("outgoing calls wrong: %+v", out)
	}
}

func TestManagerCallHierarchyGatedOnCapability(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, noCallHierarchy: true}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	items, err := m.PrepareCallHierarchy(context.Background(), path, buffer.Position{})
	if err != nil || items != nil {
		t.Fatalf("missing capability should no-op, got %v / %v", items, err)
	}
}
