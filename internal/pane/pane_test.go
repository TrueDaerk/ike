package pane

import (
	"testing"

	"ike/internal/host"
)

func newReg() *Registry { return NewRegistry(host.MapConfig{}) }

// TestKeyAllocation verifies the explorer keeps the stable key and editors get
// monotonic, never-reused keys.
func TestKeyAllocation(t *testing.T) {
	r := newReg()
	if k := r.AddExplorer(); k != ExplorerKey {
		t.Fatalf("explorer key = %q, want %q", k, ExplorerKey)
	}
	if k := r.AddEditor(); k != "editor" {
		t.Fatalf("first editor key = %q, want editor", k)
	}
	if k := r.AddEditor(); k != "editor:2" {
		t.Fatalf("second editor key = %q, want editor:2", k)
	}
	if k := r.AddEditor(); k != "editor:3" {
		t.Fatalf("third editor key = %q, want editor:3", k)
	}
}

// TestAddExplorerSingleton confirms a second AddExplorer does not duplicate.
func TestAddExplorerSingleton(t *testing.T) {
	r := newReg()
	r.AddExplorer()
	r.AddExplorer()
	if r.Len() != 1 {
		t.Fatalf("explorer should be a singleton, len=%d", r.Len())
	}
}

// TestKindsAndContext verifies instances advertise their kind and context id.
func TestKindsAndContext(t *testing.T) {
	r := newReg()
	r.AddExplorer()
	ek := r.AddEditor()
	if r.Get(ExplorerKey).Kind() != KindExplorer || r.Get(ExplorerKey).ContextID() != "explorer" {
		t.Fatal("explorer instance has wrong kind/context")
	}
	if r.Get(ek).Kind() != KindEditor || r.Get(ek).ContextID() != "editor" {
		t.Fatal("editor instance has wrong kind/context")
	}
}

// TestCloseRemovesAndClearsFocus checks Close drops the instance and clears focus
// when the closed key was focused.
func TestCloseRemovesAndClearsFocus(t *testing.T) {
	r := newReg()
	r.AddExplorer()
	ek := r.AddEditor()
	r.SetFocused(ek)
	if r.Focused() != ek {
		t.Fatalf("focus = %q, want %q", r.Focused(), ek)
	}
	r.Close(ek)
	if r.Has(ek) {
		t.Fatal("closed key should be gone")
	}
	if r.Focused() != "" {
		t.Fatalf("closing the focused key should clear focus, got %q", r.Focused())
	}
}

// TestSetFocusedMarksInstances verifies exactly the focused instance is marked.
func TestSetFocusedMarksInstances(t *testing.T) {
	r := newReg()
	r.AddExplorer()
	ek := r.AddEditor()
	r.SetFocused(ek)
	if r.FocusedInstance() != r.Get(ek) {
		t.Fatal("FocusedInstance should resolve to the editor")
	}
	// A bogus key clears focus without panicking.
	r.SetFocused("nope")
	if r.Focused() != "" {
		t.Fatalf("focusing an absent key should clear focus, got %q", r.Focused())
	}
}

// TestAddEditorKeyAdvancesCounter ensures restoring an explicit key bumps the
// minting counter so a later AddEditor never collides.
func TestAddEditorKeyAdvancesCounter(t *testing.T) {
	r := newReg()
	r.AddExplorer()
	r.AddEditorKey("editor:5")
	if k := r.AddEditor(); k == "editor:5" {
		t.Fatal("AddEditor reused a restored key")
	} else if k != "editor:6" {
		t.Fatalf("next key = %q, want editor:6", k)
	}
}

// TestKeysInsertionOrder verifies Keys reflects insertion order.
func TestKeysInsertionOrder(t *testing.T) {
	r := newReg()
	r.AddExplorer()
	a := r.AddEditor()
	b := r.AddEditor()
	keys := r.Keys()
	if len(keys) != 3 || keys[0] != ExplorerKey || keys[1] != a || keys[2] != b {
		t.Fatalf("keys order = %v", keys)
	}
}
