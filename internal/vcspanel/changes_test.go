package vcspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/vcs"
)

func snapWith(entries ...vcs.FileEntry) *vcs.Snapshot {
	return &vcs.Snapshot{Root: "/r", Branch: "main", Entries: entries}
}

func changesPanel() Model {
	m := New(nil)
	m.SetSize(90, 14)
	m.SetFocused(true)
	m.SetVCS(snapWith(
		vcs.FileEntry{Path: "a.go", Status: vcs.StatusModified, X: '.', Y: 'M'},
		vcs.FileEntry{Path: "b.go", Status: vcs.StatusAdded, X: 'A', Y: '.'},
	))
	return m
}

func ctrlS() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl} }

func TestChangesRowsFromSnapshot(t *testing.T) {
	m := changesPanel()
	if len(m.chRows) != 2 || m.chRows[0].Path != "a.go" || m.chRows[0].Staged || !m.chRows[1].Staged {
		t.Fatalf("rows = %+v", m.chRows)
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{"[ ] M a.go", "[x] A b.go", "space stage"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
	// A refresh keeps the cursor on the same path.
	m.Update(key("j"))
	m.SetVCS(snapWith(
		vcs.FileEntry{Path: "0.go", Status: vcs.StatusUntracked, X: '?', Y: '?'},
		vcs.FileEntry{Path: "a.go", Status: vcs.StatusModified, X: '.', Y: 'M'},
		vcs.FileEntry{Path: "b.go", Status: vcs.StatusAdded, X: 'A', Y: '.'},
	))
	if m.chRows[m.chCursor].Path != "b.go" {
		t.Fatalf("cursor drifted to %q", m.chRows[m.chCursor].Path)
	}
}

func TestChangesToggleAndDiff(t *testing.T) {
	m := changesPanel()
	cmd := m.Update(key(" "))
	if cmd == nil {
		t.Fatal("space emitted nothing")
	}
	tgl, ok := cmd().(ToggleMsg)
	if !ok || tgl.Path != "a.go" || !tgl.Stage || !m.chRows[0].Staged {
		t.Fatalf("toggle = %#v staged=%v", tgl, m.chRows[0].Staged)
	}
	cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if od, ok := cmd().(OpenDiffMsg); !ok || od.Path != "a.go" {
		t.Fatalf("enter = %#v", cmd())
	}
}

func TestChangesCommitFlow(t *testing.T) {
	m := changesPanel()
	// b.go staged, message empty: hint.
	if h, ok := m.Update(ctrlS())().(HintMsg); !ok || !strings.Contains(h.Text, "message is empty") {
		t.Fatalf("hint = %#v", h)
	}
	// c focuses the message; typed keys land in the shared draft, digits too.
	m.Update(key("c"))
	if !m.msgFocus {
		t.Fatal("c must focus the message")
	}
	for _, r := range "fix 12" {
		m.Update(key(string(r)))
	}
	if m.draft.Text != "fix 12" || m.ActiveTab() != TabChanges {
		t.Fatalf("draft = %q tab = %v", m.draft.Text, m.ActiveTab())
	}
	sub, ok := m.Update(ctrlS())().(SubmitMsg)
	if !ok || sub.Message != "fix 12" {
		t.Fatalf("submit = %#v", sub)
	}
	// esc returns to the list.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.msgFocus {
		t.Fatal("esc must leave the message field")
	}
}

func TestChangesSharedDraft(t *testing.T) {
	m := changesPanel()
	shared := &vcs.MessageDraft{Text: "wip", Pos: 3}
	m.SetDraft(shared)
	m.Update(key("c"))
	m.Update(key("!"))
	if shared.Text != "wip!" {
		t.Fatalf("shared draft = %q", shared.Text)
	}
	shared.Clear()
	if m.draft.Text != "" {
		t.Fatal("clear must reach the panel")
	}
}
