package usages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

func ref(path string, line, col int, preview string) ilsp.Reference {
	return ilsp.Reference{Path: path, Line: line, Col: col, Preview: preview}
}

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

// filled builds a pane holding refs across two files in server order.
func filled() Model {
	m := New(nil)
	m.SetSize(80, 12)
	m.Set("Foo", []ilsp.Reference{
		ref("/proj/a.go", 3, 1, "Foo()"),
		ref("/proj/a.go", 9, 4, "x := Foo"),
		ref("/proj/b.go", 0, 0, "Foo{}"),
	}, nil)
	return m
}

func TestSetGroupsByFileInServerOrder(t *testing.T) {
	m := filled()
	if m.Rows() != 5 {
		t.Fatalf("rows = %d, want 5 (2 headers + 3 refs)", m.Rows())
	}
	r := m.rows
	if !r[0].header || r[0].path != "/proj/a.go" {
		t.Fatalf("row 0 = %+v, want header for the first-seen file", r[0])
	}
	if r[1].ref.Line != 3 || r[2].ref.Line != 9 {
		t.Fatalf("in-file server order broken: %d, %d", r[1].ref.Line, r[2].ref.Line)
	}
	if !r[3].header || r[3].path != "/proj/b.go" {
		t.Fatalf("row 3 = %+v, want second file header", r[3])
	}
	if m.Count() != 3 || m.Files() != 2 || m.Symbol() != "Foo" {
		t.Fatalf("totals = %d in %d files, symbol %q", m.Count(), m.Files(), m.Symbol())
	}
	// The cursor starts on the first reference, not its header.
	if m.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", m.Cursor())
	}
}

func TestTitleCarriesSymbolAndTotals(t *testing.T) {
	m := filled()
	if got := m.Title(); got != "Usages: Foo — 3 in 2 files" {
		t.Fatalf("title = %q", got)
	}
	m.Set("Bar", []ilsp.Reference{ref("/a.go", 0, 0, "Bar")}, nil)
	if got := m.Title(); got != "Usages: Bar — 1 in 1 file" {
		t.Fatalf("singular title = %q", got)
	}
	empty := New(nil)
	if got := empty.Title(); got != "Usages" {
		t.Fatalf("unfilled title = %q", got)
	}
}

func TestEnterDispatchesDefinitionMsg(t *testing.T) {
	m := filled()
	// Cursor starts on the first reference.
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter must dispatch")
	}
	msg, ok := cmd().(ilsp.DefinitionMsg)
	if !ok || msg.Path != "/proj/a.go" || msg.Line != 3 || msg.Col != 1 {
		t.Fatalf("msg = %#v", msg)
	}
	// Enter on a file header opens its first reference.
	m.Update(key("g"))
	cmd = m.Update(key("enter"))
	msg, ok = cmd().(ilsp.DefinitionMsg)
	if !ok || msg.Line != 3 {
		t.Fatalf("header enter msg = %#v", msg)
	}
}

func TestNavigationKeys(t *testing.T) {
	m := filled()
	m.Update(key("j"))
	m.Update(key("j"))
	if m.Cursor() != 3 {
		t.Fatalf("cursor = %d, want 3", m.Cursor())
	}
	m.Update(key("k"))
	if m.Cursor() != 2 {
		t.Fatalf("cursor = %d, want 2", m.Cursor())
	}
	m.Update(key("G"))
	if m.Cursor() != 4 {
		t.Fatalf("G cursor = %d, want last", m.Cursor())
	}
	m.Update(key("g"))
	if m.Cursor() != 0 {
		t.Fatalf("g cursor = %d, want 0", m.Cursor())
	}
}

func TestRefreshKeyRunsStoredContinuation(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	type refreshed struct{}
	ran := tea.Cmd(func() tea.Msg { return refreshed{} })
	m.Set("Foo", nil, ran)
	cmd := m.Update(key("r"))
	if cmd == nil {
		t.Fatal("r must return the stored refresh command")
	}
	if _, ok := cmd().(refreshed); !ok {
		t.Fatal("r must run the continuation the result carried")
	}
	// Without a stored continuation, r is a no-op.
	m.Set("Foo", nil, nil)
	if cmd := m.Update(key("r")); cmd != nil {
		t.Fatal("r without a continuation must be a no-op")
	}
}

func TestViewRendersRowsAndFooter(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	m.SetDisplayPath(func(p string) string { return strings.TrimPrefix(p, "/proj/") })
	m.Set("Foo", []ilsp.Reference{ref("/proj/a.go", 12, 4, "x := Foo(1)")}, nil)
	v := m.View()
	if !strings.Contains(v, "a.go") {
		t.Fatalf("view misses the file header:\n%s", v)
	}
	// line:col are 1-based on screen; the preview follows.
	if !strings.Contains(v, "13:5") || !strings.Contains(v, "x := Foo(1)") {
		t.Fatalf("reference row wrong:\n%s", v)
	}
	if !strings.Contains(v, "Usages: Foo") || !strings.Contains(v, "1 in 1 file") {
		t.Fatalf("header title missing:\n%s", v)
	}
	if !strings.Contains(v, "r refresh") {
		t.Fatalf("footer must document the refresh key:\n%s", v)
	}
}

func TestViewEmptyStates(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 8)
	if v := m.View(); !strings.Contains(v, "no search yet") {
		t.Fatalf("pre-search empty view:\n%s", v)
	}
	m.Set("Foo", nil, nil)
	if v := m.View(); !strings.Contains(v, "(no usages found)") {
		t.Fatalf("found-nothing view:\n%s", v)
	}
}
