package menu

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	// The letter-jump hint underlines each title's first rune separately, so
	// match on stripped text.
	bar := ansi.Strip(m.Bar())
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
	// Row 1 is the top border, row 2 the first entry; x one column inside the
	// left border.
	if idx, ok := m.ItemAt(m.DropdownX()+2, 2); !ok || idx != 0 {
		t.Fatalf("ItemAt first row = %d,%v", idx, ok)
	}
	if _, ok := m.ItemAt(m.DropdownX()+2, 1); ok {
		t.Fatal("ItemAt on the top border must miss")
	}
	if _, ok := m.ItemAt(m.DropdownX()+2, 10); ok {
		t.Fatal("ItemAt below the dropdown must miss")
	}
	// Enter on a disabled row via Invoke is a no-op.
	if cmd := m.Invoke(1); cmd != nil {
		t.Fatal("invoking a disabled entry must be a no-op")
	}
}

// TestHoverMovesSelection verifies mouse hover selects runnable entries and
// ignores disabled ones.
func TestHoverMovesSelection(t *testing.T) {
	m := New(testMenus(), testInfo)
	m.OpenMenu(0)
	m.Hover(2) // Close
	cmd := m.Update(key("enter"))
	if run := cmd().(RunMsg); run.Command != "editor.closeTab" {
		t.Fatalf("hover must move the selection, got %s", run.Command)
	}
	m.OpenMenu(0)
	m.Hover(1) // blocked.future — disabled, selection stays on Save
	cmd = m.Update(key("enter"))
	if run := cmd().(RunMsg); run.Command != "editor.write" {
		t.Fatalf("hover on a disabled entry must not move the selection, got %s", run.Command)
	}
}

// TestLetterJumpsToMenu verifies a title's first letter opens that menu while
// a dropdown is open, case-insensitively.
func TestLetterJumpsToMenu(t *testing.T) {
	m := New(testMenus(), testInfo)
	m.Toggle() // File
	m.Update(key("e"))
	cmd := m.Update(key("enter"))
	if run := cmd().(RunMsg); run.Command != "editor.undo" {
		t.Fatalf("'e' must jump to Edit, got %s", run.Command)
	}
	m.Toggle()
	m.Update(key("right")) // Edit
	m.Update(key("F"))     // uppercase jumps too
	cmd = m.Update(key("enter"))
	if run := cmd().(RunMsg); run.Command != "editor.write" {
		t.Fatalf("'F' must jump back to File, got %s", run.Command)
	}
	m.Toggle()
	m.Update(key("x")) // no match: stays on File, stays open
	if !m.IsOpen() {
		t.Fatal("an unmatched letter must not close the menu")
	}
	cmd = m.Update(key("enter"))
	if run := cmd().(RunMsg); run.Command != "editor.write" {
		t.Fatalf("unmatched letter must not switch menus, got %s", run.Command)
	}
}

// TestDropdownHasBorder verifies the open dropdown is framed so it separates
// from the content it floats over.
func TestDropdownHasBorder(t *testing.T) {
	m := New(testMenus(), testInfo)
	m.Toggle()
	v := m.Dropdown()
	if !strings.Contains(v, "╭") || !strings.Contains(v, "╰") {
		t.Fatalf("dropdown missing border frame:\n%s", v)
	}
}
