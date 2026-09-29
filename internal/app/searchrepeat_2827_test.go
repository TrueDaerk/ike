package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/allfind"
	"ike/internal/finder"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/palette"
	"ike/internal/registry"
	"ike/internal/search"
)

// Tests for #2827: the real cmd+g / cmd+shift+g chords repeat the project's
// last committed in-file query ("/", "?", cmd+f) in another file, in normal
// and insert mode and in a split, and opening that file through a picker or a
// find-in-path hit never steals the chord from the in-file query.

// pressMatchStep presses cmd+g (delta 1) or cmd+shift+g (delta -1) through the
// full key path — keymap layer included — rather than posting MatchStepMsg.
func pressMatchStep(m Model, delta int) Model {
	k := tea.KeyPressMsg{Code: 'g', Mod: tea.ModSuper}
	if delta < 0 {
		k.Mod |= tea.ModShift
	}
	return drainKey(m, k)
}

// typeKeys feeds s rune by rune as plain key presses.
func typeKeys(m Model, s string) Model {
	for _, r := range s {
		m = drainKey(m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	return m
}

// commitWith opens path and commits pattern through the search line opened by
// opener ("/", "?" or cmd+f).
func commitWith(t *testing.T, m Model, path, opener, pattern string) Model {
	t.Helper()
	m = openInEditor(m, path)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	switch opener {
	case "cmd+f":
		m = drainKey(m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModSuper})
	default:
		r := rune(opener[0])
		m = drainKey(m, tea.KeyPressMsg{Text: opener, Code: r})
	}
	m = typeKeys(m, pattern)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.shell.IsOpen() {
		t.Fatalf("%s opened an overlay instead of the editor's search line", opener)
	}
	if !m.inFileSearchRecent || m.lastSearch.query.Pattern != pattern {
		t.Fatalf("%s commit not recorded: recent=%v last=%q", opener, m.inFileSearchRecent, m.lastSearch.query.Pattern)
	}
	return m
}

// TestMatchStepChordRepeatsEveryCommitKind: "/", "?" and cmd+f commits in
// file A are all repeated by the real cmd+g chord in file B, in the commit's
// own direction like n; a miss toasts and stays put.
func TestMatchStepChordRepeatsEveryCommitKind(t *testing.T) {
	cases := []struct {
		opener   string
		wantLine int // 1-based line cmd+g lands on from the top of B
	}{
		{"/", 2},
		{"cmd+f", 2},
		{"?", 3}, // backward like n after "?": wraps to the last foo
	}
	for _, tc := range cases {
		t.Run(tc.opener, func(t *testing.T) {
			m := dismissOnboarding(newSized())
			a := tempFileWith(t, "a.txt", "foo one foo two\n")
			b := tempFileWith(t, "b.txt", "x\nfoo b1\nfoo b2\n")
			miss := tempFileWith(t, "c.txt", "nothing\n")
			m = commitWith(t, m, a, tc.opener, "foo")
			m = drainKey(m, tea.KeyPressMsg{Text: "n", Code: 'n'})
			m = openInEditor(m, b)
			m = pressMatchStep(m, 1)
			if m.activeEditor().Path() != b {
				t.Fatalf("cmd+g moved to %s, want file B", m.activeEditor().Path())
			}
			if line, col := cursorOf(t, m); line != tc.wantLine || col != 1 {
				t.Fatalf("cmd+g cursor at %d,%d, want %d,1", line, col, tc.wantLine)
			}
			if !m.activeEditor().HasSearch() {
				t.Fatal("cmd+g must seed the query into file B (highlights, n/N)")
			}
			m = openInEditor(m, miss)
			m = pressMatchStep(m, -1)
			if line, col := cursorOf(t, m); line != 1 || col != 1 {
				t.Fatalf("a miss moved the cursor to %d,%d", line, col)
			}
			if !strings.Contains(toastText(m), `no match for "foo"`) {
				t.Fatalf("missing no-match toast, got %q", toastText(m))
			}
		})
	}
}

// TestQuestionMarkInEditorStartsBackwardSearch: "?" in a focused normal-mode
// editor is vim's backward search, not the help overlay (the root "?" help
// shortcut used to swallow it, so a "?" commit never reached cmd+g).
func TestQuestionMarkInEditorStartsBackwardSearch(t *testing.T) {
	m := dismissOnboarding(newSized())
	a := tempFileWith(t, "a.txt", "foo\n")
	m = openInEditor(m, a)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = drainKey(m, tea.KeyPressMsg{Text: "?", Code: '?'})
	if m.shell.IsOpen() {
		t.Fatal(`"?" in an editor opened the help overlay`)
	}
	if !m.editorFindField() {
		t.Fatal(`"?" must open the editor's backward search line`)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyF1})
	if !m.shell.IsOpen() {
		t.Fatal("f1 must still open help from an editor")
	}
}

// TestMatchStepChordInInsertMode: the chord fires in insert mode too (#2622)
// and repeats the last query there.
func TestMatchStepChordInInsertMode(t *testing.T) {
	m := dismissOnboarding(newSized())
	a := tempFileWith(t, "a.txt", "foo one\n")
	b := tempFileWith(t, "b.txt", "x\nfoo b1\n")
	m = commitWith(t, m, a, "/", "foo")
	m = openInEditor(m, b)
	m = drainKey(m, tea.KeyPressMsg{Text: "i", Code: 'i'})
	m = pressMatchStep(m, 1)
	if line, col := cursorOf(t, m); line != 2 || col != 1 {
		t.Fatalf("insert-mode cmd+g cursor at %d,%d, want 2,1", line, col)
	}
}

// TestMatchStepChordInSplitPane: file B in a fresh split repeats the query.
func TestMatchStepChordInSplitPane(t *testing.T) {
	m := dismissOnboarding(newSized())
	a := tempFileWith(t, "a.txt", "foo one\n")
	b := tempFileWith(t, "b.txt", "x\nfoo b1\n")
	m = commitWith(t, m, a, "/", "foo")
	before := m.activeWS().Panes.Focused()
	tm, _ := m.Update(SplitFocusedMsg{Zone: layout.ZoneRight})
	m = tm.(Model)
	m = openInEditor(m, b)
	if m.activeWS().Panes.Focused() == before {
		t.Fatal("precondition: file B must sit in the new split")
	}
	m = pressMatchStep(m, 1)
	if m.activeEditor().Path() != b {
		t.Fatalf("cmd+g moved to %s, want file B", m.activeEditor().Path())
	}
	if line, col := cursorOf(t, m); line != 2 || col != 1 {
		t.Fatalf("split cmd+g cursor at %d,%d, want 2,1", line, col)
	}
}

// TestPickerOpensDoNotStealMatchStep: opening B through the file picker /
// search everywhere, through a hit of a merely re-opened find-in-path overlay,
// or through an all-projects hit leaves cmd+g on the in-file query. Only a
// find-in-path search the user runs takes it over.
func TestPickerOpensDoNotStealMatchStep(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	opens := map[string]func(m Model, a, b string) Model{
		"palette": func(m Model, a, b string) Model {
			tm, cmd := m.Update(palette.OpenFileMsg{Path: b, CountUsage: true})
			return drainCmd(tm.(Model), cmd)
		},
		"find-in-path hit": func(m Model, a, b string) Model {
			// A remembered query: the reopen replays it — not a new search.
			tm, _ := m.Update(OpenFindInPathMsg{})
			m = tm.(Model)
			m = typeKeys(m, "needle")
			m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
			m = commitWith(t, m, a, "/", "foo")
			tm, _ = m.Update(OpenFindInPathMsg{})
			m = tm.(Model)
			gen := m.searcher.Gen()
			tm, _ = m.Update(search.BatchMsg{Gen: gen, Matches: []search.Match{
				{Path: b, Line: 1, Text: "x needle", StartCol: 2, EndCol: 8},
			}})
			m = tm.(Model)
			tm, _ = m.Update(search.DoneMsg{Gen: gen, Total: 1})
			m = tm.(Model)
			tm, cmd := m.Update(finder.OpenLocationMsg{Path: b, Line: 1, Col: 0})
			m = drainCmd(tm.(Model), cmd)
			return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
		},
		"all-projects hit": func(m Model, a, b string) Model {
			m.markAllFindRecent() // an earlier all-projects scan
			m = commitWith(t, m, a, "/", "foo")
			tm, cmd := m.Update(allfind.OpenMatchMsg{Root: cwd, Path: b, Line: 1})
			return drainCmd(tm.(Model), cmd)
		},
	}
	for name, open := range opens {
		t.Run(name, func(t *testing.T) {
			m := dismissOnboarding(newSized())
			a := tempFileWith(t, "a.txt", "foo one\n")
			b := tempFileWith(t, "b.txt", "x needle\nfoo b1\n")
			m = commitWith(t, m, a, "/", "foo")
			m = open(m, a, b)
			if m.activeEditor() == nil || m.activeEditor().Path() != b {
				t.Fatal("precondition: file B must be the active editor")
			}
			m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
			m = pressMatchStep(m, 1)
			if m.activeEditor().Path() != b {
				t.Fatalf("cmd+g moved to %s, want to stay in file B", m.activeEditor().Path())
			}
			if line, col := cursorOf(t, m); line != 2 || col != 1 {
				t.Fatalf("cmd+g cursor at %d,%d, want 2,1 (foo in B)", line, col)
			}
		})
	}
}

// TestFindInPathTypedSearchReclaimsMatchStep: a find-in-path query the user
// types is a new search and takes cmd+g over from the in-file query; its
// stale generations do not.
func TestFindInPathTypedSearchReclaimsMatchStep(t *testing.T) {
	m := dismissOnboarding(newSized())
	a := tempFileWith(t, "a.txt", "foo one\n")
	m = commitWith(t, m, a, "/", "foo")
	tm, _ := m.Update(OpenFindInPathMsg{})
	m = tm.(Model)
	m = typeKeys(m, "ne")
	stale := m.searcher.Gen()
	m = typeKeys(m, "edle")
	tm, _ = m.Update(search.DoneMsg{Gen: stale})
	m = tm.(Model)
	if !m.inFileSearchRecent {
		t.Fatal("a stale scan generation must not steal cmd+g")
	}
	tm, _ = m.Update(search.DoneMsg{Gen: m.searcher.Gen()})
	m = tm.(Model)
	if m.inFileSearchRecent {
		t.Fatal("a typed find-in-path search must take cmd+g over")
	}
}

// TestMatchStepChordAfterProjectRoundTrip: commit in P1, switch to P2 and
// back, open another P1 file in a split — the real chord repeats P1's query.
// The model carries the global registry (switchModel's is empty), so the
// chord resolves through the keymap like in the app.
func TestMatchStepChordAfterProjectRoundTrip(t *testing.T) {
	p1, p2 := twoProjects(t)
	t.Setenv("IKE_CONFIG_DIR", "")
	tm, _ := NewWith(registry.Global(), host.MapConfig{"project.auto_save_on_switch": "false"}).
		Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := dismissOnboarding(tm.(Model))
	a1 := filepath.Join(p1, "a1.txt")
	a2 := filepath.Join(p1, "a2.txt")
	for path, body := range map[string]string{a1: "foo one\n", a2: "x\nfoo in a2\n"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m = commitWith(t, m, a1, "/", "foo")
	m = switchTo(t, m, p2)
	m = switchTo(t, m, p1)
	m = dismissOnboarding(m)
	tm, _ = m.Update(SplitFocusedMsg{Zone: layout.ZoneRight})
	m = openInEditor(tm.(Model), a2)
	m = pressMatchStep(m, 1)
	if m.activeEditor().Path() != a2 {
		t.Fatalf("cmd+g moved to %s, want a2", m.activeEditor().Path())
	}
	if line, col := cursorOf(t, m); line != 2 || col != 1 {
		t.Fatalf("cmd+g after the round trip at %d,%d, want 2,1", line, col)
	}
}
