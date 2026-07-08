package help

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/plugin"
	"ike/internal/registry"
)

// stubPlugin is a minimal Plugin exposing a fixed set of commands.
type stubPlugin struct {
	id  string
	cmd []plugin.Command
}

func (s stubPlugin) ID() string                        { return s.id }
func (s stubPlugin) Capabilities() plugin.Capabilities { return plugin.Capabilities{Commands: s.cmd} }

func testRegistry() *registry.Registry {
	r := registry.New()
	r.Add(stubPlugin{id: "core", cmd: []plugin.Command{
		{ID: "core.quit", Title: "Quit", Scope: plugin.GlobalScope()},
		{ID: "core.open", Title: "Open File", Scope: plugin.GlobalScope()},
		{ID: "editor.save", Title: "Save", Scope: plugin.PaneScope("editor")},
		{ID: "explorer.new", Title: "New File", Scope: plugin.PaneScope("explorer")},
	}})
	return r
}

func TestSnapshotJoinsBindingsAndGroups(t *testing.T) {
	r := testRegistry()
	res := MapResolver{"core.quit": "ctrl+c", "editor.save": ":w"}

	groups := Snapshot(r, res, "")

	// An empty contextID lists every scope at once: global first, then the rest
	// alphabetically (editor, explorer).
	var labels []string
	byLabel := map[string][]Entry{}
	for _, g := range groups {
		labels = append(labels, g.Label)
		byLabel[g.Label] = g.Entries
	}
	if got, want := strings.Join(labels, ","), "global,editor,explorer"; got != want {
		t.Fatalf("group order = %q, want %q", got, want)
	}

	// shortcut join
	var quit Entry
	for _, e := range byLabel["global"] {
		if e.ID == "core.quit" {
			quit = e
		}
	}
	if quit.Shortcut != "ctrl+c" {
		t.Fatalf("core.quit shortcut = %q, want ctrl+c", quit.Shortcut)
	}
	// unbound command degrades to title-only
	for _, e := range byLabel["global"] {
		if e.ID == "core.open" && e.Shortcut != "" {
			t.Fatalf("core.open should be unbound, got %q", e.Shortcut)
		}
	}
}

// TestSnapshotFiltersToFocusedContext verifies a non-empty contextID narrows
// the sheet to global commands plus the focused context's own group.
func TestSnapshotFiltersToFocusedContext(t *testing.T) {
	groups := Snapshot(testRegistry(), nil, "editor")
	var labels []string
	for _, g := range groups {
		labels = append(labels, g.Label)
	}
	if got, want := strings.Join(labels, ","), "global,editor"; got != want {
		t.Fatalf("filtered groups = %q, want %q", got, want)
	}
}

// TestSnapshotFallsBackToDocShortcut verifies a command with no resolver
// binding still shows its documentation-only Shortcut hint (vim ex-commands).
func TestSnapshotFallsBackToDocShortcut(t *testing.T) {
	r := registry.New()
	r.Add(stubPlugin{id: "editor", cmd: []plugin.Command{
		{ID: "editor.write", Title: "Save File", Scope: plugin.PaneScope("editor"), Shortcut: ":w"},
	}})
	groups := Snapshot(r, nil, "") // nil resolver -> only the doc hint can apply
	if len(groups) != 1 || len(groups[0].Entries) != 1 {
		t.Fatalf("unexpected groups %+v", groups)
	}
	if got := groups[0].Entries[0].Shortcut; got != ":w" {
		t.Fatalf("doc-hint shortcut = %q, want :w", got)
	}
}

// TestSnapshotResolverWinsOverDocShortcut verifies a live keymap binding takes
// precedence over the documentation hint.
func TestSnapshotResolverWinsOverDocShortcut(t *testing.T) {
	r := registry.New()
	r.Add(stubPlugin{id: "p", cmd: []plugin.Command{
		{ID: "p.do", Title: "Do", Scope: plugin.GlobalScope(), Shortcut: "doc"},
	}})
	groups := Snapshot(r, MapResolver{"p.do": "ctrl+x"}, "")
	if got := groups[0].Entries[0].Shortcut; got != "ctrl+x" {
		t.Fatalf("resolver shortcut = %q, want ctrl+x", got)
	}
}

// TestRenderSeparatesGroupsWithBlankLine verifies a blank line sits between the
// section blocks (Global, Editor, …) so they read as distinct clusters.
func TestRenderSeparatesGroupsWithBlankLine(t *testing.T) {
	h := New(testRegistry(), nil, 0)
	h.Snapshot("")
	body := h.Render(120)
	lines := strings.Split(body, "\n")
	blank := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			blank = true
			break
		}
	}
	if !blank {
		t.Fatalf("expected a blank separator line between groups:\n%s", body)
	}
}

func TestSnapshotDeterministicEntryOrder(t *testing.T) {
	r := testRegistry()
	a := Snapshot(r, nil, "")
	b := Snapshot(r, nil, "")
	if len(a) != len(b) {
		t.Fatalf("group count differs between snapshots")
	}
	for i := range a {
		if a[i].Label != b[i].Label || len(a[i].Entries) != len(b[i].Entries) {
			t.Fatalf("snapshot not deterministic at group %d", i)
		}
		for j := range a[i].Entries {
			if a[i].Entries[j].ID != b[i].Entries[j].ID {
				t.Fatalf("entry order not deterministic")
			}
		}
	}
}

func TestColumnCount(t *testing.T) {
	cases := []struct {
		width, minCol, want int
	}{
		{0, 10, 1},   // narrow -> single column
		{11, 10, 1},  // one column + gutter fits once (10+2=12 > 11)
		{24, 10, 2},  // two columns (12*2 = 24)
		{120, 18, 6}, // 120/(18+2)=6
		{5, 100, 1},  // floor
	}
	for _, c := range cases {
		if got := ColumnCount(c.width, c.minCol); got != c.want {
			t.Errorf("ColumnCount(%d,%d) = %d, want %d", c.width, c.minCol, got, c.want)
		}
	}
}

func TestPackColumnMajorBalanced(t *testing.T) {
	cells := []string{"a", "b", "c", "d", "e"}
	got := Pack(cells, 2)
	// rows = ceil(5/2) = 3 => col0 = a,b,c ; col1 = d,e
	if len(got) != 2 {
		t.Fatalf("columns = %d, want 2", len(got))
	}
	if strings.Join(got[0], "") != "abc" {
		t.Errorf("col0 = %v, want a,b,c", got[0])
	}
	if strings.Join(got[1], "") != "de" {
		t.Errorf("col1 = %v, want d,e", got[1])
	}
}

func TestPackSingleColumnFallback(t *testing.T) {
	cells := []string{"a", "b"}
	got := Pack(cells, 0)
	if len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("expected single column of 2, got %v", got)
	}
}

func TestPackEmpty(t *testing.T) {
	if got := Pack(nil, 3); got != nil {
		t.Fatalf("Pack(nil) = %v, want nil", got)
	}
}

func TestMinColumnWidth(t *testing.T) {
	cells := []string{"short", "a much longer entry"}
	// longest = 19, configMin smaller -> longest wins
	if got := MinColumnWidth(cells, 5); got != 19 {
		t.Errorf("MinColumnWidth = %d, want 19", got)
	}
	// configMin larger -> configMin wins
	if got := MinColumnWidth(cells, 40); got != 40 {
		t.Errorf("MinColumnWidth = %d, want 40", got)
	}
	// no cells, no config -> default
	if got := MinColumnWidth(nil, 0); got != defaultMinColWidth {
		t.Errorf("MinColumnWidth default = %d, want %d", got, defaultMinColWidth)
	}
}

func TestRenderContainsTitlesAndShortcuts(t *testing.T) {
	h := New(testRegistry(), MapResolver{"core.quit": "ctrl+c"}, 0)
	h.Snapshot("")
	// lipgloss v2 always emits styling escapes (it no longer detects the
	// terminal); strip them so assertions match the logical text.
	body := ansi.Strip(h.Render(120))
	for _, want := range []string{"Quit", "Save", "ctrl+c", "Global", "Editor"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

// TestRenderEntryRightAlignsShortcut verifies the shortcut is pushed to the
// right edge of the column so the keys line up as their own column.
func TestRenderEntryRightAlignsShortcut(t *testing.T) {
	h := New(registry.New(), nil, 0)
	got := ansi.Strip(h.renderEntry(Entry{Title: "Quit", Shortcut: "ctrl+c"}, 20))
	if got != "Quit          ctrl+c" {
		t.Fatalf("entry = %q, want shortcut right-aligned to width 20", got)
	}
	// A clamped column keeps at least the minimum gap between title and key.
	tight := ansi.Strip(h.renderEntry(Entry{Title: "Quit", Shortcut: "ctrl+c"}, 5))
	if tight != "Quit  ctrl+c" {
		t.Fatalf("clamped entry = %q, want minimum two-space gap", tight)
	}
	// Unbound commands render title-only.
	if got := h.renderEntry(Entry{Title: "Open"}, 20); got != "Open" {
		t.Fatalf("unbound entry = %q, want title only", got)
	}
}

func TestRenderEmptyWhenNoCommands(t *testing.T) {
	h := New(registry.New(), nil, 0)
	h.Snapshot("")
	if got := h.Render(80); got != "no commands registered" {
		t.Fatalf("empty render = %q", got)
	}
}

func TestRenderNeverExceedsTwoColumns(t *testing.T) {
	r := registry.New()
	var cmds []plugin.Command
	for i := 0; i < 40; i++ {
		id := "g.cmd" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		cmds = append(cmds, plugin.Command{ID: id, Title: "Cmd", Scope: plugin.GlobalScope()})
	}
	r.Add(stubPlugin{id: "g", cmd: cmds})
	h := New(r, nil, 0)
	h.Snapshot("")
	// With 40 entries capped at two columns, even given a very wide budget the
	// body packs column-major into rows = ceil(40/2) = 20 — so it stays tall and
	// narrow rather than spreading across the budget.
	body := h.Render(400)
	colW := MinColumnWidth(h.allCells(), 0) + colSlack
	if w, limit := lipgloss.Width(body), 2*colW+gutter; w > limit {
		t.Fatalf("body width %d exceeds two-column bound %d", w, limit)
	}
	if hgt := lipgloss.Height(body); hgt < 20 {
		t.Fatalf("two-column body should stack ~20 rows tall, got height %d", hgt)
	}
}
