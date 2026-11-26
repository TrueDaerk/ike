package terminal

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyinert_test.go covers the per-key verdict the app's frame reuse reads:
// a key forwarded to the child with nothing to reset in the pane is inert,
// every key the pane acts on itself is not.

func inertPipe(t *testing.T) *Model {
	t.Helper()
	m := NewPipe("inert-test", 40, 6, func(tea.Msg) {})
	t.Cleanup(m.Close)
	return &m
}

func TestKeyInertForPlainForwardedKeys(t *testing.T) {
	t.Parallel()
	m := inertPipe(t)
	keys := []tea.KeyPressMsg{
		{Text: "a", Code: 'a'},
		{Code: tea.KeyEnter},
		{Code: tea.KeyBackspace},
		{Code: tea.KeyLeft},
		{Code: 'c', Mod: tea.ModCtrl},
		{Code: tea.KeyLeft, Mod: tea.ModAlt},
	}
	for _, k := range keys {
		m.Update(k)
		if !m.KeyInert() {
			t.Fatalf("%q forwarded at the live view must be inert", k.String())
		}
	}
}

func TestKeyInertFalseWhenThePaneActs(t *testing.T) {
	t.Parallel()
	m := inertPipe(t)
	plain := tea.KeyPressMsg{Text: "a", Code: 'a'}

	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModShift})
	if m.KeyInert() {
		t.Fatal("a paging key is consumed by the pane")
	}

	m.scroll = 3
	m.Update(plain)
	if m.KeyInert() || m.scroll != 0 {
		t.Fatal("a key that snaps the view back to live changes the pane")
	}
	m.Update(plain)
	if !m.KeyInert() {
		t.Fatal("the next key at the live view is inert again")
	}

	m.selOn = true
	m.Update(plain)
	if m.KeyInert() || m.HasSelection() {
		t.Fatal("a key that drops the selection changes the pane")
	}

	if !m.StartCopyMode() {
		t.Fatal("test setup: copy mode opens")
	}
	m.Update(tea.KeyPressMsg{Text: "k", Code: 'k'})
	if m.KeyInert() {
		t.Fatal("a copy-mode key is consumed by the pane")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.KeyInert() {
		t.Fatal("leaving copy mode changes the pane")
	}

	m.StartSearch()
	m.Update(plain)
	if m.KeyInert() {
		t.Fatal("a key typed into the scrollback search is consumed by the pane")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	m.comp.open = true
	m.Update(plain)
	if m.KeyInert() {
		t.Fatal("a key typed with the completion popup open is not inert")
	}
}

func TestKeyInertFalseForFinishedSession(t *testing.T) {
	t.Parallel()
	m := inertPipe(t)
	m.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	if !m.KeyInert() {
		t.Fatal("test setup: a live session forwards the key")
	}
	m.FinishPipe(0, true)
	m.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	if m.KeyInert() {
		t.Fatal("a finished session has no child to forward to")
	}
}
