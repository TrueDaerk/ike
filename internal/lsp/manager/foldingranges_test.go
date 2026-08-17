package manager

import (
	"context"
	"path/filepath"
	"testing"

	"ike/internal/highlight"
	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestManagerFoldingRanges normalises the fake's messy reply (#1912): the
// degenerate ranges vanish, the over-long range clamps to the last line, the
// duplicate header keeps only the larger range, and the result is pre-ordered
// by header line.
func TestManagerFoldingRanges(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	// Five lines (0..4), so the fake's endLine 99 clamps to 4 and its
	// startLine 5 falls out of bounds.
	if err := m.Open(path, "go", "l0\nl1\nl2\nl3\nl4"); err != nil {
		t.Fatal(err)
	}
	folds, err := m.FoldingRanges(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	want := []highlight.Fold{
		{HeaderLine: 0, EndLine: 4, Kind: "imports"},
		{HeaderLine: 2, EndLine: 3},
	}
	if len(folds) != len(want) {
		t.Fatalf("folds = %+v, want %+v", folds, want)
	}
	for i, w := range want {
		if folds[i] != w {
			t.Errorf("folds[%d] = %+v, want %+v", i, folds[i], w)
		}
	}
}

// TestManagerFoldingRangesGated yields nothing when the server lacks the
// capability — no request, no error.
func TestManagerFoldingRangesGated(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, noFoldingRange: true}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	folds, err := m.FoldingRanges(context.Background(), path)
	if err != nil || len(folds) != 0 {
		t.Fatalf("gated request should be a no-op, got %+v, %v", folds, err)
	}
}
