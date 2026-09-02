package app

import (
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// panefind_test.go covers #2409: the shared find chord (search.open, cmd+f)
// opens whatever the focused pane calls its search, and a pane without one
// says so instead of swallowing the key.

// findChord is the app keymap's search.open chord as a key event: cmd+f on
// macOS, folded to ctrl+f everywhere else (keymap.NormalizeKey).
func findChord() tea.KeyPressMsg {
	if runtime.GOOS == "darwin" {
		return tea.KeyPressMsg{Code: 'f', Mod: tea.ModMeta}
	}
	return tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
}

// findApp is a sized app with the first-start LSP dialog (#301) dismissed: it
// owns the keyboard ahead of the keymap layer, so a scripted chord would
// otherwise never reach a pane. A no-op where nothing is missing.
func findApp(t *testing.T) Model {
	t.Helper()
	m := newSized()
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	return m
}

// TestFindChordOpensTheExplorerSpeedSearch is the issue's headline case: the
// chord that finds in the editor finds in the explorer too.
func TestFindChordOpensTheExplorerSpeedSearch(t *testing.T) {
	m := findApp(t)
	m.setFocus(pane.ExplorerKey)
	if m.explorer().Searching() {
		t.Fatal("the speed search starts closed")
	}
	m = drainKey(m, findChord())
	if !m.explorer().Searching() {
		t.Fatal("the find chord must open the explorer speed search")
	}
}

// TestFindChordOpensToolWindowFilters walks the singleton tool windows the
// issue lists: the chord focuses each pane's filter row.
func TestFindChordOpensToolWindowFilters(t *testing.T) {
	cases := []struct {
		name    string
		open    tea.Msg
		key     string
		filters func(m Model, key string) bool
	}{
		{"problems", ProblemsToggleMsg{}, pane.ProblemsKey,
			func(m Model, key string) bool { return m.activeWS().Panes.Get(key).Problems().Filtering() }},
		{"usages", UsagesToggleMsg{}, pane.UsagesKey,
			func(m Model, key string) bool { return m.activeWS().Panes.Get(key).Usages().Filtering() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := findApp(t)
			tm, cmd := m.Update(tc.open)
			m = drainCmd(tm.(Model), cmd)
			if m.activeWS().Panes.Get(tc.key) == nil {
				t.Fatalf("%s pane did not open", tc.name)
			}
			m.setFocus(tc.key)
			if tc.filters(m, tc.key) {
				t.Fatal("the filter row starts unfocused")
			}
			m = drainKey(m, findChord())
			if !tc.filters(m, tc.key) {
				t.Fatal("the find chord must focus the pane's filter row")
			}
		})
	}
}

// TestFindChordInAnEditorStaysWithEditorFind: the Editor-scoped editor.find
// shadows the Global search.open, so the chord keeps its editor meaning.
func TestFindChordInAnEditorStaysWithEditorFind(t *testing.T) {
	m := findApp(t)
	key := m.activeEditorKey()
	if key == "" {
		t.Skip("no editor pane in the default layout")
	}
	m.setFocus(key)
	m = drainKey(m, findChord())
	ed := m.activeWS().Panes.Get(key).Editor()
	if ed == nil {
		t.Fatal("the focused editor pane has no editor")
	}
	if !ed.Capturing() {
		t.Fatal("the find chord must open the editor's own search command line")
	}
}

// TestFindChordNotifiesWithoutAPaneSearch: a pane with no search of its own
// answers with a notice rather than nothing at all.
func TestFindChordNotifiesWithoutAPaneSearch(t *testing.T) {
	m := findApp(t)
	tm, cmd := m.Update(StructureToggleMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.activeWS().Panes.Get(pane.StructureKey) == nil {
		t.Skip("the structure pane did not open in this environment")
	}
	m.setFocus(pane.StructureKey)
	tm, cmd = m.Update(findChord())
	m = tm.(Model)
	// Feed the dispatched command back by hand rather than through drainCmd:
	// the notice's own expiry tick would otherwise fire inline and take the
	// toast straight back off the stack.
	for _, msg := range cmdMsgs(cmd) {
		tm, _ = m.Update(msg)
		m = tm.(Model)
	}
	if !hasNotice(m, "No search in this pane") {
		t.Fatalf("expected the no-search notice, toasts = %v", noticeTexts(m))
	}
}

// noticeTexts lists the live toast messages, for assertions.
func noticeTexts(m Model) []string {
	out := make([]string, 0, len(m.toasts))
	for _, t := range m.toasts {
		out = append(out, t.text)
	}
	return out
}

// hasNotice reports whether any live toast contains want.
func hasNotice(m Model, want string) bool {
	for _, s := range noticeTexts(m) {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}
