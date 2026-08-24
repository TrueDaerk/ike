package problems

// copy_test.go covers #2071: the marked row goes to the clipboard through a
// CopyMsg, with "y" and the modified copy chord.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

func copyChordKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper} }

// copyFilled builds a panel with one file holding two diagnostics.
func copyFilled() Model {
	s := NewStore()
	s.Set("/proj/a.go", []ilsp.Diagnostic{
		diag(11, 4, 1, "undefined: fooBar", "E42"),
		diag(20, 0, 2, "unused variable", ""),
	})
	m := New(nil)
	m.SetSize(80, 12)
	m.SetDisplayPath(func(p string) string { return strings.TrimPrefix(p, "/proj/") })
	m.SetStore(s)
	return m
}

func TestCopyDiagnosticRow(t *testing.T) {
	m := copyFilled()
	m.cursor = 1 // the error under the file header
	cmd := m.Update(key("y"))
	if cmd == nil {
		t.Fatal("y produced no command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message = %T, want CopyMsg", cmd())
	}
	want := "a.go:12:5: undefined: fooBar (E42)"
	if msg.Text != want {
		t.Errorf("copied %q, want %q", msg.Text, want)
	}
	if msg.What != "problem" {
		t.Errorf("what = %q, want \"problem\"", msg.What)
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

func TestCopyEmptyListDoesNothing(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	m.SetStore(NewStore())
	if cmd := m.Update(key("y")); cmd != nil {
		t.Errorf("empty list produced %+v, want no copy", cmd())
	}
	if cmd := m.Update(copyChordKey()); cmd != nil {
		t.Errorf("empty list chord produced %+v, want no copy", cmd())
	}
}

func TestCopyKeyInFooter(t *testing.T) {
	m := copyFilled()
	if !strings.Contains(m.View(), "y copy") {
		t.Error("footer must name the copy key")
	}
}
