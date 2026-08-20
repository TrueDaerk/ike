package debugpanel

// editkeys_test.go guards the #2002 sweep for the variables panel's inline
// value editor: it hand-rolled arrows, home/end and backspace and now routes
// through ui.EditKey, so word motions, the word/line kills, the macOS
// opt/cmd chords and cmd+v all work.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
)

func editingPanel(t *testing.T, value string) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(80, 10)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetChildren(100, []dap.Variable{{Name: "x", Value: value, Type: "string"}})
	m.SetEditable(true)
	m.col = colVars
	m.varSel = 1
	m.Update(key("e"))
	if !m.Editing() {
		t.Fatal("'e' must open the inline editor")
	}
	return &m
}

func TestEditWordAndLineKills(t *testing.T) {
	m := editingPanel(t, "alpha beta")
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if string(m.editBuf) != "alpha " {
		t.Fatalf("alt+backspace: %q", string(m.editBuf))
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper})
	if string(m.editBuf) != "" || m.editCur != 0 {
		t.Fatalf("super+backspace: %q/%d", string(m.editBuf), m.editCur)
	}
}

func TestEditWordMotion(t *testing.T) {
	m := editingPanel(t, "alpha beta")
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	if m.editCur != 6 {
		t.Fatalf("alt+left: cursor = %d, want 6", m.editCur)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper})
	if m.editCur != 0 {
		t.Fatalf("super+left: cursor = %d, want 0", m.editCur)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper})
	if m.editCur != 10 {
		t.Fatalf("super+right: cursor = %d, want 10", m.editCur)
	}
}

func TestEditDeleteForward(t *testing.T) {
	m := editingPanel(t, "abc")
	m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if string(m.editBuf) != "bc" {
		t.Fatalf("delete: %q", string(m.editBuf))
	}
}

func TestEditPasteAtCursor(t *testing.T) {
	m := editingPanel(t, "ac")
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if !m.PasteText("b") {
		t.Fatal("paste must be consumed by the open editor")
	}
	if string(m.editBuf) != "abc" || m.editCur != 2 {
		t.Fatalf("paste: %q/%d", string(m.editBuf), m.editCur)
	}
}

// A closed editor lets the paste fall through instead of swallowing it.
func TestPasteIgnoredWhenNotEditing(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	if m.PasteText("x") {
		t.Fatal("a closed editor must not consume a paste")
	}
}

// Rune-safe editing: multi-byte text survives a backspace intact.
func TestEditBackspaceIsRuneSafe(t *testing.T) {
	m := editingPanel(t, "größe")
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if string(m.editBuf) != "grö" {
		t.Fatalf("backspace: %q", string(m.editBuf))
	}
}
