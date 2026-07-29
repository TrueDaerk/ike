package breakpanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/debug"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func store(t *testing.T) *debug.Breakpoints {
	t.Helper()
	b := debug.NewBreakpoints()
	b.Toggle("a.py", 2)
	b.Toggle("a.py", 7)
	b.Toggle("z.go", 0)
	return b
}

// run drains a returned command into its message (commands here are pure
// message thunks).
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	return cmd()
}

func TestRefreshGroupsByFile(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetPreview(func(path string, line int) string { return "src" })
	m.SetStore(store(t))
	// a.py header, 2 rows, z.go header, 1 row.
	if m.Rows() != 5 {
		t.Fatalf("Rows = %d, want 5", m.Rows())
	}
	v := m.View()
	if !strings.Contains(v, "a.py") || !strings.Contains(v, "z.go") {
		t.Fatalf("view must list both files:\n%s", v)
	}
	if !strings.Contains(v, "3 breakpoints") {
		t.Fatalf("header must total the store:\n%s", v)
	}
	if !strings.Contains(v, "src") {
		t.Fatalf("rows must carry the injected preview:\n%s", v)
	}
}

func TestEnterEmitsOpenLocation(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(store(t))
	m.Update(key("down")) // first breakpoint of a.py
	msg := run(t, m.Update(key("enter")))
	got, ok := msg.(OpenLocationMsg)
	if !ok || got.Path != "a.py" || got.Line != 2 {
		t.Fatalf("enter = %#v, want OpenLocationMsg{a.py, 2}", msg)
	}
	// Enter on a file header jumps to its first breakpoint.
	m.Update(key("g"))
	msg = run(t, m.Update(key("enter")))
	if got := msg.(OpenLocationMsg); got.Path != "a.py" || got.Line != 2 {
		t.Fatalf("header enter = %#v", got)
	}
}

func TestSpaceEmitsToggleEnabled(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(store(t))
	m.Update(key("down"))
	m.Update(key("down")) // a.py:7
	msg := run(t, m.Update(key("space")))
	if got := msg.(ToggleEnabledMsg); got.Path != "a.py" || got.Line != 7 {
		t.Fatalf("space = %#v", got)
	}
	// Space on a header is inert.
	m.Update(key("g"))
	if cmd := m.Update(key("space")); cmd != nil {
		t.Fatal("space on a header must be a no-op")
	}
}

func TestDeleteAndDeleteAll(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(store(t))
	m.Update(key("G")) // last row: z.go:0
	msg := run(t, m.Update(key("d")))
	if got := msg.(RemoveMsg); got.Path != "z.go" || got.Line != 0 {
		t.Fatalf("d = %#v", got)
	}
	if _, ok := run(t, m.Update(key("D"))).(RemoveAllMsg); !ok {
		t.Fatal("D must emit RemoveAllMsg")
	}
}

func TestRefreshTracksStoreAndKeepsCursor(t *testing.T) {
	b := store(t)
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(b)
	m.Update(key("down"))
	m.Update(key("down")) // a.py:7
	// A gutter toggle elsewhere: the list re-derives, the cursor stays on
	// the surviving breakpoint.
	b.Toggle("a.py", 2)
	m.Refresh()
	if m.Rows() != 4 {
		t.Fatalf("Rows = %d, want 4 after removal", m.Rows())
	}
	msg := run(t, m.Update(key("enter")))
	if got := msg.(OpenLocationMsg); got.Path != "a.py" || got.Line != 7 {
		t.Fatalf("cursor must stay on a.py:7, got %#v", got)
	}
	b.RemoveAll()
	m.Refresh()
	if m.Rows() != 0 {
		t.Fatalf("Rows = %d, want 0", m.Rows())
	}
	if !strings.Contains(m.View(), "no breakpoints") {
		t.Fatal("empty list must explain itself")
	}
}

func TestDisabledRowRendersHollow(t *testing.T) {
	b := store(t)
	b.SetEnabled("a.py", 2, false)
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(b)
	v := m.View()
	if !strings.Contains(v, "○") || !strings.Contains(v, "●") {
		t.Fatalf("view must mix hollow and filled markers:\n%s", v)
	}
}

func TestClickSelectsDoubleClickJumps(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(store(t))
	at := time.Unix(0, 0)
	m.now = func() time.Time { return at }
	if cmd := m.Click(10, 2); cmd != nil { // first click: select a.py:2
		t.Fatal("single click must only select")
	}
	if m.Cursor() != 1 {
		t.Fatalf("Cursor = %d, want 1", m.Cursor())
	}
	at = at.Add(100 * time.Millisecond)
	msg := run(t, m.Click(10, 2))
	if got := msg.(OpenLocationMsg); got.Path != "a.py" || got.Line != 2 {
		t.Fatalf("double click = %#v", got)
	}
	// A click on the glyph cell flips the enabled state instead.
	at = at.Add(time.Second)
	msg = run(t, m.Click(3, 3))
	if got := msg.(ToggleEnabledMsg); got.Path != "a.py" || got.Line != 7 {
		t.Fatalf("glyph click = %#v", got)
	}
}
