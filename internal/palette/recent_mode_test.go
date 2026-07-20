package palette

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// testRecentMode builds a RecentMode over a fixed MRU list with every file
// treated as existing except those in missing.
func testRecentMode(list []string, missing ...string) *RecentMode {
	gone := make(map[string]bool, len(missing))
	for _, p := range missing {
		gone[p] = true
	}
	m := NewRecentMode(func() []string { return list })
	m.exists = func(path string) bool { return !gone[path] }
	return m
}

func TestRecentModeListsMRUOrder(t *testing.T) {
	m := testRecentMode([]string{"b.go", "a.go", "c.go"})
	items := m.Results("", Context{Root: "."})
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	for i, want := range []string{"b.go", "a.go", "c.go"} {
		if items[i].Title != want {
			t.Fatalf("items[%d] = %q, want %q", i, items[i].Title, want)
		}
	}
}

func TestRecentModeExcludesActiveFile(t *testing.T) {
	m := testRecentMode([]string{"active.go", "prev.go"})
	items := m.Results("", Context{Root: ".", ActivePath: "active.go"})
	if len(items) != 1 || items[0].Title != "prev.go" {
		t.Fatalf("items = %+v, want only prev.go", items)
	}
}

func TestRecentModeDropsMissingFiles(t *testing.T) {
	m := testRecentMode([]string{"gone.go", "here.go"}, "gone.go")
	items := m.Results("", Context{Root: "."})
	if len(items) != 1 || items[0].Title != "here.go" {
		t.Fatalf("items = %+v, want only here.go", items)
	}
}

func TestRecentModeFuzzyFiltersAndKeepsMRUOnTies(t *testing.T) {
	m := testRecentMode([]string{"pkg/beta.go", "pkg/alpha.go", "README.md"})
	items := m.Results("pkg", Context{Root: "."})
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (README filtered)", len(items))
	}
	// Both match "pkg" identically: MRU order must survive the sort.
	if items[0].Title != "pkg/beta.go" || items[1].Title != "pkg/alpha.go" {
		t.Fatalf("tie order = %q, %q; want MRU order", items[0].Title, items[1].Title)
	}
}

func TestRecentModeRelativizesAbsolutePaths(t *testing.T) {
	// The explorer opens files with absolute paths while Root is "."; the
	// display must still be project-relative, the Msg keeps the original.
	abs, err := filepath.Abs(filepath.Join("sub", "file.go"))
	if err != nil {
		t.Fatal(err)
	}
	m := testRecentMode([]string{abs})
	items := m.Results("", Context{Root: "."})
	if len(items) != 1 || items[0].Title != "sub/file.go" {
		t.Fatalf("items = %+v, want title sub/file.go", items)
	}
	if msg := items[0].Msg.(OpenFileMsg); msg.Path != abs {
		t.Fatalf("Msg.Path = %q, want the original absolute path", msg.Path)
	}
}

func TestRecentModeEmitsOpenFileMsgWithOriginalPath(t *testing.T) {
	m := testRecentMode([]string{"internal/app/app.go"})
	items := m.Results("", Context{Root: "."})
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	msg, ok := items[0].Msg.(OpenFileMsg)
	if !ok || msg.Path != "internal/app/app.go" {
		t.Fatalf("Msg = %#v, want OpenFileMsg with original path", items[0].Msg)
	}
}

// projectsMsg is the stand-in activation msg for side-column tests (#778) —
// the real app injects project.PickedMsg, which the palette never imports.
type projectsMsg struct{ Path string }

func sideRecentMode() *RecentMode {
	m := testRecentMode([]string{"a.go", "b.go"})
	m.SetProjects(func() []Item {
		return []Item{
			{Title: "alpha", Msg: projectsMsg{Path: "/p/alpha"}},
			{Title: "beta", Msg: projectsMsg{Path: "/p/beta"}},
		}
	})
	return m
}

func TestRecentModeSideResultsFilterAndOrder(t *testing.T) {
	m := sideRecentMode()
	side := m.SideResults("", Context{})
	if len(side) != 2 || side[0].Title != "alpha" || side[1].Title != "beta" {
		t.Fatalf("empty query must keep recency order, got %+v", side)
	}
	side = m.SideResults("bet", Context{})
	if len(side) != 1 || side[0].Title != "beta" {
		t.Fatalf("query must fuzzy-filter the projects, got %+v", side)
	}
	if m.SideTitle() != "Recent Projects" {
		t.Fatalf("side title = %q", m.SideTitle())
	}
}

func keyPress(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }
func downKey() tea.KeyPressMsg           { return tea.KeyPressMsg{Code: tea.KeyDown} }
func enterKey() tea.KeyPressMsg          { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func leftKey() tea.KeyPressMsg           { return tea.KeyPressMsg{Code: tea.KeyLeft} }
func rightKey() tea.KeyPressMsg          { return tea.KeyPressMsg{Code: tea.KeyRight} }

func TestPaletteSideColumnFocusAndActivate(t *testing.T) {
	m := sideRecentMode()
	p := New(Config{}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)

	if v := p.View(); !strings.Contains(v, "Recent Projects") {
		t.Fatalf("view must render the side column heading:\n%s", v)
	}
	// tab moves the focus into the projects column; down + enter picks the
	// second project and emits its injected msg.
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !p.sideFocus {
		t.Fatal("tab must focus the side column")
	}
	p.Update(downKey())
	cmd := p.Update(enterKey())
	if cmd == nil {
		t.Fatal("enter on a side row must emit its msg")
	}
	got, ok := cmd().(projectsMsg)
	if !ok || got.Path != "/p/beta" {
		t.Fatalf("side activation msg = %#v", cmd())
	}
	if p.IsOpen() {
		t.Fatal("side activation must close the palette")
	}
}

func TestPaletteSideColumnArrowsOnEmptyQuery(t *testing.T) {
	m := sideRecentMode()
	p := New(Config{}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)

	p.Update(leftKey())
	if !p.sideFocus {
		t.Fatal("left on an empty query must focus the side column")
	}
	p.Update(rightKey())
	if p.sideFocus {
		t.Fatal("right must return focus to the files column")
	}
	// With query text, arrows stay cursor keys.
	p.Update(runes("a"))
	p.Update(leftKey())
	if p.sideFocus {
		t.Fatal("left with query text must stay a cursor key")
	}
}
