package pane

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestInstanceHandledLastKey (#2889): the instance answers the host's deferred
// unbound verdict from the component the key reached — a wired tool pane
// reports its own verdict, a pane kind that cannot report says false so the
// held-back chord keeps being logged.
func TestInstanceHandledLastKey(t *testing.T) {
	r := newReg()
	unused := tea.KeyPressMsg{Code: '0', Mod: tea.ModCtrl | tea.ModAlt}
	halfPage := tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}

	exp := r.Get(r.AddExplorer())
	exp.Update(halfPage)
	if !exp.HandledLastKey() {
		t.Error("the explorer answers ctrl+d (half page) itself")
	}
	exp.Update(unused)
	if exp.HandledLastKey() {
		t.Error("the explorer has no use for ctrl+alt+0")
	}

	hp := r.Get(r.AddHTTP())
	hp.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if !hp.HandledLastKey() {
		t.Error("the HTTP viewer answers j (scroll) itself")
	}
	hp.Update(unused)
	if hp.HandledLastKey() {
		t.Error("the HTTP viewer has no use for ctrl+alt+0")
	}

	// The problems panel does not report (yet): the chord stays recorded.
	pp := r.Get(r.AddProblems())
	pp.Update(halfPage)
	if pp.HandledLastKey() {
		t.Error("a pane kind without a KeyReporter must report false")
	}
}
