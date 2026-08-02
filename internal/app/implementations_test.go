package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// TestImplementationsMsgRouting covers #1452: a single target navigates, more
// open the locked picker with the direction-specific placeholder; an empty
// message (never sent by the bridge) is inert.
func TestImplementationsMsgRouting(t *testing.T) {
	m := sized(t, 100, 40)

	// Defensive: an empty message neither toasts nor opens anything.
	out, _ := m.Update(ilsp.ImplementationsMsg{})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("empty message must not open the palette")
	}

	// A single implementation navigates straight there.
	dir := t.TempDir()
	file := filepath.Join(dir, "impl.go")
	if err := os.WriteFile(file, []byte("package impl\ntype T struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = m.Update(ilsp.ImplementationsMsg{Refs: []ilsp.Reference{{Path: file, Line: 1, Col: 5}}})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("single result must navigate, not list")
	}
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if ed.Path() != file {
		t.Fatalf("single result should open the target, got %q", ed.Path())
	}

	// Multiple implementations open the locked list with the wording.
	out, _ = m.Update(ilsp.ImplementationsMsg{Refs: sampleRefs()})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("multiple results should open the picker")
	}
	if ph := m.refs.Placeholder(); !strings.Contains(ph, "Implementations") {
		t.Fatalf("placeholder = %q, want implementations wording", ph)
	}
	if items := m.refs.Results("", palette.Context{}); len(items) != 3 {
		t.Fatalf("picker rows = %d, want 3", len(items))
	}

	// The Super flag swaps the wording.
	out, _ = m.Update(ilsp.ImplementationsMsg{Refs: sampleRefs(), Super: true})
	m = out.(Model)
	if ph := m.refs.Placeholder(); !strings.Contains(ph, "Super declarations") {
		t.Fatalf("placeholder = %q, want super wording", ph)
	}
}

// TestEditorContextMenuHasInheritanceEntries pins the right-click entries.
func TestEditorContextMenuHasInheritanceEntries(t *testing.T) {
	var super, impls bool
	for _, it := range editorContextItems(false) {
		switch it.Command {
		case "lsp.goToSuper":
			super = true
		case "lsp.implementations":
			impls = true
		}
	}
	if !super || !impls {
		t.Fatalf("context menu missing inheritance entries (super=%v, implementations=%v)", super, impls)
	}
}
