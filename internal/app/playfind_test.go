package app

import (
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
)

// playfind_test.go covers #2383: the find chord (editor.find, default cmd+f)
// opens the result buffer's search from either focus, so searching a result no
// longer costs a tab there and a tab back.

// playFindKey is the app keymap's editor.find chord as a key event: cmd+f on
// macOS, folded to ctrl+f everywhere else (keymap.NormalizeKey).
func playFindKey() tea.KeyPressMsg {
	if runtime.GOOS == "darwin" {
		return tea.KeyPressMsg{Code: 'f', Mod: tea.ModMeta}
	}
	return tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
}

// playNoOnboarding dismisses the first-start LSP dialog when the environment
// opened one (a registered language whose server is missing offers an install,
// #301): it owns the keyboard ahead of the playground, so a scripted chord
// would never reach the mode. A no-op where nothing is missing.
func playNoOnboarding(m Model) Model {
	if m.onboardingOpen() {
		return m.closeOnboarding().(Model)
	}
	return m
}

// playSearchFor drives the open search prompt: types pattern and confirms it.
func playSearchFor(m Model, pattern string) Model {
	m = playKeys(m, pattern)
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// TestPlayFindFromQueryLineSearchesResult is the issue's acceptance case: the
// find chord pressed on the query line opens the search in the result buffer
// without a preceding tab, and the keyboard is in the result buffer with it.
func TestPlayFindFromQueryLineSearchesResult(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, `{"foo":["alpha","beta","gamma"]}`)))
	m = setProgram(m, ".foo[]")
	if m.play.bufFocus {
		t.Fatal("the query line is the playground's default focus")
	}
	program := m.play.program
	result := m.play.result.Text()

	m = drainKey(m, playFindKey())
	if !m.play.bufFocus {
		t.Fatal("the find chord must move the keyboard into the result buffer")
	}
	m = playSearchFor(m, "gamma")
	if !m.play.resultEd.HasSearch() {
		t.Fatal("the find chord must open the result buffer's search")
	}
	line, _ := m.play.resultEd.Cursor()
	if want := 3; line != want {
		t.Errorf("cursor line = %d, want %d (the \"gamma\" row)", line, want)
	}
	// The chord is no query input and leaves the program untouched.
	if m.play.program != program {
		t.Errorf("program = %q, want it unchanged (%q)", m.play.program, program)
	}
	if m.play.result.Text() != result {
		t.Errorf("the search changed the result:\n%s", m.play.result.Text())
	}
}

// TestPlayFindFromResultBufferMatchesSlash: in the result buffer the chord
// does exactly what "/" does there.
func TestPlayFindFromResultBufferMatchesSlash(t *testing.T) {
	body := `{"foo":["alpha","beta","gamma"]}`
	viaChord := playNoOnboarding(openJQ(t, playApp(t, body)))
	viaChord = setProgram(viaChord, ".foo[]")
	viaChord = drainKey(viaChord, tea.KeyPressMsg{Code: tea.KeyTab})
	viaChord = playSearchFor(drainKey(viaChord, playFindKey()), "gamma")

	viaSlash := playNoOnboarding(openJQ(t, playApp(t, body)))
	viaSlash = setProgram(viaSlash, ".foo[]")
	viaSlash = drainKey(viaSlash, tea.KeyPressMsg{Code: tea.KeyTab})
	viaSlash = playSearchFor(playKeys(viaSlash, "/"), "gamma")

	cl, cc := viaChord.play.resultEd.Cursor()
	sl, sc := viaSlash.play.resultEd.Cursor()
	if cl != sl || cc != sc {
		t.Errorf("chord cursor = %d:%d, \"/\" cursor = %d:%d — they must agree", cl, cc, sl, sc)
	}
	if !viaChord.play.bufFocus {
		t.Error("searching from the result buffer must leave the keyboard there")
	}
}

// TestPlayFindNextPrevAfterQueryLineStart: n / N step the matches after a
// search started from the query line — the focus the chord leaves behind is
// the one those keys belong to.
func TestPlayFindNextPrevAfterQueryLineStart(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, `{"foo":["ha","hb","hc"]}`)))
	m = setProgram(m, ".foo[]")
	m = playSearchFor(drainKey(m, playFindKey()), "h")

	first, _ := m.play.resultEd.Cursor()
	m = playKeys(m, "n")
	second, _ := m.play.resultEd.Cursor()
	if second == first {
		t.Fatalf("n did not move: still on line %d", first)
	}
	m = playKeys(m, "N")
	if back, _ := m.play.resultEd.Cursor(); back != first {
		t.Errorf("N returned to line %d, want the previous match on %d", back, first)
	}
}

// TestPlayFindFocusAfterClosingSearch: esc closes the prompt and leaves the
// keyboard in the result buffer — the same place no matter which focus the
// search was started from, so the next key means the same thing either way.
func TestPlayFindFocusAfterClosingSearch(t *testing.T) {
	for _, tc := range []struct {
		name string
		tab  bool
	}{{"from the query line", false}, {"from the result buffer", true}} {
		t.Run(tc.name, func(t *testing.T) {
			m := playNoOnboarding(openJQ(t, playApp(t, `{"foo":["alpha","beta"]}`)))
			m = setProgram(m, ".foo[]")
			if tc.tab {
				m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
			}
			m = playKeys(drainKey(m, playFindKey()), "alp")
			m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEsc})
			if !m.play.bufFocus {
				t.Fatal("closing the search must leave the keyboard in the result buffer")
			}
			if !m.playOpen() {
				t.Fatal("closing the search must not close the playground")
			}
			if got := m.play.resultEd.ModeName(); got != editor.Normal {
				t.Errorf("result buffer mode = %v, want normal", got)
			}
		})
	}
}

// TestPlayFindHelpListsChordInBothTables: the new way is in both focus tables
// of the cheatsheet, resolved from the live binding rather than hard-coded.
func TestPlayFindHelpListsChordInBothTables(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, `{"foo":[1]}`)))
	chord, ok := m.playChordFor("editor.find")
	if !ok {
		t.Fatal("editor.find must be bound in the default table")
	}
	groups := m.playgroundHelpGroups()
	if len(groups) < 2 {
		t.Fatalf("got %d help groups, want the two focus tables", len(groups))
	}
	for i, g := range groups[:2] {
		found := false
		for _, e := range g.Entries {
			if e.Shortcut == chord {
				found = true
			}
		}
		if !found {
			t.Errorf("group %d (%q) does not list the find chord %q", i, g.Label, chord)
		}
	}
}

// TestPlayFindLeavesResultReadOnly: the search prompt's text never reaches the
// read-only buffer (#1762).
func TestPlayFindLeavesResultReadOnly(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, `{"foo":["alpha"]}`)))
	m = setProgram(m, ".foo[]")
	before := m.play.resultEd.Text()
	m = playSearchFor(drainKey(m, playFindKey()), "alpha")
	if got := m.play.resultEd.Text(); got != before {
		t.Errorf("the result buffer changed:\n%s\nwant:\n%s", got, before)
	}
	if strings.Contains(m.play.program, "alpha") {
		t.Errorf("the search text leaked into the program: %q", m.play.program)
	}
}
