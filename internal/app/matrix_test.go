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
	// 0082 sheet 11 (#18): f4 is the delivered primary; cmd+b stays a
	// secondary row for terminals that deliver Cmd.
	if r := byCmd["lsp.definition"]; r.Primary != "f4" || r.Class != keymap.Delivered || r.Status() != "live" {
		t.Errorf("lsp.definition = %+v", r)
	}
	// 0082 sheet 13 (#18): shift+f6 renames the symbol in the editor context.
	if r := byCmd["lsp.rename"]; r.Primary != "shift+f6" || r.Status() != "live" {
		t.Errorf("lsp.rename = %+v", r)
	}
	// The VCS ids went live with 0320: fragile Cmd primaries, leader
	// mnemonics as the delivered path.
	if r := byCmd["vcs.commit"]; r.Fallback != "space v c" || !strings.Contains(r.Status(), "live") {
		t.Errorf("vcs.commit = %+v", r)
	}
	if r := byCmd["vcs.updateProject"]; r.Fallback != "space v u" || !strings.Contains(r.Status(), "live") {
		t.Errorf("vcs.updateProject = %+v", r)
	}
	// The blocked ledger emptied with 0320 (#466): the blocked-status label
	// machinery is exercised through a stubbed entry.
	remove := keymap.StubBlockedForTest("vcs.revertFile", "unit-test dependency")
	blockedRows := keymap.StatusMatrix(func(string) bool { return true })
	remove()
	for _, r := range blockedRows {
		if r.Command == "vcs.revertFile" && !strings.Contains(r.Status(), "blocked:") {
			t.Errorf("stubbed blocked row = %+v", r)
		}
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
