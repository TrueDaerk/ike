package undotree

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/history"
)

// nodesFixture is a small tree: root -> 1 -> 2, plus 3 branching off 1.
// Seq 3 is current (the divergent edit), seq 2 the abandoned branch.
func nodesFixture() []history.NodeInfo {
	at := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	return []history.NodeInfo{
		{Seq: 0, Parent: -1},
		{Seq: 1, Parent: 0, At: at, Preview: `+"b"`, Edits: 1, Saved: true},
		{Seq: 2, Parent: 1, At: at, Preview: `+"c"`, Edits: 1},
		{Seq: 3, Parent: 1, At: at, Preview: `+"X"`, Edits: 1, Current: true},
	}
}

func key(s string) tea.KeyPressMsg {
	if len(s) == 1 {
		r := rune(s[0])
		return tea.KeyPressMsg{Text: s, Code: r}
	}
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	panic("unknown key " + s)
}

func TestLayoutNewestFirstWithBranchIndent(t *testing.T) {
	rows := layout(nodesFixture())
	if len(rows) != 4 {
		t.Fatalf("layout has %d rows, want 4", len(rows))
	}
	// Newest first, root last.
	if rows[0].node.Seq != 3 || rows[len(rows)-1].node.Seq != 0 {
		t.Fatalf("row order = %d..%d, want 3..0", rows[0].node.Seq, rows[len(rows)-1].node.Seq)
	}
	// The main line (root, 1, 3) stays at depth 0; the abandoned sibling
	// (seq 2, older branch) is indented.
	for _, r := range rows {
		want := 0
		if r.node.Seq == 2 {
			want = 1
		}
		if r.depth != want {
			t.Errorf("seq %d at depth %d, want %d", r.node.Seq, r.depth, want)
		}
	}
}

func TestOpenSelectsCurrentAndEnterJumps(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	m.Open(nodesFixture())
	if !m.IsOpen() {
		t.Fatal("overlay should be open")
	}
	// Current (seq 3) is the first row, so the selection starts there;
	// enter on it still emits a jump (the editor treats it as a no-op).
	m.Update(key("j")) // move to seq 2
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter should emit a jump")
	}
	jump, ok := cmd().(JumpMsg)
	if !ok {
		t.Fatalf("enter emitted %T, want JumpMsg", cmd())
	}
	if jump.Seq != 2 {
		t.Errorf("jump seq = %d, want 2", jump.Seq)
	}
	if !m.IsOpen() {
		t.Error("overlay stays open after a jump")
	}
	m.Update(key("esc"))
	if m.IsOpen() {
		t.Error("esc should close the overlay")
	}
}

func TestViewMarksCurrentAndSaved(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	m.Open(nodesFixture())
	v := m.View()
	if !strings.Contains(v, "●") {
		t.Error("view should mark the current state")
	}
	if !strings.Contains(v, "[saved]") {
		t.Error("view should mark the saved state")
	}
	if !strings.Contains(v, "(original)") {
		t.Error("view should label the root state")
	}
	if !strings.Contains(v, "4 states") {
		t.Error("view should count the states")
	}
}
