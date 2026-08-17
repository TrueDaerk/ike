package manager

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestManagerCodeLenses flattens the wire lenses onto their anchor lines —
// nil command stays empty and keeps the data token — and sorts the
// (deliberately unordered) server reply by line (#1912).
func TestManagerCodeLenses(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main\n\nfunc main() {}\n"); err != nil {
		t.Fatal(err)
	}
	lenses, err := m.CodeLenses(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lenses) != 2 {
		t.Fatalf("lenses = %+v, want 2", lenses)
	}
	if lenses[0].Line != 0 || lenses[0].Command != "" || lenses[0].Title != "" || string(lenses[0].Data) != `"tok"` {
		t.Errorf("lenses[0] = %+v, want unresolved line-0 lens keeping data", lenses[0])
	}
	if lenses[1].Line != 2 || lenses[1].Command != "test.run" || lenses[1].Title != "run test" {
		t.Errorf("lenses[1] = %+v, want resolved test.run on line 2", lenses[1])
	}
}

// TestManagerCodeLensesGated yields nothing when the server lacks the
// capability — no request, no error.
func TestManagerCodeLensesGated(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, noCodeLens: true}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	lenses, err := m.CodeLenses(context.Background(), path)
	if err != nil || len(lenses) != 0 {
		t.Fatalf("gated request should be a no-op, got %+v, %v", lenses, err)
	}
}

// TestManagerResolveCodeLens round-trips an unresolved lens through
// codeLens/resolve: the anchor line survives, the data token reaches the
// server verbatim, and the returned command fills the empty fields (#1912).
func TestManagerResolveCodeLens(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	in := lsp.CodeLens{Line: 5, Data: json.RawMessage(`"tok"`)}
	out, err := m.ResolveCodeLens(context.Background(), path, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Line != 5 || out.Command != "lens.show" || out.Title != `resolved-"tok"` {
		t.Fatalf("resolved = %+v, want lens.show titled with the round-tripped data", out)
	}
}

// TestManagerResolveCodeLensAlreadyResolved keeps a lens that carries a
// command: no resolve round trip, the input comes back unchanged.
func TestManagerResolveCodeLensAlreadyResolved(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	in := lsp.CodeLens{Line: 1, Title: "run test", Command: "test.run"}
	out, err := m.ResolveCodeLens(context.Background(), path, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Command != "test.run" || out.Title != "run test" || out.Line != 1 {
		t.Fatalf("already-resolved lens must come back unchanged, got %+v", out)
	}
}
