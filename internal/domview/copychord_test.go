package domview

// copychord_test.go covers the copy-consistency audit of #2062: the selector
// prompt owns the keyboard, but the highlighted node it copies stays visible
// behind it, so the modified copy chord has to survive the capture.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func copyChordKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper} }

// TestCopyChordAliasesC: at rest the chord does what "c" does.
func TestCopyChordAliasesC(t *testing.T) {
	m := loaded(t)
	press(&m, "j") // <ul.list>
	cmd := m.Update(copyChordKey())
	if cmd == nil {
		t.Fatal("cmd+c produced no command")
	}
	chord := cmd().(CopyMsg)
	if chord.What != "CSS selector" {
		t.Fatalf("copy = %+v", chord)
	}
	bare := press(&m, "c")().(CopyMsg)
	if chord.Text != bare.Text {
		t.Errorf("cmd+c copied %q, c copied %q", chord.Text, bare.Text)
	}
}

// TestCopyChordWhileSelectorPromptOpen: the prompt consumes every other key,
// but not the copy chord — and typing around it is unaffected, "c" included.
func TestCopyChordWhileSelectorPromptOpen(t *testing.T) {
	m := loaded(t)
	press(&m, "j") // <ul.list>
	want := press(&m, "c")().(CopyMsg).Text

	press(&m, "/")
	if !m.selEditing {
		t.Fatal("/ must open the selector prompt")
	}
	cmd := m.Update(copyChordKey())
	if cmd == nil {
		t.Fatal("cmd+c with the prompt open must still copy")
	}
	if got := cmd().(CopyMsg); got.Text != want || got.What != "CSS selector" {
		t.Errorf("copy = %+v, want the selector %q", got, want)
	}
	if !m.selEditing {
		t.Error("copying must leave the prompt open")
	}

	// "c" is selector input, not the copy key, while the prompt is open.
	press(&m, "l", "i", "c")
	if got := m.Selector(); got != "lic" {
		t.Errorf("selector = %q, want %q", got, "lic")
	}
}

// TestCtrlCStaysGlobalInTheTree: the tree has no text selection to protect,
// so ctrl+c is left to the shell's quit binding rather than aliased to "c".
func TestCtrlCStaysGlobalInTheTree(t *testing.T) {
	m := loaded(t)
	press(&m, "j")
	if cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd != nil {
		t.Errorf("ctrl+c must stay inert in the pane, got %+v", cmd())
	}
}
