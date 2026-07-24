package problems

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
)

func diag(line, col, sev int, msg, code string) ilsp.Diagnostic {
	return ilsp.Diagnostic{
		Range:    buffer.Range{Start: buffer.Position{Line: line, Col: col}},
		Severity: sev,
		Message:  msg,
		Code:     code,
	}
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

func TestStoreSetAndClear(t *testing.T) {
	s := NewStore()
	s.Set("/a.go", []ilsp.Diagnostic{diag(0, 0, 1, "x", "")})
	s.Set("/b.go", []ilsp.Diagnostic{diag(0, 0, 2, "y", "")})
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}
	// An empty publish clears the file out of the store entirely.
	s.Set("/a.go", nil)
	if s.Len() != 1 || s.Get("/a.go") != nil {
		t.Fatalf("cleared path must vanish: len=%d", s.Len())
	}
	if got := s.Paths(); len(got) != 1 || got[0] != "/b.go" {
		t.Fatalf("Paths = %v", got)
	}
}

func TestRefreshGroupsAndSortsErrorsFirst(t *testing.T) {
	s := NewStore()
	// warn.go holds only a warning; err.go an error — err.go must lead even
	// though "e" < "w" would sort it first anyway; use names that would
	// reverse under a plain path sort.
	s.Set("/a-warn.go", []ilsp.Diagnostic{diag(1, 0, 2, "warn", "")})
	s.Set("/z-err.go", []ilsp.Diagnostic{
		diag(9, 0, 2, "late warn", ""),
		diag(5, 2, 1, "boom", "E1"),
		diag(5, 0, 1, "boom2", ""),
	})
	m := New(nil)
	m.SetSize(80, 12)
	m.SetStore(s)

	if m.Rows() != 6 {
		t.Fatalf("rows = %d, want 6 (2 headers + 4 diags)", m.Rows())
	}
	r := m.rows
	if !r[0].header || r[0].path != "/z-err.go" {
		t.Fatalf("row 0 = %+v, want header for the error file first", r[0])
	}
	// Within the file: severity first, then line, then column.
	if r[1].d.Message != "boom2" || r[2].d.Message != "boom" || r[3].d.Message != "late warn" {
		t.Fatalf("in-file order = %q %q %q", r[1].d.Message, r[2].d.Message, r[3].d.Message)
	}
	if !r[4].header || r[4].path != "/a-warn.go" {
		t.Fatalf("row 4 = %+v, want warning file header last", r[4])
	}
}

func TestUnspecifiedSeverityCountsAsError(t *testing.T) {
	s := NewStore()
	s.Set("/a.go", []ilsp.Diagnostic{diag(0, 0, 0, "no sev", "")})
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(s)
	errs, warns := m.visibleCounts()
	if errs != 1 || warns != 0 {
		t.Fatalf("counts = %d/%d, want 1/0", errs, warns)
	}
}

func TestEnterDispatchesOpenLocation(t *testing.T) {
	s := NewStore()
	s.Set("/a.go", []ilsp.Diagnostic{diag(7, 3, 1, "boom", "")})
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(s)

	// Cursor starts on the file header: enter opens the first diagnostic.
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a header must dispatch")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok || msg.Path != "/a.go" || msg.Line != 7 || msg.Col != 3 {
		t.Fatalf("msg = %#v", msg)
	}

	// Down onto the diagnostic row: same target.
	m.Update(key("j"))
	if m.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", m.Cursor())
	}
	cmd = m.Update(key("enter"))
	msg, ok = cmd().(OpenLocationMsg)
	if !ok || msg.Line != 7 || msg.Col != 3 {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestFileOnlyFilterToggle(t *testing.T) {
	s := NewStore()
	s.Set("/a.go", []ilsp.Diagnostic{diag(0, 0, 1, "a", "")})
	s.Set("/b.go", []ilsp.Diagnostic{diag(0, 0, 1, "b", "")})
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(s)
	m.SetActivePath("/b.go")
	if m.Rows() != 4 {
		t.Fatalf("project scope rows = %d, want 4", m.Rows())
	}
	m.Update(key("f"))
	if !m.FileOnly() || m.Rows() != 2 || m.rows[0].path != "/b.go" {
		t.Fatalf("file scope: fileOnly=%v rows=%d", m.FileOnly(), m.Rows())
	}
	// Switching the active file re-scopes the list live.
	m.SetActivePath("/a.go")
	if m.Rows() != 2 || m.rows[0].path != "/a.go" {
		t.Fatalf("rescope rows = %d path = %q", m.Rows(), m.rows[0].path)
	}
	m.Update(key("f"))
	if m.FileOnly() || m.Rows() != 4 {
		t.Fatalf("back to project: fileOnly=%v rows=%d", m.FileOnly(), m.Rows())
	}
}

func TestRefreshKeepsCursorOnDiagnostic(t *testing.T) {
	s := NewStore()
	s.Set("/a.go", []ilsp.Diagnostic{diag(1, 0, 1, "one", ""), diag(5, 0, 1, "two", "")})
	m := New(nil)
	m.SetSize(80, 10)
	m.SetStore(s)
	m.Update(key("j"))
	m.Update(key("j")) // on "two"... rows: header, one, two
	if m.rows[m.Cursor()].d.Message != "two" {
		t.Fatalf("cursor on %q", m.rows[m.Cursor()].d.Message)
	}
	// A new file appearing above must not move the selection off "two".
	s.Set("/0-first.go", []ilsp.Diagnostic{diag(0, 0, 1, "new", "")})
	m.Refresh()
	if m.rows[m.Cursor()].d.Message != "two" {
		t.Fatalf("after refresh cursor on %q, want two", m.rows[m.Cursor()].d.Message)
	}
}

func TestViewRendersRowsAndFooter(t *testing.T) {
	s := NewStore()
	s.Set("/proj/a.go", []ilsp.Diagnostic{diag(12, 4, 1, "undefined: foo\nextra", "E42")})
	m := New(nil)
	m.SetSize(120, 10)
	m.SetDisplayPath(func(p string) string { return strings.TrimPrefix(p, "/proj/") })
	m.SetStore(s)
	v := m.View()
	if !strings.Contains(v, "a.go") {
		t.Fatalf("view misses the file header:\n%s", v)
	}
	// line:col are 1-based on screen; the message clips at the newline.
	if !strings.Contains(v, "13:5") || !strings.Contains(v, "undefined: foo") || strings.Contains(v, "extra") {
		t.Fatalf("diagnostic row wrong:\n%s", v)
	}
	if !strings.Contains(v, "(E42)") {
		t.Fatalf("code missing:\n%s", v)
	}
	// #1064: singular for count 1.
	if !strings.Contains(v, "1 error · 0 warnings") {
		t.Fatalf("header counts missing:\n%s", v)
	}
	if !strings.Contains(v, "f current file") {
		t.Fatalf("footer must document the scope toggle:\n%s", v)
	}
}

func TestViewEmptyStates(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 8)
	m.SetStore(NewStore())
	if v := m.View(); !strings.Contains(v, "(no problems)") {
		t.Fatalf("empty view:\n%s", v)
	}
	m.Update(key("f"))
	if v := m.View(); !strings.Contains(v, "no active file") {
		t.Fatalf("file-scope empty view:\n%s", v)
	}
}

// TestHeaderPluralization guards #1064.
func TestHeaderPluralization(t *testing.T) {
	cases := map[int]string{0: "0 errors", 1: "1 error", 2: "2 errors"}
	for n, want := range cases {
		if got := plural(n, "error"); got != want {
			t.Errorf("plural(%d) = %q want %q", n, got, want)
		}
	}
}

// TestStoreDropRemovesSubtree guards #1102: deleting a file (or a directory
// with findings beneath it) prunes the store.
func TestStoreDropRemovesSubtree(t *testing.T) {
	s := NewStore()
	s.Set("/proj/a.go", []ilsp.Diagnostic{diag(0, 0, 1, "x", "")})
	s.Set("/proj/sub/b.go", []ilsp.Diagnostic{diag(0, 0, 1, "y", "")})
	s.Set("/proj/subx.go", []ilsp.Diagnostic{diag(0, 0, 1, "z", "")})
	s.Drop("/proj/a.go", false)
	if s.Get("/proj/a.go") != nil {
		t.Fatal("file drop must remove the entry")
	}
	s.Drop("/proj/sub", true)
	if s.Get("/proj/sub/b.go") != nil {
		t.Fatal("dir drop must remove entries beneath it")
	}
	if s.Get("/proj/subx.go") == nil {
		t.Fatal("sibling with the dir-name prefix must survive")
	}
}
