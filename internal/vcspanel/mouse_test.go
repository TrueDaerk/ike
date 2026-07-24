package vcspanel

import (
	"testing"
	"time"

	"ike/internal/vcs"
)

func TestClickSelectsAndDoubleClickOpensDiff(t *testing.T) {
	m := changesPanel() // rows: a.go, b.go
	// Click the second row: selects only.
	if cmd := m.Click(10, 2); cmd != nil || m.chCursor != 1 {
		t.Fatalf("select click: cursor=%d cmd=%v", m.chCursor, cmd)
	}
	// Click the selected row again: opens the diff.
	cmd := m.Click(10, 2)
	if od, ok := cmd().(OpenDiffMsg); !ok || od.Path != "b.go" {
		t.Fatalf("activate click = %#v", cmd())
	}
	// Header clicks are inert.
	if cmd := m.Click(2, 0); cmd != nil {
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
}

func TestWheelScrollsList(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 6)
	var entries []vcs.FileEntry
	for i := 0; i < 40; i++ {
		entries = append(entries, vcs.FileEntry{
			Path: string(rune('a'+i%26)) + string(rune('0'+i/26)) + ".go", Status: vcs.StatusModified,
		})
	}
	m.SetVCS(&vcs.Snapshot{Root: "/r", Branch: "main", Entries: entries})
	m.Wheel(5)
	if m.chTop != 5 {
		t.Fatalf("chTop = %d", m.chTop)
	}
	m.Wheel(-99)
	if m.chTop != 0 {
		t.Fatalf("chTop after clamp = %d", m.chTop)
	}
}
