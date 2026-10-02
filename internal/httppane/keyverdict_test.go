package httppane

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestKeyVerdict (#2889): the pane reports which keys it acted on, so the host
// logs only the chords nobody answered as unbound — the search prompt's word
// kill is taken, a chord neither the prompt nor the viewer uses is not.
func TestKeyVerdict(t *testing.T) {
	altBackspace := tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	unused := tea.KeyPressMsg{Code: '0', Mod: tea.ModCtrl | tea.ModAlt}
	f13 := tea.KeyPressMsg{Code: tea.KeyF13}

	m := searchViewer(t)
	m.BeginSearch()
	for _, r := range "one two" {
		m.Update(keyPress(string(r)))
	}
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
		want bool
	}{
		{"prompt word kill", altBackspace, true},
		{"prompt typing", keyPress("x"), true},
		{"prompt unused chord", unused, false},
		{"prompt function key", f13, false},
	} {
		m.Update(tc.key)
		if got := m.HandledLastKey(); got != tc.want {
			t.Errorf("%s: HandledLastKey = %v, want %v", tc.name, got, tc.want)
		}
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // close the prompt
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
		want bool
	}{
		{"viewer scroll", keyPress("j"), true},
		{"viewer re-send chord", tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, true},
		{"viewer word kill", altBackspace, false},
		{"viewer function key", f13, false},
	} {
		m.Update(tc.key)
		if got := m.HandledLastKey(); got != tc.want {
			t.Errorf("%s: HandledLastKey = %v, want %v", tc.name, got, tc.want)
		}
	}
}
