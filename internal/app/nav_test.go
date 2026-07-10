package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// nav_test.go covers the navigation-history flow (Roadmap 0220, #218):
// jumps through the open funnel record entries, nav.back / nav.forward
// traverse them, and an exhausted direction toasts instead of erroring.

// navProject builds a project with three files of a few lines each.
func navProject(t *testing.T) (root string, files []string) {
	t.Helper()
	root = t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, []byte("l0\nl1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}
	return root, files
}

func (m Model) atPosition(t *testing.T, path string, line int) Model {
	t.Helper()
	ed := m.panes.Get(m.activeEditorKey()).Editor()
	gotLine, _ := ed.Cursor()
	if ed.Path() != path || gotLine-1 != line {
		t.Fatalf("at %s:%d, want %s:%d", ed.Path(), gotLine-1, path, line)
	}
	return m
}

func TestNavBackForwardAcrossJumps(t *testing.T) {
	_, files := navProject(t)
	m := newSized()

	// Jump chain: open a, definition-jump to b:5, then to c:7.
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[1], Line: 5, Col: 2})
	m = tm.(Model)
	m = m.atPosition(t, files[1], 5)
	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[2], Line: 7, Col: 0})
	m = tm.(Model)
	m = m.atPosition(t, files[2], 7)

	// Back retraces c -> b -> a.
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[1], 5)
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[0], 0)

	// Exhausted back direction: toast, position unchanged.
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[0], 0)
	if len(m.toasts) != 1 || !strings.Contains(m.toasts[0].text, "no earlier position") {
		t.Fatalf("toasts = %+v", m.toasts)
	}

	// Forward re-traverses a -> b -> c.
	tm, _ = m.Update(NavForwardMsg{})
	m = tm.(Model).atPosition(t, files[1], 5)
	tm, _ = m.Update(NavForwardMsg{})
	m = tm.(Model).atPosition(t, files[2], 7)
	tm, _ = m.Update(NavForwardMsg{})
	m = tm.(Model).atPosition(t, files[2], 7)
	if len(m.toasts) != 2 || !strings.Contains(m.toasts[0].text, "no later position") {
		t.Fatalf("toasts = %+v", m.toasts)
	}
}

func TestNavSameFileLineJumpRecords(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	// A same-file jump to another line (references pick, search result).
	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[0], Line: 8, Col: 1})
	m = tm.(Model).atPosition(t, files[0], 8)
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[0], 0)
	tm, _ = m.Update(NavForwardMsg{})
	tm.(Model).atPosition(t, files[0], 8)
}

func TestNavFreshJumpTruncatesForward(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[1], Line: 3, Col: 0})
	m = tm.(Model)
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[0], 0)

	// A fresh jump while back in history invalidates the forward tail.
	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[2], Line: 2, Col: 0})
	m = tm.(Model).atPosition(t, files[2], 2)
	tm, _ = m.Update(NavForwardMsg{})
	m = tm.(Model).atPosition(t, files[2], 2)
	if len(m.toasts) != 1 || !strings.Contains(m.toasts[0].text, "no later position") {
		t.Fatalf("forward after fresh jump must be empty: %+v", m.toasts)
	}
	// And back still walks the fresh chain: c returns to a, the position the
	// fresh jump departed from (b:3 lived on the truncated forward tail).
	tm, _ = m.Update(NavBackMsg{})
	tm.(Model).atPosition(t, files[0], 0)
}

func TestNavCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"nav.back", "nav.forward"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Errorf("command %s not registered", id)
		}
	}
}

func TestNavBackAfterLargeMotion(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)

	// G jumps to the last line; the departure (line 0) lands in the history
	// through the editor's EventJump seam (#219).
	m = drainKey(m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	m = m.atPosition(t, files[0], 9)
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[0], 0)
	tm, _ = m.Update(NavForwardMsg{})
	m = tm.(Model).atPosition(t, files[0], 9)

	// Small motions record nothing: j moves without touching the history,
	// so another back still returns to the G departure, not line 9→10.
	m = drainKey(m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	tm, _ = m.Update(NavBackMsg{})
	tm.(Model).atPosition(t, files[0], 0)
}
