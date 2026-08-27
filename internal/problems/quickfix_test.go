package problems

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// quickfix_test.go covers the pane half of #2175: the quick-fix key asks the
// app for the marked problem's code actions and resolves which diagnostic
// that is — the pane itself never talks to a server.

// fixPanel builds a two-file panel with the cursor left on the first row.
func fixPanel(t *testing.T) *Model {
	t.Helper()
	s := NewStore()
	s.Set("/proj/a.go", []ilsp.Diagnostic{diag(4, 2, 1, "undefined: fooBar", "E1")})
	s.Set("/proj/b.go", []ilsp.Diagnostic{diag(1, 0, 2, "unused import", "W1")})
	m := New(nil)
	m.SetStore(s)
	m.SetSize(80, 12)
	m.SetFocused(true)
	return &m
}

// TestQuickFixKeyAsksForTheMarkedDiagnostic is the acceptance core: "a" on a
// diagnostic row emits the request, and the marked row resolves to exactly
// that diagnostic — path and range included, since the bridge needs both.
func TestQuickFixKeyAsksForTheMarkedDiagnostic(t *testing.T) {
	m := fixPanel(t)
	m.cursor = 1 // the first file's only diagnostic

	cmd := m.Update(key("a"))
	if cmd == nil {
		t.Fatal(`"a" on a diagnostic must ask for its code actions`)
	}
	if _, ok := cmd().(QuickFixMsg); !ok {
		t.Fatalf("msg = %#v, want QuickFixMsg", cmd())
	}
	path, d, ok := m.SelectedDiagnostic()
	if !ok {
		t.Fatal("the marked row must resolve to a diagnostic")
	}
	if path != "/proj/a.go" || d.Message != "undefined: fooBar" {
		t.Fatalf("target = %s %+v", path, d)
	}
	if d.Range.Start.Line != 4 || d.Range.Start.Col != 2 {
		t.Fatalf("range = %+v, want the diagnostic's own position", d.Range)
	}
}

// TestQuickFixAltEnterMatchesTheEditorKey: alt+enter carries the intention
// muscle memory into the pane — nothing else claims it in this context.
func TestQuickFixAltEnterMatchesTheEditorKey(t *testing.T) {
	m := fixPanel(t)
	m.cursor = 1
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("alt+enter must request quick fixes too")
	}
	if _, ok := cmd().(QuickFixMsg); !ok {
		t.Fatalf("msg = %#v, want QuickFixMsg", cmd())
	}
}

// TestQuickFixOnHeaderTakesTheFirstDiagnostic mirrors enter's header rule: a
// file header stands for its first (most severe) finding.
func TestQuickFixOnHeaderTakesTheFirstDiagnostic(t *testing.T) {
	m := fixPanel(t)
	m.cursor = 0 // the error file's header
	if cmd := m.Update(key("a")); cmd == nil {
		t.Fatal("a header row must request its first diagnostic's fixes")
	}
	path, d, ok := m.SelectedDiagnostic()
	if !ok || path != "/proj/a.go" || d.Message != "undefined: fooBar" {
		t.Fatalf("header target = %s %+v (ok=%v)", path, d, ok)
	}
}

// TestQuickFixOnRelatedRowTargetsTheParent: a related entry is context, not a
// finding — the server offers fixes for the diagnostic it hangs under.
func TestQuickFixOnRelatedRowTargetsTheParent(t *testing.T) {
	s := NewStore()
	rel := ilsp.RelatedInfo{Path: "/proj/other.go", Line: 9, Col: 4, Message: "declared here"}
	s.Set("/proj/main.go", []ilsp.Diagnostic{relDiag("redeclared", rel)})
	m := New(nil)
	m.SetStore(s)
	m.SetSize(80, 10)
	m.cursor = 2 // the related row

	if cmd := m.Update(key("a")); cmd == nil {
		t.Fatal("a related row must still request a fix for its parent")
	}
	path, d, ok := m.SelectedDiagnostic()
	if !ok || path != "/proj/main.go" || d.Message != "redeclared" {
		t.Fatalf("related target = %s %+v (ok=%v)", path, d, ok)
	}
	if d.Range.Start.Line != 4 {
		t.Fatalf("the parent's range must travel, got %+v", d.Range)
	}
}

// TestQuickFixOnEmptyListAsksNothing: with no rows there is nothing to fix,
// so the key produces no request at all.
func TestQuickFixOnEmptyListAsksNothing(t *testing.T) {
	m := New(nil)
	m.SetStore(NewStore())
	m.SetSize(80, 10)
	if cmd := m.Update(key("a")); cmd != nil {
		t.Fatalf("an empty list must ask for nothing, got %#v", cmd())
	}
	if _, _, ok := m.SelectedDiagnostic(); ok {
		t.Fatal("an empty list has no target")
	}
}

// TestCursorRowFollowsTheScrolledWindow: the popup anchor is the cursor's
// offset *inside the visible window*, so a scrolled list still anchors on the
// row the user sees.
func TestCursorRowFollowsTheScrolledWindow(t *testing.T) {
	s := NewStore()
	var ds []ilsp.Diagnostic
	for i := 0; i < 40; i++ {
		ds = append(ds, diag(i, 0, 1, "boom", ""))
	}
	s.Set("/proj/a.go", ds)
	m := New(nil)
	m.SetStore(s)
	m.SetSize(80, 8) // body height 6
	m.cursor = 30
	m.clampScroll()

	if got := m.CursorRow(); got < 0 || got >= m.bodyHeight() {
		t.Fatalf("CursorRow = %d, must sit inside the %d visible rows", got, m.bodyHeight())
	}
	if got, want := m.CursorRow(), m.cursor-m.top; got != want {
		t.Fatalf("CursorRow = %d, want %d", got, want)
	}
}

// TestFooterNamesTheFixKey keeps the key documented where it is used.
func TestFooterNamesTheFixKey(t *testing.T) {
	m := fixPanel(t)
	if v := m.View(); !strings.Contains(v, "a fix") {
		t.Errorf("footer must name the quick-fix key:\n%s", v)
	}
}
