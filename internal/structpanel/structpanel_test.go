package structpanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/lsp"
)

func sampleTree() []lsp.SymbolNode {
	return []lsp.SymbolNode{
		{Name: "Server", Kind: 5, Line: 2, Col: 5, EndLine: 20, Children: []lsp.SymbolNode{
			{Name: "Start", Kind: 6, Line: 4, Col: 7, EndLine: 9},
			{Name: "Stop", Kind: 6, Line: 11, Col: 7, EndLine: 15},
		}},
		{Name: "main", Kind: 12, Line: 22, Col: 5, EndLine: 30},
	}
}

func newPanel() *Model {
	m := New(nil)
	m.SetSize(60, 12)
	m.SetFocused(true)
	m.SetSymbols("/proj/a.go", sampleTree(), false)
	return &m
}

func TestFlattenDepthFirstWithDepths(t *testing.T) {
	rows := Flatten(sampleTree())
	names := make([]string, len(rows))
	depths := make([]int, len(rows))
	for i, r := range rows {
		names[i], depths[i] = r.Name, r.Depth
	}
	if strings.Join(names, ",") != "Server,Start,Stop,main" {
		t.Fatalf("order = %v", names)
	}
	if depths[0] != 0 || depths[1] != 1 || depths[2] != 1 || depths[3] != 0 {
		t.Fatalf("depths = %v", depths)
	}
}

func TestEnterNavigatesToSelectedSymbol(t *testing.T) {
	m := newPanel()
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}) // Start
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must dispatch a navigation")
	}
	nav, ok := cmd().(NavigateMsg)
	if !ok || nav.Path != "/proj/a.go" || nav.Line != 4 || nav.Col != 7 {
		t.Fatalf("nav = %#v", cmd())
	}
}

func TestDoubleClickNavigatesSingleSelects(t *testing.T) {
	m := newPanel()
	now := time.Now()
	m.now = func() time.Time { return now }
	// Row area starts below the header (y 1); row index 3 = "main".
	if cmd := m.Click(2, 4); cmd != nil {
		t.Fatal("first click only selects")
	}
	if m.Cursor() != 3 {
		t.Fatalf("cursor = %d, want 3", m.Cursor())
	}
	now = now.Add(100 * time.Millisecond)
	cmd := m.Click(2, 4)
	if cmd == nil {
		t.Fatal("second click within the window must navigate")
	}
	if nav, ok := cmd().(NavigateMsg); !ok || nav.Line != 22 {
		t.Fatalf("nav = %#v", cmd())
	}
	// A slow second click selects again instead of navigating.
	now = now.Add(2 * time.Second)
	if cmd := m.Click(2, 4); cmd != nil {
		t.Fatal("a click outside the double-click window must not navigate")
	}
}

func TestFollowHighlightsEnclosingSymbol(t *testing.T) {
	m := newPanel()
	m.SetFocused(false)
	m.Follow(12) // inside Stop [11,15] inside Server [2,20]
	if m.Current() != 2 {
		t.Fatalf("current = %d, want 2 (Stop)", m.Current())
	}
	m.Follow(3) // inside Server, before Start
	if m.Current() != 0 {
		t.Fatalf("current = %d, want 0 (Server)", m.Current())
	}
	m.Follow(21) // between Server and main: nearest preceding
	if m.Current() != 2 {
		t.Fatalf("current = %d, want 2 (nearest preceding, Stop)", m.Current())
	}
	m.Follow(0) // before every symbol
	if m.Current() != -1 {
		t.Fatalf("current = %d, want -1", m.Current())
	}
}

func TestWheelScrollsAndClampsCursor(t *testing.T) {
	m := newPanel()
	m.SetSize(60, 3) // body height 2
	m.Wheel(2)
	if m.top != 2 {
		t.Fatalf("top = %d, want 2", m.top)
	}
	if m.Cursor() < m.top {
		t.Fatalf("cursor %d must be pulled into the window at top %d", m.Cursor(), m.top)
	}
	m.Wheel(-10)
	if m.top != 0 {
		t.Fatalf("top = %d, want 0 after clamping", m.top)
	}
}

func TestSetSymbolsKeepsSelectionForSameFile(t *testing.T) {
	m := newPanel()
	m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"}) // main
	m.SetSymbols("/proj/a.go", sampleTree(), false)
	if m.Rows()[m.Cursor()].Name != "main" {
		t.Fatalf("selection should stick to the symbol name, got %q", m.Rows()[m.Cursor()].Name)
	}
	m.SetSymbols("/proj/b.go", nil, false)
	if m.Cursor() != 0 || len(m.Rows()) != 0 {
		t.Fatal("a new file resets the panel")
	}
}

func TestViewRendersGlyphsIndentAndNotices(t *testing.T) {
	m := newPanel()
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "C Server") {
		t.Fatalf("class row missing: %q", v)
	}
	if !strings.Contains(v, "  m Start") {
		t.Fatalf("indented method row missing: %q", v)
	}
	if !strings.Contains(v, "ƒ main") {
		t.Fatalf("function row missing: %q", v)
	}
	if !strings.Contains(v, "a.go") {
		t.Fatalf("header must name the file: %q", v)
	}

	empty := New(nil)
	empty.SetSize(40, 6)
	if v := ansi.Strip(empty.View()); !strings.Contains(v, "open a file") {
		t.Fatalf("pathless notice missing: %q", v)
	}
	empty.SetSymbols("/proj/a.go", nil, true)
	if v := ansi.Strip(empty.View()); !strings.Contains(v, "no language server") {
		t.Fatalf("no-provider notice missing: %q", v)
	}
	empty.SetSymbols("/proj/a.go", nil, false)
	if v := ansi.Strip(empty.View()); !strings.Contains(v, "no symbols") {
		t.Fatalf("no-symbols notice missing: %q", v)
	}
}
