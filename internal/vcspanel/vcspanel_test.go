package vcspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/vcs"
)

func key(s string) tea.KeyPressMsg {
	if s == "tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestTabSwitching(t *testing.T) {
	m := New(nil)
	if m.ActiveTab() != TabChanges {
		t.Fatal("must start on Changes")
	}
	m.Update(key("2"))
	if m.ActiveTab() != TabLog {
		t.Fatal("2 must select Log")
	}
	m.Update(key("1"))
	if m.ActiveTab() != TabChanges {
		t.Fatal("1 must select Changes")
	}
	m.Update(key("tab"))
	m.Update(key("tab"))
	if m.ActiveTab() != TabChanges {
		t.Fatal("tab must cycle back")
	}
}

func TestViewShowsHeaderAndPlaceholder(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "1 Changes") || !strings.Contains(v, "2 Log") {
		t.Fatalf("header missing:\n%s", v)
	}
	if !strings.Contains(v, "not a git repository") {
		t.Fatalf("nil snapshot must show the placeholder:\n%s", v)
	}
	m.SetVCS(&vcs.Snapshot{Root: "/r", Branch: "main"})
	v = ansi.Strip(m.View())
	if !strings.Contains(v, "⎇ main") {
		t.Fatalf("branch missing from header:\n%s", v)
	}
	if strings.Contains(v, "not a git repository") {
		t.Fatal("placeholder must vanish with a snapshot")
	}
}
