package commitui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/vcs"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func rows() []Row {
	return []Row{
		{Path: "a.go", Status: vcs.StatusModified, Staged: false},
		{Path: "b.go", Status: vcs.StatusAdded, Staged: true},
	}
}

func TestToggleEmitsStageAndFlipsOptimistically(t *testing.T) {
	m := New()
	m.Open(rows())
	cmd := m.Update(key("space"))
	if cmd == nil {
		t.Fatal("toggle emitted nothing")
	}
	msg, ok := cmd().(ToggleMsg)
	if !ok || msg.Path != "a.go" || !msg.Stage {
		t.Fatalf("toggle = %#v", msg)
	}
	if !m.rows[0].Staged {
		t.Fatal("optimistic flip missing")
	}
	// Toggling a staged file unstages; a partial file re-stages the rest.
	m.SetRows([]Row{{Path: "a.go", Staged: true, Partial: true}})
	msg = m.Update(key("space"))().(ToggleMsg)
	if !msg.Stage {
		t.Fatal("partial row must stage the remainder, not unstage")
	}
	m.SetRows([]Row{{Path: "a.go", Staged: true}})
	msg = m.Update(key("space"))().(ToggleMsg)
	if msg.Stage {
		t.Fatal("staged row must unstage")
	}
}

func TestCommitGatedOnStagedAndMessage(t *testing.T) {
	m := New()
	m.Open([]Row{{Path: "a.go"}}) // nothing staged
	if msg, ok := m.Update(key("ctrl+s"))().(HintMsg); !ok || !strings.Contains(msg.Text, "nothing staged") {
		t.Fatalf("no-staged hint = %#v", msg)
	}
	m.SetRows([]Row{{Path: "a.go", Staged: true}})
	if msg, ok := m.Update(key("ctrl+s"))().(HintMsg); !ok || !strings.Contains(msg.Text, "message is empty") {
		t.Fatalf("empty-message hint = %#v", msg)
	}
	m.Update(key("tab")) // focus message
	for _, r := range "fix: x" {
		m.Update(key(string(r)))
	}
	msg, ok := m.Update(key("ctrl+s"))().(SubmitMsg)
	if !ok || msg.Message != "fix: x" {
		t.Fatalf("submit = %#v", msg)
	}
}

func TestMessageSurvivesCloseAndReopen(t *testing.T) {
	m := New()
	m.Open(rows())
	m.Update(key("tab"))
	for _, r := range "wip" {
		m.Update(key(string(r)))
	}
	m.Update(key("esc"))
	if m.IsOpen() {
		t.Fatal("esc must close")
	}
	m.Open(rows())
	if m.Message() != "wip" {
		t.Fatalf("message lost on reopen: %q", m.Message())
	}
	m.ClearMessage()
	if m.Message() != "" {
		t.Fatal("ClearMessage failed")
	}
}

func TestMessageEditing(t *testing.T) {
	m := New()
	m.Open(rows())
	m.Update(key("tab"))
	for _, r := range "ab" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	m.Update(key("c"))
	if m.Message() != "ab\nc" {
		t.Fatalf("message = %q", m.Message())
	}
	m.Update(key("backspace"))
	if m.Message() != "ab\n" {
		t.Fatalf("after backspace = %q", m.Message())
	}
}

func TestSetRowsKeepsCursorOnPath(t *testing.T) {
	m := New()
	m.Open(rows())
	m.Update(key("down"))
	if m.cursor != 1 {
		t.Fatal("setup: cursor should be on b.go")
	}
	m.SetRows([]Row{{Path: "0.go"}, {Path: "a.go"}, {Path: "b.go"}})
	if m.cursor != 2 || m.rows[m.cursor].Path != "b.go" {
		t.Fatalf("cursor drifted: %d", m.cursor)
	}
}

func TestViewShowsHintAndList(t *testing.T) {
	m := New()
	m.SetSize(100, 40)
	m.Open([]Row{{Path: "a.go", Status: vcs.StatusModified}})
	v := m.View()
	for _, want := range []string{"Commit Changes", "a.go", "nothing staged"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
}
