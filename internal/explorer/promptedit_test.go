package explorer

// promptedit_test.go guards the #2002 sweep for the explorer's name prompt:
// it hand-rolled arrows, home/end, backspace and delete and now routes
// through ui.EditKey, so word motions, the word/line kills and the macOS
// opt/cmd chords work — while the #1047 preselected stem still behaves.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func renamePrompt(t *testing.T) Model {
	t.Helper()
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	if m.prompt == nil {
		t.Fatal("rename must open the prompt")
	}
	return m
}

func TestPromptWordAndLineKills(t *testing.T) {
	m := renamePrompt(t)
	// Drop the preselection first: an arrow keeps the text (#1047).
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if m.prompt.input.Text != "a." {
		t.Fatalf("alt+backspace: %q", m.prompt.input.Text)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper})
	if m.prompt.input.Text != "" || m.prompt.input.Cur != 0 {
		t.Fatalf("super+backspace: %q/%d", m.prompt.input.Text, m.prompt.input.Cur)
	}
}

func TestPromptWordMotion(t *testing.T) {
	m := renamePrompt(t)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd}) // "a.txt", cursor 5
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	if m.prompt.input.Cur != 2 {
		t.Fatalf("alt+left: cursor = %d, want 2", m.prompt.input.Cur)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper})
	if m.prompt.input.Cur != 0 {
		t.Fatalf("super+left: cursor = %d, want 0", m.prompt.input.Cur)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper})
	if m.prompt.input.Cur != 5 {
		t.Fatalf("super+right: cursor = %d, want 5", m.prompt.input.Cur)
	}
}

// The preselected stem is still replaced wholesale by the first typed rune —
// now with the insertion itself done by the shared editor.
func TestPromptPreselectStillReplacedOnType(t *testing.T) {
	m := renamePrompt(t)
	if m.prompt.selStart != 0 || m.prompt.selEnd != 1 {
		t.Fatalf("selection = [%d,%d), want the stem", m.prompt.selStart, m.prompt.selEnd)
	}
	m, _ = send(m, key("zz"))
	if m.prompt.input.Text != "zz.txt" || m.prompt.input.Cur != 2 {
		t.Fatalf("typing over the stem: %q/%d", m.prompt.input.Text, m.prompt.input.Cur)
	}
}

// A word kill with the stem preselected drops the selection first, then kills
// the word before the cursor — the text is never silently doubled.
func TestPromptWordKillWithPreselection(t *testing.T) {
	m := renamePrompt(t)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if m.prompt.selStart != m.prompt.selEnd {
		t.Fatalf("selection must be dropped, got [%d,%d)", m.prompt.selStart, m.prompt.selEnd)
	}
	if m.prompt.input.Text != ".txt" {
		t.Fatalf("alt+backspace over the stem: %q", m.prompt.input.Text)
	}
}
