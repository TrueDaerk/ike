package vcspanel

import (
	"testing"

	"ike/internal/vcs"
)

func TestClickSwitchesTabs(t *testing.T) {
	m := changesPanel()
	if cmd := m.Click(16, 0); cmd == nil || m.ActiveTab() != TabLog {
		t.Fatalf("log-label click: tab=%v cmd=%v", m.ActiveTab(), cmd)
	}
	m.Click(2, 0)
	if m.ActiveTab() != TabChanges {
		t.Fatal("changes-label click failed")
	}
}

func TestClickChangesSelectToggleDiff(t *testing.T) {
	m := changesPanel() // rows: a.go (unstaged), b.go (staged)
	// Click the second row: selects only.
	if cmd := m.Click(10, 2); cmd != nil || m.chCursor != 1 {
		t.Fatalf("select click: cursor=%d cmd=%v", m.chCursor, cmd)
	}
	// Click the selected row again: opens the diff.
	cmd := m.Click(10, 2)
	if od, ok := cmd().(OpenDiffMsg); !ok || od.Path != "b.go" {
		t.Fatalf("activate click = %#v", cmd())
	}
	// Checkbox region toggles staging on the clicked row.
	cmd = m.Click(2, 1)
	tgl, ok := cmd().(ToggleMsg)
	if !ok || tgl.Path != "a.go" || !tgl.Stage {
		t.Fatalf("checkbox click = %#v", tgl)
	}
}

func TestClickLogSelectAndActivate(t *testing.T) {
	m := logPanel()
	m.Update(key("2"))
	m.ApplyLog(vcs.LogMsg{Entries: entries("one", "two")})
	// Rows start at y 2 (tab header + column header).
	if cmd := m.Click(5, 3); cmd != nil || m.logCursor != 1 {
		t.Fatalf("select: cursor=%d cmd=%v", m.logCursor, cmd)
	}
	cmd := m.Click(5, 3)
	show, ok := cmd().(ShowRequestMsg)
	if !ok || show.Hash != m.logEntries[1].Hash {
		t.Fatalf("activate = %#v", cmd())
	}
	// Column-header click is inert.
	if cmd := m.Click(5, 1); cmd != nil {
		t.Fatal("header click must be inert")
	}
}

func TestWheelScrollsActiveList(t *testing.T) {
	m := logPanel()
	m.Update(key("2"))
	var subjects []string
	for i := 0; i < 40; i++ {
		subjects = append(subjects, "s")
	}
	m.ApplyLog(vcs.LogMsg{Entries: entries(subjects...)})
	m.Wheel(5)
	if m.logTop != 5 {
		t.Fatalf("logTop = %d", m.logTop)
	}
	m.Wheel(-99)
	if m.logTop != 0 {
		t.Fatalf("logTop after clamp = %d", m.logTop)
	}
}
