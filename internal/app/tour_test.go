package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// tourSeed builds a sized app and opens the tour via its command message.
func tourSeed(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(ShowWelcomeTourMsg{})
	return tm.(Model)
}

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Text: s, Code: rune(s[0])} }

func TestTourOpensAndPages(t *testing.T) {
	m := tourSeed(t)
	if !m.tourOpen() {
		t.Fatal("ShowWelcomeTourMsg must open the tour")
	}
	if v := ansi.Strip(m.shell.View()); !strings.Contains(v, "WELCOME TO IKE — 1/5") {
		t.Fatalf("view missing tour page 1: %q", v)
	}

	// space pages forward; h pages back; clamping at the first page.
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	m = tm.(Model)
	if v := ansi.Strip(m.shell.View()); !strings.Contains(v, "2/5") {
		t.Fatalf("space must page forward: %q", v)
	}
	tm, _ = m.Update(key("h"))
	m = tm.(Model)
	tm, _ = m.Update(key("h")) // clamp at page 1
	m = tm.(Model)
	if v := ansi.Strip(m.shell.View()); !strings.Contains(v, "1/5") {
		t.Fatal("h must page back and clamp at the first page")
	}

	// Other keys are swallowed: the tour stays open, the app does not act.
	tm, _ = m.Update(key("x"))
	m = tm.(Model)
	if !m.tourOpen() {
		t.Fatal("unrelated keys must be swallowed, not close the tour")
	}
}

func TestTourEscCloses(t *testing.T) {
	m := tourSeed(t)
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.tourOpen() || m.tour != nil {
		t.Fatal("esc must close the tour and clear its state")
	}
}

func TestTourFinishOnLastPage(t *testing.T) {
	m := tourSeed(t)
	for i := 0; i < 5; i++ { // 4 advances + 1 finish on page 5
		tm, _ := m.Update(key("l"))
		m = tm.(Model)
	}
	if m.tourOpen() || m.tour != nil {
		t.Fatal("paging past the last page must finish (close) the tour")
	}
}
