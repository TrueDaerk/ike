package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestViewDoesNotRequestEventTypes locks in the fix for the F8 double-step
// (#622): enabling KeyboardEnhancements.ReportEventTypes makes a full Kitty
// terminal (Ghostty) emit a release after every key, and ultraviolet's
// parseKittyKeyboardExt mis-parses the release of a CSI-`~` function key as a
// second KeyPressEvent — so one F8 tap stepped the debugger twice. We ignore
// repeat/release anyway, so the flag must stay off. Basic disambiguation (the
// default) is all the keymap layer needs.
func TestViewDoesNotRequestEventTypes(t *testing.T) {
	m := newSized()
	v := m.View()
	if v.KeyboardEnhancements.ReportEventTypes {
		t.Fatal("View must not enable ReportEventTypes: it doubles CSI-~ function keys on Kitty terminals (#622)")
	}
}

// TestKeyReleaseIgnored is a guard for the dispatch contract the fix relies on:
// only KeyPressMsg drives Update, so even if a stray release arrives it is a
// no-op rather than a second action.
func TestKeyReleaseIgnored(t *testing.T) {
	m := newSized()
	before := m.render()
	tm, cmd := m.Update(tea.KeyReleaseMsg{Code: 'j'})
	if cmd != nil {
		t.Fatal("a key release must not produce a command")
	}
	if got := tm.(Model).render(); got != before {
		t.Fatal("a key release must not change the view")
	}
}
