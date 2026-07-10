package app

import (
	"strings"
	"testing"

	"ike/internal/keymap"
)

// TestBindingMatrixShape spot-checks the ledger columns. The full-registry
// final gate lives in cmd/ike (the shipped plugin set).
func TestBindingMatrixShape(t *testing.T) {
	rows := keymap.StatusMatrix(nil)
	byCmd := map[string]keymap.MatrixRow{}
	for _, r := range rows {
		byCmd[r.Command] = r
	}
	// The advertised JetBrains primary stays (muscle memory); the delivered
	// twin resolves the row as the fallback.
	if r := byCmd["editor.write"]; r.Primary != "cmd+s" || r.Fallback != "ctrl+s" || r.Status() != "live via ctrl+s" {
		t.Errorf("editor.write = %+v", r)
	}
	if r := byCmd["file.rename"]; r.Class != keymap.Delivered || r.Status() != "live" {
		t.Errorf("file.rename = %+v", r)
	}
	if r := byCmd["lsp.definition"]; r.Fallback == "" || !strings.Contains(r.Status(), "live via") {
		t.Errorf("lsp.definition = %+v", r)
	}
	if r := byCmd["vcs.commit"]; !strings.Contains(r.Status(), "blocked:") {
		t.Errorf("vcs.commit = %+v", r)
	}
	if r := byCmd["editor.copy"]; r.Fallback != "vim y" {
		t.Errorf("editor.copy = %+v", r)
	}
	// A data-only view with a missing command surfaces as unresolved.
	broken := keymap.StatusMatrix(func(string) bool { return false })
	found := false
	for _, r := range broken {
		if !r.Resolved() && strings.Contains(r.Status(), "not registered") {
			found = true
			break
		}
	}
	if !found {
		t.Error("a missing command must surface as unresolved")
	}
}
