package menu

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// testInfo marks "blocked.*" ids disabled with a hint and gives "editor.write"
// a shortcut.
func testInfo(id string) Info {
	if strings.HasPrefix(id, "blocked.") {
		return Info{Hint: "lands with #99"}
	}
	info := Info{Runnable: true}
	if id == "editor.write" {
		info.Shortcut = "cmd+s"
	}
	return info
}

func testMenus() []Menu {
	return []Menu{
		{Title: "File", Items: []Item{
			{Title: "Save", Command: "editor.write"},
			{Title: "Future", Command: "blocked.future"},
			{Title: "Close", Command: "editor.closeTab"},
		}},
		{Title: "Edit", Items: []Item{
			{Title: "Undo", Command: "editor.undo"},
		}},
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Text: s, Code: rune(s[0])}
}

func TestEnterDispatchesSelectedCommand(t *testing.T) {
	m := New(testMenus(), testInfo)
	m.Toggle()
	if !m.IsOpen() {
		t.Fatal("toggle must open the first menu")
	}
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a runnable entry must dispatch")
	}
	run, ok := cmd().(RunMsg)
	if !ok || run.Command != "editor.write" {
		t.Fatalf("expected RunMsg{editor.write}, got %#v", cmd())
	}
	if m.IsOpen() {
		t.Fatal("invoking an entry must close the menu")
	}
}

func TestNavigationSkipsDisabledAndWraps(t *testing.T) {
	m := New(testMenus(), testInfo)
	m.Toggle()
	m.Update(key("down")) // Save -> skips blocked Future -> Close
	cmd := m.Update(key("enter"))
	if run := cmd().(RunMsg); run.Command != "editor.closeTab" {
		t.Fatalf("down must skip the disabled entry, got %s", run.Command)
	}
	m.Toggle()
	m.Update(key("up")) // wraps from Save upward, skipping Future -> Close
	cmd = m.Update(key("enter"))
	if run := cmd().(RunMsg); run.Command != "editor.closeTab" {
		t.Fatalf("up must wrap and skip disabled, got %s", run.Command)
	}
}

func TestLeftRightSwitchMenusAndEscCloses(t *testing.T) {
	m := New(testMenus(), testInfo)
	m.Toggle()
	m.Update(key("right"))
	cmd := m.Update(key("enter"))
	if run := cmd().(RunMsg); run.Command != "editor.undo" {
		t.Fatalf("right must switch to Edit, got %s", run.Command)
	}
	m.Toggle()
	m.Update(key("left")) // wraps to Edit
	cmd = m.Update(key("enter"))
	if run := cmd().(RunMsg); run.Command != "editor.undo" {
		t.Fatalf("left must wrap to Edit, got %s", run.Command)
	}
	m.Toggle()
	m.Update(key("esc"))
	if m.IsOpen() {
		t.Fatal("esc must close the dropdown")
	}
}

func TestDropdownRendersShortcutAndDisabledHint(t *testing.T) {
	m := New(testMenus(), testInfo)
	m.SetWidth(80)
	m.Toggle()
	v := m.Dropdown()
	if !strings.Contains(v, "cmd+s") {
		t.Fatalf("shortcut missing from dropdown:\n%s", v)
	}
	if !strings.Contains(v, "lands with #99") {
		t.Fatalf("disabled entry must show its dependency hint:\n%s", v)
	}
	bar := m.Bar()
	if !strings.Contains(bar, "File") || !strings.Contains(bar, "Edit") {
		t.Fatalf("bar missing titles: %q", bar)
	}
}

func TestMouseHitTesting(t *testing.T) {
	m := New(testMenus(), testInfo)
	m.SetWidth(80)
	// " File  Edit " — column 1 hits File, column 7 hits Edit.
	if i, ok := m.TitleAt(1); !ok || i != 0 {
		t.Fatalf("TitleAt(1) = %d,%v", i, ok)
	}
	if i, ok := m.TitleAt(7); !ok || i != 1 {
		t.Fatalf("TitleAt(7) = %d,%v", i, ok)
	}
	if _, ok := m.TitleAt(60); ok {
		t.Fatal("TitleAt far right must miss")
	}
	m.OpenMenu(0)
	// Row 1 is the first entry; x within the dropdown width.
	if idx, ok := m.ItemAt(m.DropdownX()+1, 1); !ok || idx != 0 {
		t.Fatalf("ItemAt first row = %d,%v", idx, ok)
	}
	if _, ok := m.ItemAt(m.DropdownX()+1, 10); ok {
		t.Fatal("ItemAt below the dropdown must miss")
	}
	// Enter on a disabled row via Invoke is a no-op.
	if cmd := m.Invoke(1); cmd != nil {
		t.Fatal("invoking a disabled entry must be a no-op")
	}
}
