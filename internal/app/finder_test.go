package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/explorer"
	"ike/internal/search"
)

// finderApp opens the find-in-path overlay and feeds it one streamed result
// pointing at a real file, as the scan service would.
func finderApp(t *testing.T) (Model, string) {
	t.Helper()
	m := newSized()
	path := filepath.Join(t.TempDir(), "hit.go")
	if err := os.WriteFile(path, []byte("one\ntwo needle\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(OpenFindInPathMsg{})
	m = tm.(Model)
	if !m.finder.IsOpen() {
		t.Fatal("project.findInPath must open the overlay")
	}
	tm, _ = m.Update(search.BatchMsg{Matches: []search.Match{
		{Path: path, Line: 2, Text: "two needle", StartCol: 4, EndCol: 10},
	}})
	m = tm.(Model)
	tm, _ = m.Update(search.DoneMsg{Total: 1})
	return tm.(Model), path
}

func TestFindInPathCommandRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"project.findInPath", "search.nextMatch", "search.prevMatch"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("%s must be a registry command", id)
		}
	}
}

func TestFinderRendersStreamedResults(t *testing.T) {
	m, _ := finderApp(t)
	frame := m.render()
	// The title renders styled per rune; assert on the input label and the
	// streamed result's file name instead.
	if !strings.Contains(frame, "Search") || !strings.Contains(frame, "hit.go") {
		t.Fatal("overlay with streamed results missing from the frame")
	}
}

func TestFinderEnterOpensFileAtMatch(t *testing.T) {
	m, path := finderApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.finder.IsOpen() {
		t.Fatal("enter must close the overlay")
	}
	key := m.editorKeyForPath(path)
	if key == "" {
		t.Fatal("enter must open the matched file")
	}
	ed := m.activeWS().Panes.Get(key).Editor()
	if line, col := ed.Cursor(); line != 2 || col != 5 {
		t.Fatalf("cursor at %d,%d, want 2,5 (1-based)", line, col)
	}
}

func TestMatchStepNavigatesWithoutOverlay(t *testing.T) {
	m, path := finderApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // close, keep results
	tm, cmd := m.Update(MatchStepMsg{Delta: 1})
	m = tm.(Model)
	for cmd != nil { // drain the open/reparse batch
		if msg := cmd(); msg != nil {
			tm, cmd = m.Update(msg)
			m = tm.(Model)
		} else {
			break
		}
	}
	if key := m.editorKeyForPath(path); key == "" {
		t.Fatal("next-match must open the file with the overlay closed")
	}
}

// commitInFileSearch opens path in an editor and commits "/foo" through the
// full key path, as cmd+f (editor.find -> vim "/") ends up doing.
func commitInFileSearch(t *testing.T, m Model, path string) Model {
	t.Helper()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Text: "/", Code: '/'})
	for _, r := range "foo" {
		m = drainKey(m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// TestMatchStepRepeatsInFileSearchWhenMostRecent covers #376: after a committed
// in-file search, f3/shift+f3 (search.nextMatch/prevMatch) repeat it like n/N
// instead of stepping the retained find-in-path results.
func TestMatchStepRepeatsInFileSearchWhenMostRecent(t *testing.T) {
	m, hitPath := finderApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // close, keep results
	path := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(path, []byte("foo one foo two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = commitInFileSearch(t, m, path)
	ed := m.activeEditor()
	if line, col := ed.Cursor(); line != 1 || col != 9 {
		t.Fatalf("committed /foo cursor at %d,%d, want 1,9 (1-based)", line, col)
	}
	// f3 wraps to the first in-file match; shift+f3 steps back.
	tm, _ := m.Update(MatchStepMsg{Delta: 1})
	m = tm.(Model)
	if line, col := m.activeEditor().Cursor(); line != 1 || col != 1 {
		t.Fatalf("f3 cursor at %d,%d, want 1,1", line, col)
	}
	tm, _ = m.Update(MatchStepMsg{Delta: -1})
	m = tm.(Model)
	if line, col := m.activeEditor().Cursor(); line != 1 || col != 9 {
		t.Fatalf("shift+f3 cursor at %d,%d, want 1,9", line, col)
	}
	if key := m.editorKeyForPath(hitPath); key != "" {
		t.Fatal("f3 must not open find-in-path matches while the in-file search is most recent")
	}
}

// TestMatchStepMostRecentSearchWins covers the flip side of #376: a new
// find-in-path scan after a committed in-file search reclaims f3.
func TestMatchStepMostRecentSearchWins(t *testing.T) {
	m, hitPath := finderApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	path := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(path, []byte("foo one foo two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = commitInFileSearch(t, m, path)
	// A new find-in-path scan makes path results the most recent search again.
	tm, _ := m.Update(OpenFindInPathMsg{})
	m = tm.(Model)
	tm, _ = m.Update(search.BatchMsg{Matches: []search.Match{
		{Path: hitPath, Line: 2, Text: "two needle", StartCol: 4, EndCol: 10},
	}})
	m = tm.(Model)
	tm, _ = m.Update(search.DoneMsg{Total: 1})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	tm, cmd := m.Update(MatchStepMsg{Delta: 1})
	m = tm.(Model)
	for cmd != nil {
		if msg := cmd(); msg != nil {
			tm, cmd = m.Update(msg)
			m = tm.(Model)
		} else {
			break
		}
	}
	if key := m.editorKeyForPath(hitPath); key == "" {
		t.Fatal("a newer find-in-path scan must reclaim f3 from the in-file search")
	}
}

func TestFinderSwallowsKeysFromPanes(t *testing.T) {
	m, _ := finderApp(t)
	before := m.finder.Query()
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if !m.finder.IsOpen() {
		t.Fatal("plain keys must stay in the overlay")
	}
	if m.finder.Query() == before {
		t.Fatal("typed key must edit the finder query, not move a pane cursor")
	}
}

func TestFinderMouseClicksToggleAndDismiss(t *testing.T) {
	m, _ := finderApp(t)
	v := m.finder.View()
	bx := (m.width - lipgloss.Width(v)) / 2
	by := (m.height - lipgloss.Height(v)) / 2
	// The Case toggle sits on content row 3 (title, blank, query, toggles),
	// starting at content column 8; +2/+1 skip the border and padding.
	tm, _ := m.Update(tea.MouseClickMsg{X: bx + 2 + 10, Y: by + 1 + 3, Button: tea.MouseLeft})
	m = tm.(Model)
	if !strings.Contains(m.render(), "[x] Case") {
		t.Fatal("click on the Case toggle must flip it")
	}
	// A click outside the overlay dismisses it (never reaching the panes).
	tm, _ = m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m = tm.(Model)
	if m.finder.IsOpen() {
		t.Fatal("click outside the overlay must close it")
	}
}
