package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// playkeys_test.go covers the playground's keyboard usability round (#2237):
// the esc-esc palette chord out of the query line and the result buffer, the
// code-action chord's honest answer, and the cheatsheet context that finally
// says which keys apply while the mode owns the keyboard.

// escKey is the key press the double-esc detector counts.
func escKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// atClock pins the model's clock so the esc-esc window is deterministic.
func atClock(m Model, t time.Time) Model {
	m.nowFn = func() time.Time { return t }
	return m
}

func TestPlaygroundEscEscOpensPalette(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":1}`))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m = atClock(m, now)

	out, _ := m.Update(escKey())
	m = out.(Model)
	if m.playOpen() {
		t.Fatal("the first esc must still leave the playground")
	}
	if m.palette.IsOpen() {
		t.Fatal("a single esc must not open the palette")
	}

	m = atClock(m, now.Add(100*time.Millisecond))
	out, _ = m.Update(escKey())
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("esc esc out of the playground query line must open the palette (#2237)")
	}
}

func TestPlaygroundEscEscFromResultBuffer(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":1}`))
	m.play.setBufFocus(true)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m = atClock(m, now)

	out, _ := m.Update(escKey())
	m = out.(Model)
	if m.playOpen() {
		t.Fatal("esc from the resting result buffer must leave the playground")
	}
	m = atClock(m, now.Add(100*time.Millisecond))
	out, _ = m.Update(escKey())
	if !out.(Model).palette.IsOpen() {
		t.Fatal("esc esc out of the result buffer must open the palette (#2237)")
	}
}

func TestPlaygroundSingleEscKeepsMeaningOutsideTheWindow(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":1}`))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m = atClock(m, now)

	out, _ := m.Update(escKey())
	m = out.(Model)
	if m.playOpen() {
		t.Fatal("esc must leave the playground immediately, not after a chord timeout")
	}
	// A second esc well past escEscTimeout is a fresh first esc, not the
	// other half of a double tap.
	m = atClock(m, now.Add(2*time.Second))
	out, _ = m.Update(escKey())
	if out.(Model).palette.IsOpen() {
		t.Fatal("an esc beyond the chord window must not open the palette")
	}
}

// TestPlaygroundEscDismissesCompletionWithoutArming: the popup's own esc is a
// dismissal, not the first half of the palette chord — the same rule the
// editor's insert mode follows.
func TestPlaygroundEscDismissesCompletionWithoutArming(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":1}`))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m = atClock(m, now)
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.play.comp == nil {
		t.Skip("ctrl+space did not open the completion popup in this fixture")
	}
	out, _ = m.Update(escKey())
	m = out.(Model)
	if !m.playOpen() {
		t.Fatal("esc under an open popup dismisses the popup, not the mode")
	}
	m = atClock(m, now.Add(50*time.Millisecond))
	out, _ = m.Update(escKey())
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("the popup's esc must not arm the palette chord")
	}
	if m.playOpen() {
		t.Fatal("the esc after the dismissal leaves the mode")
	}
}

func TestPlaygroundCodeActionReportsUnavailable(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":1}`))
	if !m.playCodeActionChord(altEnter()) {
		t.Fatal("alt+enter must resolve as the lsp.codeAction chord")
	}
	before := m.play.program
	out, _ := m.Update(altEnter())
	m = out.(Model)
	if !m.playOpen() {
		t.Fatal("the code-action chord must not close the playground")
	}
	if !strings.Contains(m.play.status, "no code actions") {
		t.Fatalf("code actions in the playground must say so, got status %q (#2237)", m.play.status)
	}
	if !m.play.statusWarn {
		t.Fatal("a 'not available here' line is not a success message")
	}
	if got := m.play.program; got != before {
		t.Fatalf("the chord must not type into the query line: %q -> %q", before, got)
	}
}

func TestPlaygroundCodeActionReportsUnavailableInResultBuffer(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":1}`))
	m.play.setBufFocus(true)
	out, _ := m.Update(altEnter())
	m = out.(Model)
	if !strings.Contains(m.play.status, "no code actions") {
		t.Fatalf("the result buffer answers the chord too, got %q", m.play.status)
	}
}

// altEnter is the default lsp.codeAction chord.
func altEnter() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
}

func TestPlaygroundHelpOpensOnItsOwnContext(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":1}`))
	if got := m.helpContext(); got != ctxPlayground {
		t.Fatalf("help context = %q, want %q while the playground owns the keyboard", got, ctxPlayground)
	}
	groups := m.playgroundHelpGroups()
	if len(groups) < 2 {
		t.Fatalf("want a query-line and a result-buffer group, got %d", len(groups))
	}
	for _, g := range groups[:2] {
		if !g.Focused {
			t.Fatalf("group %q must be flagged as the focused context", g.Label)
		}
		if len(g.Entries) == 0 {
			t.Fatalf("group %q is empty", g.Label)
		}
		if !strings.Contains(g.Label, "jq playground") {
			t.Fatalf("group label %q should name the dialect", g.Label)
		}
	}
	// The sheet itself opens on that context and leads with the mode's keys.
	m.openHelp()
	if title := m.help.Title(); !strings.Contains(title, "playground context") {
		t.Fatalf("help opens on the playground context, got title %q", title)
	}
	view := m.help.Render(100)
	esc := strings.Index(view, "esc esc")
	if global := strings.Index(view, "global"); global >= 0 && esc > global {
		t.Fatal("the playground's own keys must lead the sheet, ahead of the global bindings")
	}
	if !strings.Contains(view, "esc esc") {
		t.Fatal("the playground cheatsheet must document esc esc")
	}
	if !strings.Contains(view, "ctrl+l") {
		t.Fatal("the playground cheatsheet must document the saved-filter picker")
	}
}

func TestPlaygroundHelpGroupsEmptyWhenNotFocused(t *testing.T) {
	m := playApp(t, `{"foo":1}`)
	if g := m.playgroundHelpGroups(); g != nil {
		t.Fatalf("a closed playground contributes no help groups, got %d", len(g))
	}
	if got := m.helpContext(); got == ctxPlayground {
		t.Fatal("without the mode the help context is the focused pane's")
	}
}

// TestPlaygroundHintsNameTheHelpKey keeps the discoverability fix honest: the
// info row is where a user finds out the cheatsheet exists at all.
func TestPlaygroundHintsNameTheHelpKey(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":1}`))
	if !hasHint(m.playHints(), playHelpHint) {
		t.Fatalf("query-line hints must name the help key, got %v", m.playHints())
	}
	m.play.setBufFocus(true)
	if !hasHint(m.playHints(), playHelpHint) {
		t.Fatalf("result-buffer hints must name the help key, got %v", m.playHints())
	}
}

func hasHint(hints []string, want string) bool {
	for _, h := range hints {
		if h == want {
			return true
		}
	}
	return false
}
