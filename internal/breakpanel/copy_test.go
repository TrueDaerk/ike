package breakpanel

// copy_test.go covers #2071: the marked row goes to the clipboard through a
// CopyMsg, with "y" and the modified copy chord — "c" stays the condition
// editor here.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/debug"
)

func copyChordKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper} }

func copyFilled(t *testing.T) Model {
	t.Helper()
	b := store(t)
	b.SetMeta("a.py", 7, debug.Meta{Condition: "x > 1"})
	m := New(nil)
	m.SetSize(80, 10)
	m.SetPreview(func(path string, line int) string { return "src line" })
	m.SetStore(b)
	return m
}

func TestCopyBreakpointRow(t *testing.T) {
	m := copyFilled(t)
	m.cursor = 2 // a.py:8, the conditional one
	msg, ok := run(t, m.Update(key("y"))).(CopyMsg)
	if !ok {
		t.Fatal("y must emit a CopyMsg")
	}
	if want := "a.py:8  src line  if x > 1"; msg.Text != want {
		t.Errorf("copied %q, want %q", msg.Text, want)
	}
	if msg.What != "breakpoint" {
		t.Errorf("what = %q, want \"breakpoint\"", msg.What)
	}
}

func TestCopyChordAliasesY(t *testing.T) {
	m := copyFilled(t)
	m.cursor = 1
	bare := run(t, m.Update(key("y"))).(CopyMsg)
	chord, ok := run(t, m.Update(copyChordKey())).(CopyMsg)
	if !ok {
		t.Fatal("cmd+c must emit a CopyMsg")
	}
	if chord != bare {
		t.Errorf("chord copied %+v, bare copied %+v", chord, bare)
	}
}

// TestCopyChordDoesNotOpenConditionEditor guards the overlap with "c".
func TestCopyChordDoesNotOpenConditionEditor(t *testing.T) {
	m := copyFilled(t)
	m.cursor = 1
	m.Update(copyChordKey())
	if m.editing {
		t.Error("cmd+c must copy, not open the condition editor")
	}
}

func TestCopyFileHeaderCopiesPath(t *testing.T) {
	m := copyFilled(t)
	m.cursor = 0
	msg := run(t, m.Update(key("y"))).(CopyMsg)
	if msg.Text != "a.py" || msg.What != "file" {
		t.Errorf("header copy = %+v, want the file path", msg)
	}
}

func TestCopyEmptyListDoesNothing(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(debug.NewBreakpoints())
	if cmd := m.Update(key("y")); cmd != nil {
		t.Errorf("empty list produced %+v, want no copy", cmd())
	}
	if cmd := m.Update(copyChordKey()); cmd != nil {
		t.Errorf("empty list chord produced %+v, want no copy", cmd())
	}
}

func TestCopyKeyInFooter(t *testing.T) {
	m := copyFilled(t)
	if !strings.Contains(m.View(), "y copy") {
		t.Error("footer must name the copy key")
	}
}
