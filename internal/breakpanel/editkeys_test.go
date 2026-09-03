package breakpanel

// editkeys_test.go guards the #2002 sweep for the refinement editor: the
// condition / hit-count / log-message line hand-rolled arrows, home/end and
// backspace and now routes through ui.EditKey, so word motions, the
// word/line kills, the macOS opt/cmd chords and cmd+v all work.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/debug"
)

func editingMeta(t *testing.T, value string) Model {
	t.Helper()
	b := store(t)
	b.SetMeta("a.py", 2, debug.Meta{Condition: value})
	m := New(nil)
	m.SetSize(120, 10)
	m.SetStore(b)
	m.Update(key("down")) // onto a.py:2
	m.Update(key("c"))
	if !m.Editing() {
		t.Fatal("c must open the condition editor")
	}
	return m
}

func TestMetaEditWordAndLineKills(t *testing.T) {
	m := editingMeta(t, "alpha beta")
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if m.edit.Text != "alpha " {
		t.Fatalf("alt+backspace: %q", m.edit.Text)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper})
	if m.edit.Text != "" || m.edit.Cur != 0 {
		t.Fatalf("super+backspace: %q/%d", m.edit.Text, m.edit.Cur)
	}
}

func TestMetaEditWordMotion(t *testing.T) {
	m := editingMeta(t, "i > 3")
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	if m.edit.Cur != 4 {
		t.Fatalf("alt+left: cursor = %d, want 4", m.edit.Cur)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper})
	if m.edit.Cur != 0 {
		t.Fatalf("super+left: cursor = %d, want 0", m.edit.Cur)
	}
}

func TestMetaEditPasteAtCursor(t *testing.T) {
	m := editingMeta(t, "i > ")
	if !m.PasteText("3") {
		t.Fatal("paste must be consumed by the open editor")
	}
	if m.edit.Text != "i > 3" {
		t.Fatalf("paste: %q", m.edit.Text)
	}
	msg := run(t, m.Update(key("enter")))
	got, ok := msg.(SetMetaMsg)
	if !ok || got.Meta.Condition != "i > 3" {
		t.Fatalf("commit = %+v, want the pasted condition", msg)
	}
}

func TestMetaPasteIgnoredWhenNotEditing(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	if m.PasteText("x") {
		t.Fatal("a closed editor must not consume a paste")
	}
}

func TestMetaEditBackspaceIsRuneSafe(t *testing.T) {
	m := editingMeta(t, "größe")
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.edit.Text != "grö" {
		t.Fatalf("backspace: %q", m.edit.Text)
	}
}
