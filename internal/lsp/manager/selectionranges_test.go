package manager

import (
	"context"
	"path/filepath"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestManagerSelectionRanges walks the fake's parent chain innermost-first,
// converts the UTF-16 unit offsets to rune columns, and drops the empty rung
// and the consecutive duplicate (#1912).
func TestManagerSelectionRanges(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	// "a🙂bcdefghij": the emoji is 2 UTF-16 units, so unit 3 is rune column 2,
	// unit 7 is rune 6 and unit 12 (line end) is rune 11.
	if err := m.Open(path, "go", "a🙂bcdefghij"); err != nil {
		t.Fatal(err)
	}
	ranges, err := m.SelectionRanges(context.Background(), path, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []buffer.Range{
		{Start: buffer.Position{Line: 0, Col: 2}, End: buffer.Position{Line: 0, Col: 6}},
		{Start: buffer.Position{Line: 0, Col: 0}, End: buffer.Position{Line: 0, Col: 11}},
	}
	if len(ranges) != len(want) {
		t.Fatalf("ranges = %+v, want %+v", ranges, want)
	}
	for i, w := range want {
		if ranges[i] != w {
			t.Errorf("ranges[%d] = %+v, want %+v", i, ranges[i], w)
		}
	}
}

// TestManagerSelectionRangesGated yields nothing when the server lacks the
// capability — no request, no error.
func TestManagerSelectionRangesGated(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, noSelectionRange: true}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	ranges, err := m.SelectionRanges(context.Background(), path, 0, 0)
	if err != nil || len(ranges) != 0 {
		t.Fatalf("gated request should be a no-op, got %+v, %v", ranges, err)
	}
}
