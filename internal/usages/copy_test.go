package usages

// copy_test.go covers #2071: the marked row goes to the clipboard through a
// CopyMsg, with "y" and the modified copy chord.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func copyChordKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper} }

// copyFilled is filled() with the project-relative shortener wired.
func copyFilled() Model {
	m := filled()
	m.SetDisplayPath(func(p string) string { return strings.TrimPrefix(p, "/proj/") })
	return m
}

func TestCopyReferenceRow(t *testing.T) {
	m := copyFilled()
	m.cursor = 1 // the first hit, where Set leaves the cursor
	cmd := m.Update(key("y"))
	if cmd == nil {
		t.Fatal("y produced no command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message = %T, want CopyMsg", cmd())
	}
	if want := "a.go:4:2  Foo()"; msg.Text != want {
		t.Errorf("copied %q, want %q", msg.Text, want)
	}
	if msg.What != "usage" {
		t.Errorf("what = %q, want \"usage\"", msg.What)
	}
}

func TestCopyChordAliasesY(t *testing.T) {
	m := copyFilled()
	m.cursor = 2
	bare := m.Update(key("y"))().(CopyMsg)
	chord := m.Update(copyChordKey())
	if chord == nil {
		t.Fatal("cmd+c produced no command")
	}
	if got := chord().(CopyMsg); got != bare {
		t.Errorf("chord copied %+v, bare copied %+v", got, bare)
	}
}

func TestCopyFileHeaderCopiesPath(t *testing.T) {
	m := copyFilled()
	m.cursor = 0
	msg := m.Update(key("y"))().(CopyMsg)
	if msg.Text != "a.go" || msg.What != "file" {
		t.Errorf("header copy = %+v, want the file path", msg)
	}
}

func TestCopyEmptyPaneDoesNothing(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	if cmd := m.Update(key("y")); cmd != nil {
		t.Errorf("empty pane produced %+v, want no copy", cmd())
	}
	if cmd := m.Update(copyChordKey()); cmd != nil {
		t.Errorf("empty pane chord produced %+v, want no copy", cmd())
	}
}

func TestCopyKeyInFooter(t *testing.T) {
	m := copyFilled()
	if !strings.Contains(m.View(), "y copy") {
		t.Error("footer must name the copy key")
	}
}
