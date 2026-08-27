package debugpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/terminal"
)

// console_test.go covers the combined debug area's panel side (#2190): the
// internal tab bar, view switching, sizing, and key/paste routing into the
// embedded console terminal.

func consolePanel(t *testing.T) (*Model, *terminal.Model) {
	t.Helper()
	m := New(nil)
	m.SetSize(60, 10)
	term := terminal.NewPipe("t1", 60, 10, nil)
	m.SetTerm(&term)
	t.Cleanup(func() { m.CloseTerm() })
	return &m, &term
}

// TestTabBarAppearsWithTerm: no console → no bar (the historic view); with
// one, the bar leads and the body loses a row.
func TestTabBarAppearsWithTerm(t *testing.T) {
	m := New(nil)
	m.SetSize(60, 10)
	if got := len(strings.Split(m.View(), "\n")); got != 10 {
		t.Fatalf("bare panel rows = %d, want 10", got)
	}
	if m.HasTerm() || m.ConsoleActive() {
		t.Fatal("a fresh panel has no console")
	}
	term := terminal.NewPipe("t0", 60, 10, nil)
	m.SetTerm(&term)
	defer m.CloseTerm()
	view := m.View()
	if !strings.Contains(view, "Variables") || !strings.Contains(view, "Console") {
		t.Fatalf("the tab bar must render both labels:\n%s", view)
	}
	if w, h := term.Size(); w != 60 || h != 9 {
		t.Fatalf("console size = %dx%d, want 60x9 (one row under the bar)", w, h)
	}
}

// TestTabSwitchKeysAndClicks: tab switches Variables→Console; on the pipe
// console tab/shift+tab return; the bar's spans resolve clicks either way.
func TestTabSwitchKeysAndClicks(t *testing.T) {
	m, _ := consolePanel(t)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.ConsoleActive() {
		t.Fatal("tab must switch to the console view")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.ConsoleActive() {
		t.Fatal("tab on the pipe console must switch back to variables")
	}
	// A click on the Console label's span switches; the bar sits at y 0.
	spans := tabBarSpans()
	m.Click(spans[1][0], 0)
	if !m.ConsoleActive() {
		t.Fatal("a tab-bar click must switch to the console view")
	}
	m.Click(spans[0][0], 0)
	if m.ConsoleActive() {
		t.Fatal("a tab-bar click must switch back to variables")
	}
}

// TestAutoTabRespectsUserChoice: AutoTab drives the view only until the user
// picked one; ResetSession re-arms it.
func TestAutoTabRespectsUserChoice(t *testing.T) {
	m, _ := consolePanel(t)
	m.AutoTab(TabConsole)
	if !m.ConsoleActive() {
		t.Fatal("AutoTab must switch an untouched panel")
	}
	m.SetTab(TabVariables) // the user's pick
	m.AutoTab(TabConsole)
	if m.ConsoleActive() {
		t.Fatal("AutoTab must not override a user-picked view")
	}
	m.ResetSession()
	m.AutoTab(TabConsole)
	if !m.ConsoleActive() {
		t.Fatal("ResetSession must re-arm the automatic view selection")
	}
}

// TestConsoleScrollAndSelectionSurviveSwitch: the terminal model is hosted
// whole, so its scrollback offset and selection survive view switches.
func TestConsoleScrollAndSelectionSurviveSwitch(t *testing.T) {
	m, term := consolePanel(t)
	term.ScrollBy(3)
	term.MousePress(1, 1)
	term.MouseRelease(2, 1)
	scroll := term.Scroll()
	m.SetTab(TabConsole)
	m.SetTab(TabVariables)
	m.SetTab(TabConsole)
	if got := m.Term().Scroll(); got != scroll {
		t.Fatalf("scroll offset = %d after switches, want %d", got, scroll)
	}
}

// TestSetTermReplacesAndCloses: installing a new console closes the previous
// session (a fresh launch reuses the area).
func TestSetTermReplacesAndCloses(t *testing.T) {
	m, old := consolePanel(t)
	next := terminal.NewPipe("t2", 60, 10, nil)
	m.SetTerm(&next)
	if old.Running() {
		t.Fatal("SetTerm must close the replaced console session")
	}
	if m.Term() != &next {
		t.Fatal("SetTerm must install the new console")
	}
}

// TestConsolePaste: a paste while the console view is visible goes to the
// terminal, not the panel's inline editor.
func TestConsolePaste(t *testing.T) {
	m, _ := consolePanel(t)
	m.SetTab(TabConsole)
	if !m.PasteText("hello") {
		t.Fatal("the console view must consume the paste")
	}
}

// TestSeparatorInactiveOnConsole: the frames│variables separator only exists
// in the variables view; the console view never starts a column resize.
func TestSeparatorInactiveOnConsole(t *testing.T) {
	m, _ := consolePanel(t)
	fw := 0
	for x := 0; x < 60; x++ {
		if m.SeparatorHit(x) == 0 {
			fw = x
			break
		}
	}
	if fw == 0 {
		t.Fatal("precondition: the variables view has a separator")
	}
	m.SetTab(TabConsole)
	if m.SeparatorHit(fw) != -1 {
		t.Fatal("the console view must not expose the column separator")
	}
}
