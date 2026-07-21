package settings

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestTextFieldUmlautBackspace guards #888: backspace removes one rune, not
// one byte — the old append-only inputs corrupted multi-byte text.
func TestTextFieldUmlautBackspace(t *testing.T) {
	f := newTextField("grün")
	f.Handle(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if f.text != "grü" {
		t.Fatalf("after backspace = %q, want grü", f.text)
	}
}

// TestTextFieldCursorAndWordOps: the shared input supports cursor movement,
// mid-string insertion and word deletion.
func TestTextFieldCursorAndWordOps(t *testing.T) {
	f := newTextField("hello world")
	f.Handle(tea.KeyPressMsg{Code: tea.KeyHome})
	f.Handle(tea.KeyPressMsg{Code: 'X', Text: "X"})
	if f.text != "Xhello world" || f.cur != 1 {
		t.Fatalf("insert at home = %q cur %d", f.text, f.cur)
	}
	f.Handle(tea.KeyPressMsg{Code: tea.KeyEnd})
	f.Handle(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if f.text != "Xhello " {
		t.Fatalf("ctrl+w = %q, want the last word gone", f.text)
	}
}

// TestSchemaEditCursorInsert: the panel's schema edit inherits the cursor —
// typing mid-value no longer means backspacing through the tail.
func TestSchemaEditCursorInsert(t *testing.T) {
	m := mouseModel(t)
	m.focus = formColumn
	m.sel = 1 // editor.tab_width (Int)
	m.Update(key("enter"))
	if !m.editing {
		t.Fatal("setup: edit must open")
	}
	m.edit = newTextField("100")
	m.Update(key("home"))
	m.Update(keyRune('8'))
	if m.edit.text != "8100" || m.edit.cur != 1 {
		t.Fatalf("mid-edit insert = %q cur %d", m.edit.text, m.edit.cur)
	}
}
