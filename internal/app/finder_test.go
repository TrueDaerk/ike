package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	ed := m.panes.Get(key).Editor()
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
