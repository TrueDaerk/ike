package settings

// filteredit_test.go guards the #2002 sweep for the settings filter lines:
// the panel-wide filter, the keymap page's filter and the enum editor's
// type-to-filter were append-only strings (the panel's even byte-sliced its
// backspace, corrupting multi-byte text). All three now edit through
// ui.EditKey.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func editKeys(m *Model, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		m.Update(k)
	}
}

func typeFilter(m *Model, s string) {
	for _, r := range s {
		m.Update(keyRune(r))
	}
}

var (
	altBack  = tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	cmdBack  = tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}
	altLeft  = tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}
	plainLft = tea.KeyPressMsg{Code: tea.KeyLeft}
	homeKey  = tea.KeyPressMsg{Code: tea.KeyHome}
)

func TestPanelFilterSharedEditing(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	typeFilter(m, "python venv")
	editKeys(m, altBack)
	if m.filter != "python " {
		t.Fatalf("alt+backspace: %q", m.filter)
	}
	editKeys(m, altLeft)
	if m.filterCur != 0 {
		t.Fatalf("alt+left: cursor = %d, want 0", m.filterCur)
	}
	typeFilter(m, "X")
	if m.filter != "Xpython " {
		t.Fatalf("insert at line start: %q", m.filter)
	}
	editKeys(m, tea.KeyPressMsg{Code: tea.KeyEnd}, cmdBack)
	if m.filter != "" || m.filterCur != 0 {
		t.Fatalf("cmd+backspace: %q/%d", m.filter, m.filterCur)
	}
}

// The old byte-sliced backspace cut a multi-byte rune in half.
func TestPanelFilterBackspaceIsRuneSafe(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	typeFilter(m, "größe")
	editKeys(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	editKeys(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.filter != "grö" {
		t.Fatalf("rune-safe backspace: %q", m.filter)
	}
	if !strings.Contains(m.renderFilter(), "grö") {
		t.Fatalf("filter head lost its text: %q", m.renderFilter())
	}
}

func TestPanelFilterPasteAtCursor(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	typeFilter(m, "ab")
	editKeys(m, plainLft)
	if !m.Paste("XY") {
		t.Fatal("paste must be consumed by the open filter")
	}
	if m.filter != "aXYb" || m.filterCur != 3 {
		t.Fatalf("paste: %q/%d", m.filter, m.filterCur)
	}
}

// Esc still clears the filter — and the cursor with it.
func TestPanelFilterEscResetsCursor(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	typeFilter(m, "abc")
	editKeys(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.filter != "" || m.filterCur != 0 {
		t.Fatalf("esc: %q/%d", m.filter, m.filterCur)
	}
}

func TestKeymapPageFilterSharedEditing(t *testing.T) {
	k, _ := keymapPage(t)
	k.Update(key("/"))
	for _, r := range "editor move" {
		k.Update(keyRune(r))
	}
	k.Update(altBack)
	if k.filter != "editor " {
		t.Fatalf("alt+backspace: %q", k.filter)
	}
	k.Update(homeKey)
	k.Update(keyRune('X'))
	if k.filter != "Xeditor " {
		t.Fatalf("insert at line start: %q", k.filter)
	}
	if !k.Paste("Y") {
		t.Fatal("paste must reach the open keymap filter")
	}
	if k.filter != "XYeditor " {
		t.Fatalf("paste at cursor: %q", k.filter)
	}
	k.Update(cmdBack)
	if k.filter != "editor " {
		t.Fatalf("cmd+backspace kills to line start: %q", k.filter)
	}
}
