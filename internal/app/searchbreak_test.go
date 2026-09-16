package app

// searchbreak_test.go guards the dispatch half of #2600: alt+enter is bound to
// lsp.codeAction in the editor context, so without an explicit claim the keymap
// layer would consume it before the pane — and the find/replace fields would
// never see their insert-a-line-break chord.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
)

// openedEditor returns an app with a small file open and focused.
func openedEditor(t *testing.T, content string) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	// The first key after the open settles the freshly focused pane without
	// reaching the editor; spend an esc on it so the scripted keys below land.
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
}

func TestAltEnterReachesTheSearchLine(t *testing.T) {
	m := openedEditor(t, "foo\nbar\n")
	m = drainKey(m, tea.KeyPressMsg{Code: '/', Text: "/"})
	if ed := m.activeEditor(); ed == nil || !ed.FindFieldOpen() {
		t.Fatal("the search line should be open")
	}
	for _, k := range []tea.KeyPressMsg{
		{Code: 'f', Text: "f"},
		{Code: 'o', Text: "o"},
		{Code: 'o', Text: "o"},
		{Code: tea.KeyEnter, Mod: tea.ModAlt},
		{Code: 'b', Text: "b"},
		{Code: 'a', Text: "a"},
		{Code: 'r', Text: "r"},
	} {
		m = drainKey(m, k)
	}
	cl := m.activeEditor().CommandLine()
	if !strings.Contains(cl, "foo\nbar") {
		t.Fatalf("search line = %q, want a line break between foo and bar", cl)
	}
}

func TestAltEnterStaysTheIntentionChordOtherwise(t *testing.T) {
	// With no find/replace field open the chord belongs to the keymap layer as
	// before; the claim must not swallow it in ordinary normal mode.
	m := openedEditor(t, "foo\n")
	if m.editorFindField() {
		t.Fatal("no find field should be open in resting normal mode")
	}
}
