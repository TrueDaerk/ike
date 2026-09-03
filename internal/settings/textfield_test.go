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
	if f.Text != "grü" {
		t.Fatalf("after backspace = %q, want grü", f.Text)
	}
}

// TestTextFieldCursorAndWordOps: the shared input supports cursor movement,
// mid-string insertion and word deletion.
func TestTextFieldCursorAndWordOps(t *testing.T) {
	f := newTextField("hello world")
	f.Handle(tea.KeyPressMsg{Code: tea.KeyHome})
	f.Handle(tea.KeyPressMsg{Code: 'X', Text: "X"})
	if f.Text != "Xhello world" || f.Cur != 1 {
		t.Fatalf("insert at home = %q cur %d", f.Text, f.Cur)
	}
	f.Handle(tea.KeyPressMsg{Code: tea.KeyEnd})
	f.Handle(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if f.Text != "Xhello " {
		t.Fatalf("ctrl+w = %q, want the last word gone", f.Text)
	}
}

// TestSchemaEditCursorInsert: the panel's schema edit inherits the cursor —
// typing mid-value no longer means backspacing through the tail.
func TestSchemaEditCursorInsert(t *testing.T) {
	m := mouseModel(t)
	m.focus = formColumn
	m.sel = 1 // editor.tab_width (Int)
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("setup: editor = %T, want *intEditor", m.editor)
	}
	ed.tf = newTextField("100")
	m.Update(key("home"))
	m.Update(keyRune('8'))
	if ed.tf.Text != "8100" || ed.tf.Cur != 1 {
		t.Fatalf("mid-edit insert = %q cur %d", ed.tf.Text, ed.tf.Cur)
	}
}
