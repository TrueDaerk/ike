package manager

import (
	"context"
	"path/filepath"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

const inheritanceFixture = "package main\ntype Shape interface {\n\tArea() int\n}\ntype Circle struct{}\nfunc (Circle) Area() int { return 0 }\n\n\nconst answer = 42\n"

func TestManagerImplementation(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", inheritanceFixture); err != nil {
		t.Fatal(err)
	}
	locs, err := m.Implementation(context.Background(), path, buffer.Position{Line: 2, Col: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 || locs[0].URI != "file:///tmp/other.go" {
		t.Fatalf("locations = %+v", locs)
	}
	if !m.ImplementationSupported(path) {
		t.Fatal("ImplementationSupported should be true with the capability advertised")
	}
}

func TestManagerImplementationGatedOnCapability(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, noImplementation: true}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", inheritanceFixture); err != nil {
		t.Fatal(err)
	}
	locs, err := m.Implementation(context.Background(), path, buffer.Position{})
	if err != nil || locs != nil {
		t.Fatalf("missing capability should no-op, got %v / %v", locs, err)
	}
	if m.ImplementationSupported(path) {
		t.Fatal("ImplementationSupported should be false without the capability")
	}
	marks, err := m.InheritanceMarks(context.Background(), path)
	if err != nil || marks != nil {
		t.Fatalf("marks without capability should no-op, got %v / %v", marks, err)
	}
}

func TestManagerTypeHierarchy(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", inheritanceFixture); err != nil {
		t.Fatal(err)
	}

	items, err := m.PrepareTypeHierarchy(context.Background(), path, buffer.Position{Line: 4, Col: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "type@4:5" {
		t.Fatalf("prepare should round-trip the position, items = %+v", items)
	}

	supers, err := m.Supertypes(context.Background(), path, items[0])
	if err != nil {
		t.Fatal(err)
	}
	// The fake echoes the item's name and opaque data token, proving the
	// prepared item round-trips verbatim into the follow-up request.
	if len(supers) != 1 || supers[0].Name != `super-of-type@4:5-"ttoken"` {
		t.Fatalf("supertypes wrong: %+v", supers)
	}

	subs, err := m.Subtypes(context.Background(), path, items[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Name != "sub" {
		t.Fatalf("subtypes wrong: %+v", subs)
	}
	if !m.TypeHierarchySupported(path) {
		t.Fatal("TypeHierarchySupported should be true with the capability advertised")
	}
}

func TestManagerTypeHierarchyGatedOnCapability(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, noTypeHierarchy: true}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", inheritanceFixture); err != nil {
		t.Fatal(err)
	}
	items, err := m.PrepareTypeHierarchy(context.Background(), path, buffer.Position{})
	if err != nil || items != nil {
		t.Fatalf("missing capability should no-op, got %v / %v", items, err)
	}
	if m.TypeHierarchySupported(path) {
		t.Fatal("TypeHierarchySupported should be false without the capability")
	}
}

// TestManagerInheritanceMarks drives the whole batch against the scripted
// fixture: the interface, its method and the concrete method get marks with
// the heuristic directions; the struct (whose probe answers empty) and the
// constant (filtered by kind) get none.
func TestManagerInheritanceMarks(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", inheritanceFixture); err != nil {
		t.Fatal(err)
	}

	marks, err := m.InheritanceMarks(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	want := []lsp.InheritanceMark{
		{Line: 1, Kind: lsp.InheritanceImplemented}, // interface Shape ↓
		{Line: 2, Kind: lsp.InheritanceImplemented}, // interface method Area ↓
		{Line: 5, Kind: lsp.InheritanceImplements},  // concrete method Area ↑
	}
	if len(marks) != len(want) {
		t.Fatalf("marks = %+v, want %+v", marks, want)
	}
	for i := range want {
		if marks[i] != want[i] {
			t.Fatalf("mark %d = %+v, want %+v", i, marks[i], want[i])
		}
	}
}

// TestCollectInheritCandidatesDirections pins the kind filter and direction
// heuristic without a server round-trip.
func TestCollectInheritCandidatesDirections(t *testing.T) {
	syms := []protocol.DocumentSymbol{
		{Name: "I", Kind: protocol.SymKindInterface, SelectionRange: protocol.Range{Start: protocol.Position{Line: 0}},
			Children: []protocol.DocumentSymbol{{Name: "M", Kind: protocol.SymKindMethod, SelectionRange: protocol.Range{Start: protocol.Position{Line: 1}}}}},
		{Name: "S", Kind: protocol.SymKindStruct, SelectionRange: protocol.Range{Start: protocol.Position{Line: 3}},
			Children: []protocol.DocumentSymbol{{Name: "M", Kind: protocol.SymKindMethod, SelectionRange: protocol.Range{Start: protocol.Position{Line: 4}}}}},
		{Name: "C", Kind: protocol.SymKindClass, SelectionRange: protocol.Range{Start: protocol.Position{Line: 6}}},
		{Name: "ignored", Kind: 14, SelectionRange: protocol.Range{Start: protocol.Position{Line: 8}}},
	}
	var got []inheritCandidate
	collectInheritCandidates(syms, 0, &got)
	want := []inheritCandidate{
		{pos: protocol.Position{Line: 0}, kind: lsp.InheritanceImplemented},
		{pos: protocol.Position{Line: 1}, kind: lsp.InheritanceImplemented},
		{pos: protocol.Position{Line: 3}, kind: lsp.InheritanceImplements},
		{pos: protocol.Position{Line: 4}, kind: lsp.InheritanceImplements},
		{pos: protocol.Position{Line: 6}, kind: lsp.InheritanceImplements},
	}
	if len(got) != len(want) {
		t.Fatalf("candidates = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestInheritanceSymbolCap keeps a giant file from flooding the server: only
// the first maxInheritanceSymbols candidates are probed.
func TestInheritanceSymbolCap(t *testing.T) {
	syms := make([]protocol.DocumentSymbol, 0, maxInheritanceSymbols+50)
	for i := 0; i < maxInheritanceSymbols+50; i++ {
		syms = append(syms, protocol.DocumentSymbol{Kind: protocol.SymKindStruct, SelectionRange: protocol.Range{Start: protocol.Position{Line: i}}})
	}
	var got []inheritCandidate
	collectInheritCandidates(syms, 0, &got)
	if len(got) != maxInheritanceSymbols+50 {
		t.Fatalf("collector should not cap (the batch does), got %d", len(got))
	}
}
