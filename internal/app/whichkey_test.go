package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
)

// pendPrefix feeds the cmd+k prefix (normalized ctrl+k on linux tables, raw
// meta on darwin — Feed normalizes) so the resolver holds a bare prefix and
// the which-key overlay is populated.
func pendPrefix(t *testing.T, m Model) Model {
	t.Helper()
	cmd, handled := m.resolveKeymap(keymap.Key{Base: "k", Mods: keymap.ModMeta})
	if !handled || cmd == nil {
		t.Fatalf("cmd+k must pend and arm the timeout")
	}
	if len(m.whichKey) == 0 {
		t.Fatalf("which-key rows must be populated while the chord pends")
	}
	return m
}

func TestWhichKeySurvivesTimeout(t *testing.T) {
	m := sized(t, 120, 40)
	m = pendPrefix(t, m)
	// The 600 ms timer fires: a bare prefix (cmd+k has no exact binding)
	// keeps the popup and the pending chord (#1482).
	out, _ := m.Update(keymapTimeoutMsg{})
	m = out.(Model)
	if len(m.whichKey) == 0 {
		t.Fatalf("which-key popup must survive the timeout for a bare prefix")
	}
	if !m.keys.Pending() {
		t.Fatalf("the pending chord must survive the timeout")
	}
}

func TestWhichKeyDismissedByClick(t *testing.T) {
	m := sized(t, 120, 40)
	m = pendPrefix(t, m)
	out, _ := m.Update(keymapTimeoutMsg{})
	m = out.(Model)
	out, _ = m.Update(tea.MouseClickMsg{X: 10, Y: 10, Button: tea.MouseLeft})
	m = out.(Model)
	if len(m.whichKey) != 0 {
		t.Fatalf("a mouse click must close the which-key popup")
	}
	if m.keys.Pending() {
		t.Fatalf("a mouse click must reset the pending chord")
	}
}

func TestWhichKeyDismissedByNonMatchingKey(t *testing.T) {
	m := sized(t, 120, 40)
	m = pendPrefix(t, m)
	out, _ := m.Update(keymapTimeoutMsg{})
	m = out.(Model)
	// Escape matches no continuation of the cmd+k family: Feed dead-ends,
	// the popup closes and the key falls through.
	if _, handled := m.resolveKeymap(keymap.Key{Base: "esc"}); handled {
		t.Fatalf("esc must fall through, not resolve")
	}
	if len(m.whichKey) != 0 {
		t.Fatalf("a non-matching key must close the which-key popup")
	}
	if m.keys.Pending() {
		t.Fatalf("a non-matching key must reset the pending chord")
	}
}
