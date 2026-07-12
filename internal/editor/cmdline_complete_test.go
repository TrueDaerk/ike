package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// cmdTree builds a fixture with Development/, Downloads/ and notes.txt.
func cmdTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"Development", "Downloads"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func tab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab} }

func TestSplitPathArg(t *testing.T) {
	cases := []struct {
		line, prefix, arg string
		ok                bool
	}{
		{"e ~/De", "e ", "~/De", true},
		{"edit  /usr/lo", "edit  ", "/usr/lo", true},
		{"w! out", "w! ", "out", true},
		{"wq f", "wq ", "f", true},
		{"e", "", "", false},   // no argument started
		{"d 3", "", "", false}, // not a path verb
		{"s/a/b/", "", "", false},
	}
	for _, c := range cases {
		prefix, arg, ok := splitPathArg(c.line)
		if prefix != c.prefix || arg != c.arg || ok != c.ok {
			t.Errorf("splitPathArg(%q) = %q %q %v, want %q %q %v",
				c.line, prefix, arg, ok, c.prefix, c.arg, c.ok)
		}
	}
}

func TestCmdlineTabCompletesPath(t *testing.T) {
	root := cmdTree(t)
	m, _ := loaded(t, "x\n")
	m = typeKeys(m, ":e "+filepath.Join(root, "Dev"))
	m = send(m, tab())
	want := "e " + filepath.Join(root, "Development") + string(filepath.Separator)
	if m.cmdline != want {
		t.Fatalf("cmdline = %q, want %q", m.cmdline, want)
	}
	// Repeated tab descends into the (empty) directory: nothing new, inert.
	m = send(m, tab())
	if m.cmdline != want {
		t.Fatalf("tab in empty dir must be inert, got %q", m.cmdline)
	}
}

func TestCmdlineTabAmbiguousShowsHint(t *testing.T) {
	root := cmdTree(t)
	m, _ := loaded(t, "x\n")
	m = typeKeys(m, ":w "+filepath.Join(root, "D"))
	m = send(m, tab())
	if len(m.cmdSuggest) != 2 {
		t.Fatalf("cmdSuggest = %v, want the two directories", m.cmdSuggest)
	}
	row := m.commandLineRow()
	if !strings.Contains(row, "Development"+string(filepath.Separator)) || !strings.Contains(row, "Downloads"+string(filepath.Separator)) {
		t.Fatalf("hint row must name the candidates: %q", row)
	}
	// Typing narrows the open hint list.
	m = typeKeys(m, "ev")
	if len(m.cmdSuggest) != 1 {
		t.Fatalf("typing must narrow the hint, got %v", m.cmdSuggest)
	}
	// Enter clears the completion state (the command itself runs).
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.cmdSuggest != nil {
		t.Fatalf("enter must clear the suggestions, got %v", m.cmdSuggest)
	}
}

func TestCmdlineTabInertOnNonPathInput(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m = typeKeys(m, ":d 3")
	m = send(m, tab())
	if m.cmdline != "d 3" || m.cmdSuggest != nil {
		t.Fatalf("tab on a non-path command must be inert, got %q %v", m.cmdline, m.cmdSuggest)
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	// Search mode: tab must not complete either.
	m = typeKeys(m, "/e ")
	m = send(m, tab())
	if m.cmdSuggest != nil {
		t.Fatalf("tab while searching must not complete, got %v", m.cmdSuggest)
	}
}

func TestCmdlineEscapeClearsSuggestions(t *testing.T) {
	root := cmdTree(t)
	m, _ := loaded(t, "x\n")
	m = typeKeys(m, ":e "+filepath.Join(root, "D"))
	m = send(m, tab())
	if len(m.cmdSuggest) == 0 {
		t.Fatal("expected suggestions before escape")
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.cmdSuggest != nil {
		t.Fatalf("escape must clear the suggestions, got %v", m.cmdSuggest)
	}
}
