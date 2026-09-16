package app

// #2603: while an editor's search line is open, search.nextMatch /
// search.prevMatch (cmd+g / cmd+shift+g) step the preview through the matches
// of the pattern being typed instead of repeating the previously committed
// search, and the line stays open.

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
)

// openSearchLine opens a three-match file in an editor and types "/foo"
// without committing it.
func openSearchLine(t *testing.T, m Model) Model {
	t.Helper()
	// A first-start model may have the language-server setup dialog up, which
	// would swallow the scripted keys; esc closes it and is a harmless no-op
	// in normal mode.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	path := filepath.Join(t.TempDir(), "step.txt")
	body := "alpha\nfoo one\nbar\nfoo two\nbaz\nfoo three\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Text: "/", Code: '/'})
	for _, r := range "foo" {
		m = drainKey(m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	return m
}

func TestMatchStepStepsOpenEditorSearchLine(t *testing.T) {
	m := openSearchLine(t, newSized())
	if line, _ := m.activeEditor().Cursor(); line != 2 {
		t.Fatalf("preview cursor on line %d, want 2 (1-based)", line)
	}
	tm, _ := m.Update(MatchStepMsg{Delta: 1})
	m = tm.(Model)
	ed := m.activeEditor()
	if line, _ := ed.Cursor(); line != 4 {
		t.Fatalf("next-match cursor on line %d, want 4", line)
	}
	if got := ed.CommandLine(); got != "/foo" {
		t.Fatalf("command line = %q, want the search line still open with %q", got, "/foo")
	}
	// Back again, then commit: enter keeps the previewed match.
	tm, _ = m.Update(MatchStepMsg{Delta: -1})
	m = tm.(Model)
	if line, _ := m.activeEditor().Cursor(); line != 2 {
		t.Fatalf("prev-match cursor on line %d, want 2", line)
	}
	tm, _ = m.Update(MatchStepMsg{Delta: 1})
	m = tm.(Model)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if line, _ := m.activeEditor().Cursor(); line != 4 {
		t.Fatalf("committed cursor on line %d, want the stepped match on 4", line)
	}
	if m.activeEditor().CommandLine() != "" {
		t.Fatal("enter must close the search line")
	}
}

// The open line wins over the chord's older meaning: a search committed
// earlier must not be the one that steps.
func TestMatchStepPrefersOpenSearchLineOverCommitted(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "prev.txt")
	if err := os.WriteFile(path, []byte("bar\nbar\nbar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // close a first-start dialog
	m = commitInFileSearch(t, m, path)                    // commits "/foo" — no match here
	m = openSearchLine(t, m)
	before := m.activeEditor().CommandLine()
	tm, _ := m.Update(MatchStepMsg{Delta: 1})
	m = tm.(Model)
	if got := m.activeEditor().CommandLine(); got != before {
		t.Fatalf("command line = %q, want it untouched (%q)", got, before)
	}
	if line, _ := m.activeEditor().Cursor(); line != 4 {
		t.Fatalf("cursor on line %d, want the open line's second match on 4", line)
	}
}
