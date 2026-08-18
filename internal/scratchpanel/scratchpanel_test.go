package scratchpanel

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/scratch"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// fixed pins the panel's clock so RelTime and the double-click window are
// deterministic.
var fixed = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

// panel returns a focused, sized panel over the given scratch entries.
func panel(t *testing.T, entries ...scratch.Entry) *Model {
	t.Helper()
	m := New(nil)
	m.now = func() time.Time { return fixed }
	m.SetSize(60, 6)
	m.SetFocused(true)
	m.SetLister(func() ([]scratch.Entry, error) { return entries, nil })
	return &m
}

func entry(path string, ago time.Duration) scratch.Entry {
	return scratch.Entry{Path: path, ModTime: fixed.Add(-ago)}
}

// run feeds one key and returns the message its command produced, if any.
func run(m *Model, k string) tea.Msg {
	cmd := m.Update(key(k))
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestRefreshListsTheStore(t *testing.T) {
	m := panel(t, entry("/s/scratch-2.py", time.Minute), entry("/s/scratch-1.txt", time.Hour))
	if got := m.Entries(); len(got) != 2 || got[0].Path != "/s/scratch-2.py" {
		t.Fatalf("entries = %v", got)
	}
	view := m.View()
	// The language column falls back to the bare extension for files no
	// registered language claims — no language plugin is loaded here.
	for _, want := range []string{"scratch-2.py", "py ", "scratch-1.txt", "1h ago", "enter open"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view must mention %q:\n%s", want, view)
		}
	}
}

// A store error renders as the empty hint rather than an empty pane.
func TestRefreshShowsListerError(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 4)
	m.SetLister(func() ([]scratch.Entry, error) { return nil, errors.New("boom") })
	if !strings.Contains(m.View(), "boom") {
		t.Fatalf("view must show the error:\n%s", m.View())
	}
}

func TestEnterOpensTheSelectedScratch(t *testing.T) {
	m := panel(t, entry("/s/a.txt", time.Minute), entry("/s/b.txt", time.Hour))
	run(m, "down")
	msg, ok := run(m, "enter").(OpenMsg)
	if !ok || msg.Path != "/s/b.txt" {
		t.Fatalf("enter = %#v, want OpenMsg for b.txt", msg)
	}
}

// Delete asks first: 'd' only arms the prompt, 'y' emits the delete.
func TestDeleteConfirms(t *testing.T) {
	m := panel(t, entry("/s/a.txt", time.Minute))
	if msg := run(m, "d"); msg != nil {
		t.Fatalf("d must not delete on its own, got %#v", msg)
	}
	if m.Confirming() != "/s/a.txt" {
		t.Fatalf("confirm = %q, want the selected path", m.Confirming())
	}
	if !strings.Contains(m.View(), "Delete a.txt?") {
		t.Fatalf("view must show the confirmation:\n%s", m.View())
	}
	msg, ok := run(m, "y").(DeleteMsg)
	if !ok || msg.Path != "/s/a.txt" {
		t.Fatalf("y = %#v, want DeleteMsg for a.txt", msg)
	}
	if m.Confirming() != "" {
		t.Fatal("the confirmation must clear once answered")
	}
}

func TestDeleteCancels(t *testing.T) {
	m := panel(t, entry("/s/a.txt", time.Minute), entry("/s/b.txt", time.Hour))
	run(m, "d")
	// Any other key cancels — and is swallowed, so it does not also navigate.
	if msg := run(m, "j"); msg != nil {
		t.Fatalf("a cancelling key must produce nothing, got %#v", msg)
	}
	if m.Confirming() != "" || m.Cursor() != 0 {
		t.Fatalf("cancel must clear the prompt and swallow the key: %q %d", m.Confirming(), m.Cursor())
	}
	// Losing focus drops a live prompt too.
	run(m, "d")
	m.SetFocused(false)
	if m.Confirming() != "" {
		t.Fatal("an unfocused panel must not keep a live confirmation")
	}
}

// The list is re-read on demand and keeps the cursor on the same file; a
// vanished file drops the pending confirmation with it.
func TestRefreshKeepsSelection(t *testing.T) {
	entries := []scratch.Entry{entry("/s/a.txt", time.Minute), entry("/s/b.txt", time.Hour)}
	model := New(nil)
	m := &model
	m.now = func() time.Time { return fixed }
	m.SetSize(60, 6)
	m.SetFocused(true)
	m.SetLister(func() ([]scratch.Entry, error) { return entries, nil })

	run(m, "down")
	// A newly created scratch appears on top without moving the selection.
	entries = append([]scratch.Entry{entry("/s/c.go", 0)}, entries...)
	m.Refresh()
	if sel, _ := m.Selected(); sel.Path != "/s/b.txt" {
		t.Fatalf("selection = %q, want b.txt", sel.Path)
	}
	run(m, "d")
	entries = entries[:2] // b.txt is gone
	m.Refresh()
	if m.Confirming() != "" {
		t.Fatal("a confirmation for a vanished row must clear")
	}
	if len(m.Entries()) != 2 {
		t.Fatalf("entries = %v, want the two survivors", m.Entries())
	}
}

func TestClickSelectsAndDoubleClickOpens(t *testing.T) {
	m := panel(t, entry("/s/a.txt", time.Minute), entry("/s/b.txt", time.Hour))
	now := fixed
	m.now = func() time.Time { return now }

	if cmd := m.Click(2, 1); cmd != nil {
		t.Fatal("a first click only selects")
	}
	if m.Cursor() != 1 {
		t.Fatalf("cursor = %d, want the clicked row", m.Cursor())
	}
	now = now.Add(50 * time.Millisecond)
	cmd := m.Click(2, 1)
	if cmd == nil {
		t.Fatal("a double click must open the row")
	}
	if msg, ok := cmd().(OpenMsg); !ok || msg.Path != "/s/b.txt" {
		t.Fatalf("double click = %#v, want OpenMsg for b.txt", msg)
	}
	// A click past the last row does nothing.
	if cmd := m.Click(2, 4); cmd != nil {
		t.Fatal("a click below the list must do nothing")
	}
}

func TestWheelScrollsWithinTheList(t *testing.T) {
	var entries []scratch.Entry
	for i := 0; i < 10; i++ {
		entries = append(entries, entry("/s/x"+string(rune('0'+i))+".txt", time.Duration(i)*time.Minute))
	}
	m := panel(t, entries...)
	m.Wheel(9)
	if m.Cursor() < 3 {
		t.Fatalf("the wheel must drag the cursor along, got %d", m.Cursor())
	}
	if strings.Contains(m.View(), "x0.txt") {
		t.Fatalf("scrolled past the head, yet it still renders:\n%s", m.View())
	}
	m.Wheel(-100)
	if !strings.Contains(m.View(), "x0.txt") {
		t.Fatalf("scrolling back must clamp to the first row:\n%s", m.View())
	}
}
