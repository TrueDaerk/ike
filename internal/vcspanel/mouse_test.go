package vcspanel

import (
	"testing"
	"time"

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

func TestSlowSecondClickOnlySelects(t *testing.T) {
	m := changesPanel()
	clock := time.Now()
	m.now = func() time.Time { return clock }

	if cmd := m.Click(10, 1); cmd != nil {
		t.Fatal("first click must only select")
	}
	// Second click outside the window: still just a selection.
	clock = clock.Add(doubleClickWindow + time.Millisecond)
	if cmd := m.Click(10, 1); cmd != nil {
		t.Fatal("slow second click must not open the diff")
	}
	// Third click inside the window completes the double-click.
	clock = clock.Add(100 * time.Millisecond)
	cmd := m.Click(10, 1)
	if cmd == nil {
		t.Fatal("fast second click must activate")
	}
	if od, ok := cmd().(OpenDiffMsg); !ok || od.Path != "a.go" {
		t.Fatalf("activate = %#v", cmd())
	}

	// Log rows use the same window.
	m.Click(16, 0) // switch to Log (resets nothing relevant)
	m.ApplyLog(vcs.LogMsg{Entries: entries("one")})
	clock = clock.Add(time.Second)
	if cmd := m.Click(5, 2); cmd != nil {
		t.Fatal("log first click must only select")
	}
	clock = clock.Add(doubleClickWindow * 2)
	if cmd := m.Click(5, 2); cmd != nil {
		t.Fatal("slow log second click must not activate")
	}
	clock = clock.Add(50 * time.Millisecond)
	if cmd := m.Click(5, 2); cmd == nil {
		t.Fatal("fast log second click must activate")
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
