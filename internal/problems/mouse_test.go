package problems

import (
	"testing"
	"time"

	ilsp "ike/internal/lsp"
)

func mouseModel(t *testing.T) (*Model, *time.Time) {
	t.Helper()
	s := NewStore()
	s.Set("/a.go", []ilsp.Diagnostic{
		diag(1, 0, 1, "one", ""),
		diag(2, 0, 1, "two", ""),
		diag(3, 0, 1, "three", ""),
	})
	m := New(nil)
	m.SetSize(80, 5) // body height 3
	m.SetStore(s)
	at := time.Unix(0, 0)
	m.now = func() time.Time { return at }
	return &m, &at
}

func TestClickSelectsDoubleClickActivates(t *testing.T) {
	m, at := mouseModel(t)
	// y 1 is the first row (the file header); y 2 the first diagnostic.
	if cmd := m.Click(4, 2); cmd != nil {
		t.Fatal("single click must only select")
	}
	if m.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", m.Cursor())
	}
	*at = at.Add(100 * time.Millisecond)
	cmd := m.Click(4, 2)
	if cmd == nil {
		t.Fatal("double click must activate")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok || msg.Path != "/a.go" || msg.Line != 1 {
		t.Fatalf("msg = %#v", msg)
	}
	// A slow second click on another run is a fresh selection.
	*at = at.Add(time.Second)
	if cmd := m.Click(4, 2); cmd != nil {
		t.Fatal("slow click must not activate")
	}
}

func TestClickOutsideRowsIgnored(t *testing.T) {
	m, _ := mouseModel(t)
	if cmd := m.Click(0, 0); cmd != nil || m.Cursor() != 0 {
		t.Fatal("header-line click must be a no-op")
	}
	if cmd := m.Click(0, 4); cmd != nil {
		t.Fatal("footer click must be a no-op")
	}
}

func TestWheelScrollsAndDragsCursor(t *testing.T) {
	m, _ := mouseModel(t)
	m.Wheel(2)
	if m.top != 2 {
		t.Fatalf("top = %d, want 2", m.top)
	}
	if m.Cursor() < m.top {
		t.Fatalf("cursor %d left above the window (top %d)", m.Cursor(), m.top)
	}
	m.Wheel(-10)
	if m.top != 0 {
		t.Fatalf("top = %d, want clamped to 0", m.top)
	}
}
