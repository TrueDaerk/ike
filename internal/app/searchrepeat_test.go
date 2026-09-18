package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/search"
)

// Tests for #2623: cmd+g / cmd+shift+g repeat the project's *last entered*
// in-file search in any editor of that project, kept per project across
// switches, and never fall through to find-in-path results while an in-file
// search is the most recent one.

// tempFileWith writes body to name under a fresh temp dir and returns the path.
func tempFileWith(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// openInEditor opens path in an editor through the explorer's open message.
func openInEditor(m Model, path string) Model {
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return tm.(Model)
}

// commitSearchIn opens path and commits "/<pattern>" through the full key
// path, the way cmd+f (editor.find -> vim "/") ends up doing.
func commitSearchIn(t *testing.T, m Model, path, pattern string) Model {
	t.Helper()
	m = openInEditor(m, path)
	// A first-start model may have the language-server setup dialog up, which
	// would swallow the scripted keys; esc closes it and is a harmless no-op
	// in normal mode.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = drainKey(m, tea.KeyPressMsg{Text: "/", Code: '/'})
	for _, r := range pattern {
		m = drainKey(m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// stepMatch sends search.nextMatch (delta 1) / search.prevMatch (delta -1) and
// drains whatever the step opened.
func stepMatch(m Model, delta int) Model {
	tm, cmd := m.Update(MatchStepMsg{Delta: delta})
	return drainCmd(tm.(Model), cmd)
}

func cursorOf(t *testing.T, m Model) (int, int) {
	t.Helper()
	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("no active editor")
	}
	return ed.Cursor()
}

// TestMatchStepRepeatsLastSearchInOtherEditor: search foo in file A, open
// file B, cmd+g steps foo in B (seeded as B's own search), cmd+shift+g steps
// back — no find-in-path result is opened.
func TestMatchStepRepeatsLastSearchInOtherEditor(t *testing.T) {
	m := dismissOnboarding(newSized())
	a := tempFileWith(t, "a.txt", "foo one foo two\n")
	b := tempFileWith(t, "b.txt", "x\nfoo b1\nfoo b2\n")
	m = commitSearchIn(t, m, a, "foo")
	m = openInEditor(m, b)
	if m.activeEditor().HasSearch() {
		t.Fatal("precondition: file B must start without a committed search")
	}
	m = stepMatch(m, 1)
	if line, col := cursorOf(t, m); line != 2 || col != 1 {
		t.Fatalf("cmd+g cursor at %d,%d, want 2,1 (1-based)", line, col)
	}
	if m.activeEditor().Path() != b {
		t.Fatalf("cmd+g moved to %s, want to stay in file B", m.activeEditor().Path())
	}
	if !m.activeEditor().HasSearch() {
		t.Fatal("cmd+g must seed the last search into file B's editor")
	}
	m = stepMatch(m, 1)
	if line, _ := cursorOf(t, m); line != 3 {
		t.Fatalf("second cmd+g on line %d, want 3", line)
	}
	m = stepMatch(m, -1)
	if line, _ := cursorOf(t, m); line != 2 {
		t.Fatalf("cmd+shift+g on line %d, want 2", line)
	}
	// n/N carry on from the seeded query, as if typed in B.
	m = drainKey(m, tea.KeyPressMsg{Text: "n", Code: 'n'})
	if line, _ := cursorOf(t, m); line != 3 {
		t.Fatalf("n after seeding on line %d, want 3", line)
	}
}

// TestMatchStepLastSearchNoMatchToasts: file B has no foo — a toast says so
// and the view stays where it is.
func TestMatchStepLastSearchNoMatchToasts(t *testing.T) {
	m := dismissOnboarding(newSized())
	a := tempFileWith(t, "a.txt", "foo one foo two\n")
	b := tempFileWith(t, "b.txt", "nothing here\nnor here\n")
	m = commitSearchIn(t, m, a, "foo")
	m = openInEditor(m, b)
	m = stepMatch(m, 1)
	if m.activeEditor().Path() != b {
		t.Fatalf("cmd+g moved to %s, want to stay in file B", m.activeEditor().Path())
	}
	if line, col := cursorOf(t, m); line != 1 || col != 1 {
		t.Fatalf("cursor moved to %d,%d on a miss", line, col)
	}
	if !strings.Contains(toastText(m), `no match for "foo"`) {
		t.Fatalf("missing no-match toast, got %q", toastText(m))
	}
}

// TestMatchStepRecencyBetweenInFileAndFindInPath: retained find-in-path
// results are walked by cmd+g while that scan is the most recent search; an
// in-file search committed afterwards takes cmd+g back, even from an editor
// that never typed it.
func TestMatchStepRecencyBetweenInFileAndFindInPath(t *testing.T) {
	m, hitPath := finderApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // close, keep results
	m = stepMatch(m, 1)
	if key := m.editorKeyForPath(hitPath); key == "" {
		t.Fatal("cmd+g must walk the retained find-in-path results after a scan")
	}
	local := tempFileWith(t, "local.txt", "bar one bar two\n")
	m = commitSearchIn(t, m, local, "bar")
	if line, col := cursorOf(t, m); line != 1 || col != 9 {
		t.Fatalf("committed /bar cursor at %d,%d, want 1,9", line, col)
	}
	// Another editor of the same project: cmd+g steps bar there, not the
	// find-in-path results.
	other := tempFileWith(t, "other.txt", "x\nbar here\n")
	m = openInEditor(m, other)
	m = stepMatch(m, 1)
	if m.activeEditor().Path() != other {
		t.Fatalf("cmd+g moved to %s, want to stay in the other editor", m.activeEditor().Path())
	}
	if line, col := cursorOf(t, m); line != 2 || col != 1 {
		t.Fatalf("cmd+g cursor at %d,%d, want 2,1", line, col)
	}
	// A new find-in-path scan reclaims the chord.
	tm, _ := m.Update(OpenFindInPathMsg{})
	m = tm.(Model)
	tm, _ = m.Update(search.BatchMsg{Matches: []search.Match{
		{Path: hitPath, Line: 2, Text: "two needle", StartCol: 4, EndCol: 10},
	}})
	m = tm.(Model)
	tm, _ = m.Update(search.DoneMsg{Total: 1})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = stepMatch(m, 1)
	if m.activeEditor().Path() != hitPath {
		t.Fatalf("after a new scan cmd+g stayed in %s, want the find-in-path hit", m.activeEditor().Path())
	}
}

// TestLastSearchKeptPerProject: the last search is project state — foo in
// P1, baz in P2 — and a switch back repeats the project's own query in a
// fresh editor.
func TestLastSearchKeptPerProject(t *testing.T) {
	a, b := twoProjects(t)
	m := dismissOnboarding(switchModel(t))
	write := func(dir, name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	a1 := write(a, "a1.txt", "foo one foo two\n")
	a2 := write(a, "a2.txt", "x\nfoo in a2\n")
	b1 := write(b, "b1.txt", "baz one baz two\n")
	b2 := write(b, "b2.txt", "x\nbaz in b2\nfoo not wanted\n")

	m = commitSearchIn(t, m, a1, "foo")
	m = switchTo(t, m, b)
	if m.inFileSearchRecent || !m.lastSearch.query.Empty() {
		t.Fatal("a project with no search yet must not inherit the other project's query")
	}
	m = commitSearchIn(t, m, b1, "baz")

	m = switchTo(t, m, a)
	if got := m.lastSearch.query.Pattern; got != "foo" {
		t.Fatalf("P1 last search %q, want foo", got)
	}
	m = openInEditor(m, a2)
	m = stepMatch(m, 1)
	if line, col := cursorOf(t, m); line != 2 || col != 1 {
		t.Fatalf("P1 cmd+g cursor at %d,%d, want 2,1 (foo in a2)", line, col)
	}

	m = switchTo(t, m, b)
	if got := m.lastSearch.query.Pattern; got != "baz" {
		t.Fatalf("P2 last search %q, want baz", got)
	}
	m = openInEditor(m, b2)
	m = stepMatch(m, 1)
	if line, col := cursorOf(t, m); line != 2 || col != 1 {
		t.Fatalf("P2 cmd+g cursor at %d,%d, want 2,1 (baz in b2)", line, col)
	}
}
