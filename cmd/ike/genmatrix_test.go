package main

import (
	"os"
	"testing"

	"ike/internal/keymap"
	"ike/internal/registry"
)

// TestGenerateMatrixMarkdown emits the ledger for the wiki when
// IKE_GEN_MATRIX names an output file; a plain test run skips it.
func TestGenerateMatrixMarkdown(t *testing.T) {
	out := os.Getenv("IKE_GEN_MATRIX")
	if out == "" {
		t.Skip("set IKE_GEN_MATRIX=<file> to emit the ledger")
	}
	exists := func(id string) bool { _, ok := registry.Global().Command(id); return ok }
	md := keymap.MatrixMarkdown(keymap.StatusMatrix(exists))
	if err := os.WriteFile(out, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}
