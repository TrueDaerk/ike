package explorer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func navModel(t *testing.T, files int) Model {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < files; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%02d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(root)
	m.SetSize(30, 10)
	m.applyScan(scanCmd(root)().(ScanDoneMsg))
	m.SetFocused(true)
	return m
}

func press(m Model, key string) Model {
	var msg tea.KeyPressMsg
	switch key {
	case "pgdown":
		msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		msg = tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "ctrl+d":
		msg = tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+u":
		msg = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	default:
		msg = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
	m, _ = m.Update(msg)
	return m
}

// TestTreePageAndJumpMotions guards #1032: G/gg jump, PageDown/PageUp page,
// ctrl+d/ctrl+u half-page.
func TestTreePageAndJumpMotions(t *testing.T) {
	m := navModel(t, 40)
	m = press(m, "G")
	if m.cursor != len(m.rows)-1 {
		t.Fatalf("G: cursor = %d want %d", m.cursor, len(m.rows)-1)
	}
	m = press(m, "g")
	m = press(m, "g")
	if m.cursor != 0 {
		t.Fatalf("gg: cursor = %d want 0", m.cursor)
	}
	_, textH, _, _, _ := m.viewport()
	m = press(m, "pgdown")
	if m.cursor != textH {
		t.Fatalf("pgdown: cursor = %d want %d", m.cursor, textH)
	}
	m = press(m, "ctrl+d")
	if m.cursor != textH+textH/2 {
		t.Fatalf("ctrl+d: cursor = %d want %d", m.cursor, textH+textH/2)
	}
	m = press(m, "pgup")
	m = press(m, "ctrl+u")
	if m.cursor != 0 {
		t.Fatalf("back up: cursor = %d want 0", m.cursor)
	}
	// A lone g followed by a non-g key must not jump.
	m = press(m, "G")
	bottom := m.cursor
	m = press(m, "g")
	m = press(m, "j") // disarms; j is clamped at bottom
	m = press(m, "g")
	if m.cursor != bottom {
		t.Fatalf("disarmed g must not jump, cursor = %d", m.cursor)
	}
}

// TestExpandAllRecursesThroughLazyLevels guards #1043: expand-all descends
// through unloaded directories via continued scans.
func TestExpandAllRecursesThroughLazyLevels(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "leaf.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	m.SetSize(30, 12)
	m.applyScan(scanCmd(root)().(ScanDoneMsg))
	m.cursor = 0 // root selected → expand everything
	cmd := m.expandAllUnderSelection()
	// Drive the scan/continue loop synchronously: run each pending scan and
	// feed applyScan + continueExpandAll directly (no poll ticks).
	for i := 0; cmd != nil && i < 20; i++ {
		var pending []tea.Cmd
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			pending = msg
		default:
			if sd, ok := msg.(ScanDoneMsg); ok {
				m.applyScan(sd)
				cmd = m.continueExpandAll(sd.Path)
				continue
			}
			cmd = nil
			continue
		}
		cmd = nil
		for _, c := range pending {
			if c == nil {
				continue
			}
			if sd, ok := c().(ScanDoneMsg); ok {
				m.applyScan(sd)
				if next := m.continueExpandAll(sd.Path); next != nil {
					cmd = next
				}
			}
		}
	}
	found := false
	for _, n := range m.rows {
		if strings.HasSuffix(n.path, "leaf.txt") {
			found = true
		}
	}
	if !found {
		t.Fatalf("leaf.txt not visible after expand-all; rows: %d", len(m.rows))
	}
	if m.expandAllRoot != "" {
		t.Fatal("expand-all state must clear when done")
	}
}

// TestClippedRowEndsInEllipsis guards #1035.
func TestClippedRowEndsInEllipsis(t *testing.T) {
	root := t.TempDir()
	long := "this_is_a_very_long_file_name_that_overflows.txt"
	if err := os.WriteFile(filepath.Join(root, long), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	m.SetSize(14, 6)
	m.applyScan(scanCmd(root)().(ScanDoneMsg))
	v := m.View()
	if !strings.Contains(v, "…") {
		t.Fatalf("clipped row must end in an ellipsis:\n%s", v)
	}
}

// TestNavCommandsDispatch guards #1041: the registry-facing navigation
// messages drive the same motions as the raw keys.
func TestNavCommandsDispatch(t *testing.T) {
	m := navModel(t, 30)
	m, _ = m.Update(CursorBottomMsg{})
	if m.cursor != len(m.rows)-1 {
		t.Fatalf("bottom: cursor = %d", m.cursor)
	}
	m, _ = m.Update(CursorTopMsg{})
	if m.cursor != 0 {
		t.Fatalf("top: cursor = %d", m.cursor)
	}
	m, _ = m.Update(CursorMoveMsg{Delta: 3})
	if m.cursor != 3 {
		t.Fatalf("move: cursor = %d", m.cursor)
	}
	_, textH, _, _, _ := m.viewport()
	m, _ = m.Update(CursorPageMsg{Dir: 1})
	if m.cursor != 3+textH {
		t.Fatalf("page: cursor = %d want %d", m.cursor, 3+textH)
	}
	m, _ = m.Update(CollapseOrParentMsg{})
	if m.cursor != 0 {
		t.Fatalf("collapseOrParent from a file must go to the parent, cursor = %d", m.cursor)
	}
}

// TestEmptyDirHidesExpander guards #1039: a loaded empty directory shows no
// caret; an unloaded one keeps it (contents unknown), hidden-only contents
// count as empty until the toggle reveals them.
func TestEmptyDirHidesExpander(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	hid := filepath.Join(root, "hiddenonly")
	if err := os.MkdirAll(hid, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hid, ".secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	m.SetSize(30, 10)
	m.applyScan(scanCmd(root)().(ScanDoneMsg))
	find := func(name string) *node {
		for _, n := range m.rows {
			if n.name == name {
				return n
			}
		}
		t.Fatalf("row %q missing", name)
		return nil
	}
	// Unloaded: caret shows.
	if got := m.marker(find("emptydir")); got != "▸ " {
		t.Fatalf("unloaded dir marker = %q", got)
	}
	// Load both: empty and hidden-only lose the caret.
	m.applyScan(scanCmd(filepath.Join(root, "emptydir"))().(ScanDoneMsg))
	m.applyScan(scanCmd(hid)().(ScanDoneMsg))
	if got := m.marker(find("emptydir")); got != "  " {
		t.Fatalf("loaded empty dir marker = %q want blank", got)
	}
	if got := m.marker(find("hiddenonly")); got != "  " {
		t.Fatalf("hidden-only dir marker = %q want blank", got)
	}
	m, _ = m.Update(ToggleHiddenMsg{})
	if got := m.marker(find("hiddenonly")); got != "▸ " {
		t.Fatalf("with hidden shown the caret must return, got %q", got)
	}
}
