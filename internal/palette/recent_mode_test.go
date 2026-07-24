package palette

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// testRecentMode builds a RecentMode over a fixed MRU list with every file
// treated as existing except those in missing.
func testRecentMode(list []string, missing ...string) *RecentMode {
	gone := make(map[string]bool, len(missing))
	for _, p := range missing {
		gone[p] = true
	}
	m := NewRecentMode(func() []RecentEntry {
		out := make([]RecentEntry, len(list))
		for i, p := range list {
			out[i] = RecentEntry{Path: p}
		}
		return out
	})
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

// TestRecentModeRowsCarryTimeAndAux (#1113): every row shows the relative
// last-opened time in the right-aligned Time column and carries the prune
// aux action, mirroring the project picker after #842.
func TestRecentModeRowsCarryTimeAndAux(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	m := NewRecentMode(func() []RecentEntry {
		return []RecentEntry{
			{Path: "a.go", LastOpened: now.Add(-5 * time.Minute)},
			{Path: "b.go"}, // migrated legacy entry: no timestamp
		}
	})
	m.exists = func(string) bool { return true }
	m.now = func() time.Time { return now }

	items := m.Results("", Context{Root: "."})
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Time != "5m ago" {
		t.Fatalf("Time = %q, want \"5m ago\"", items[0].Time)
	}
	if items[1].Time != "" {
		t.Fatalf("legacy entry must render no time, got %q", items[1].Time)
	}
	for i, want := range []string{"a.go", "b.go"} {
		aux, ok := items[i].Aux.(RemoveRecentFileMsg)
		if !ok || aux.Path != want {
			t.Fatalf("items[%d].Aux = %#v, want RemoveRecentFileMsg{%s}", i, items[i].Aux, want)
		}
	}
}

// TestRecentModeAuxKeyEmitsRemove (#1113): shift+delete on a recent-files row
// emits its RemoveRecentFileMsg and keeps the palette open.
func TestRecentModeAuxKeyEmitsRemove(t *testing.T) {
	m := testRecentMode([]string{"a.go", "b.go"})
	p := New(Config{}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)

	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("shift+delete must emit the aux msg")
	}
	if msg, ok := cmd().(RemoveRecentFileMsg); !ok || msg.Path != "a.go" {
		t.Fatalf("aux msg = %#v, want RemoveRecentFileMsg{a.go}", cmd())
	}
	if !p.IsOpen() {
		t.Fatal("removal must keep the palette open")
	}
}

// TestRecentModeClickAuxZoneEmitsRemove (#1113): a click on a row's "✕" zone
// emits the remove msg instead of opening the file.
func TestRecentModeClickAuxZoneEmitsRemove(t *testing.T) {
	m := testRecentMode([]string{"a.go"})
	m.SetProjects(func() []Item { return nil }) // single column: main list spans the box
	p := New(Config{}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)

	inner := p.boxWidth() - 4
	// The first result row is at box y=3 (border, prompt, separator); the ✕
	// zone is the last auxGlyphW cells. Click x is box-relative and shifted
	// by 2 (border + padding) inside Click.
	cmd := p.Click(2+inner-1, 3)
	if cmd == nil {
		t.Fatal("click on the ✕ zone must emit the aux msg")
	}
	if msg, ok := cmd().(RemoveRecentFileMsg); !ok || msg.Path != "a.go" {
		t.Fatalf("click aux msg = %#v, want RemoveRecentFileMsg{a.go}", cmd())
	}
	if !p.IsOpen() {
		t.Fatal("the ✕ click must keep the palette open")
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

// TestPaletteSideFocusStartsOnProjectsWhenNoFiles (#819): an empty recent-files
// list opens the dialog with the projects column focused, so enter immediately
// opens the previous project.
func TestPaletteSideFocusStartsOnProjectsWhenNoFiles(t *testing.T) {
	m := testRecentMode(nil)
	m.SetProjects(func() []Item {
		return []Item{{Title: "alpha", Msg: projectsMsg{Path: "/p/alpha"}}}
	})
	p := New(Config{}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)

	if !p.sideFocus {
		t.Fatal("empty files list must start the focus on the projects column")
	}
	cmd := p.Update(enterKey())
	if cmd == nil {
		t.Fatal("enter must activate the focused project")
	}
	if got, ok := cmd().(projectsMsg); !ok || got.Path != "/p/alpha" {
		t.Fatalf("activation msg = %#v, want the top project", cmd())
	}
}

// TestPaletteSideFocusFollowsProjectOnlyMatch (#819): a query matching only
// projects moves the focus (and enter target) to the projects column; deleting
// back to a file-matching query returns it.
func TestPaletteSideFocusFollowsProjectOnlyMatch(t *testing.T) {
	m := sideRecentMode()
	p := New(Config{}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)

	if p.sideFocus {
		t.Fatal("non-empty files list must start on the files column")
	}
	for _, r := range "alph" { // matches only the project "alpha"
		p.Update(runes(string(r)))
	}
	if len(p.items) != 0 {
		t.Fatalf("query should match no files, got %+v", p.items)
	}
	if !p.sideFocus {
		t.Fatal("project-only match must focus the projects column")
	}
	cmd := p.Update(enterKey())
	if got, ok := cmd().(projectsMsg); !ok || got.Path != "/p/alpha" {
		t.Fatalf("enter must open the matching project, got %#v", cmd())
	}
}

// TestPaletteSideFocusShiftsOnBetterProjectMatch (#819): a project whose match
// strictly outscores the best file hit pulls the focus to the projects column;
// files keep it on ties.
func TestPaletteSideFocusShiftsOnBetterProjectMatch(t *testing.T) {
	m := testRecentMode([]string{"bexxtxa.go"}) // weak scattered match for "beta"
	m.SetProjects(func() []Item {
		return []Item{{Title: "beta", Msg: projectsMsg{Path: "/p/beta"}}}
	})
	p := New(Config{}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)

	for _, r := range "beta" {
		p.Update(runes(string(r)))
	}
	if len(p.items) == 0 {
		t.Fatal("the scattered file must still fuzzy-match")
	}
	if !p.sideFocus {
		t.Fatal("a strictly better project match must shift the focus to projects")
	}
}

// TestPaletteSideFocusManualOverride (#819): an explicit tab switch wins over
// the automatic placement until the query changes again.
func TestPaletteSideFocusManualOverride(t *testing.T) {
	m := sideRecentMode()
	p := New(Config{}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)

	for _, r := range "alph" {
		p.Update(runes(string(r)))
	}
	if !p.sideFocus {
		t.Fatal("precondition: project-only match focuses projects")
	}
	// Manual switch back to files sticks while the query is unchanged.
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.sideFocus {
		t.Fatal("tab must move the focus back to the files column")
	}
	p.Update(downKey())
	if p.sideFocus {
		t.Fatal("navigation must not re-trigger the automatic placement")
	}
	// The next query edit clears the override and auto-places again.
	p.Update(runes("a"))
	if !p.sideFocus {
		t.Fatal("a query change must re-apply the automatic placement")
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
