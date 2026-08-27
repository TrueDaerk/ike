package pane

import (
	"testing"

	"ike/internal/debugpanel"
	"ike/internal/host"
	"ike/internal/terminal"
)

func newReg() *Registry { return NewRegistry(host.MapConfig{}, nil) }

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

// TestDebugConsoleEmbedding (#2190): the debug area embeds the debuggee's
// console terminal; while the console view is active the pane's input reaches
// it (ActiveTerminal, terminal keymap context), and closing the pane releases
// the session.
func TestDebugConsoleEmbedding(t *testing.T) {
	r := newReg()
	key := r.AddDebug()
	inst := r.Get(key)
	if inst.ActiveTerminal() != nil {
		t.Fatal("a console-less debug pane must expose no active terminal")
	}
	term := terminal.NewPipe(r.MintTerminalKey(), 40, 6, nil)
	inst.Debug().SetTerm(&term)
	if inst.ActiveTerminal() != nil || inst.ContextID() != ctxDebug {
		t.Fatal("the variables view must keep the debug context and no active terminal")
	}
	inst.Debug().SetTab(debugpanel.TabConsole)
	if inst.ActiveTerminal() != &term {
		t.Fatal("the console view must expose the embedded terminal as the active one")
	}
	if inst.ContextID() != ctxTerminal {
		t.Fatalf("console view context = %q, want %q", inst.ContextID(), ctxTerminal)
	}
	r.Close(key)
	if term.Running() {
		t.Fatal("closing the debug pane must end the console session")
	}
}
