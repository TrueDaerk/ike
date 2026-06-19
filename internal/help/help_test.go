package help

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

	groups := Snapshot(r, res, "editor")

	// editor context => global commands + editor-scoped, but not explorer-scoped.
	var labels []string
	byLabel := map[string][]Entry{}
	for _, g := range groups {
		labels = append(labels, g.Label)
		byLabel[g.Label] = g.Entries
	}
	if got, want := strings.Join(labels, ","), "global,editor"; got != want {
		t.Fatalf("group order = %q, want %q", got, want)
	}
	if _, ok := byLabel["explorer"]; ok {
		t.Fatalf("explorer-scoped command leaked into editor context")
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

func TestOverlayOpenCloseDismiss(t *testing.T) {
	h := New(testRegistry(), nil, 0)
	h.SetSize(100, 40)
	if h.IsOpen() {
		t.Fatal("overlay should start closed")
	}
	h.Open("editor")
	if !h.IsOpen() {
		t.Fatal("overlay should be open after Open")
	}
	if h.View() == "" {
		t.Fatal("open overlay should render non-empty")
	}
	dismiss := []struct {
		name string
		msg  tea.KeyMsg
	}{
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}},
		{"?", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}},
		{"q", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}},
	}
	for _, d := range dismiss {
		h.Open("editor")
		if !h.Update(d.msg) {
			t.Fatalf("key %q not consumed", d.name)
		}
		if h.IsOpen() {
			t.Fatalf("key %q did not dismiss overlay", d.name)
		}
	}
}

func TestOverlayUpdateIgnoredWhenClosed(t *testing.T) {
	h := New(testRegistry(), nil, 0)
	if h.Update(tea.KeyMsg{Type: tea.KeyEsc}) {
		t.Fatal("closed overlay should not consume keys")
	}
}

func TestScrollBoundsClamp(t *testing.T) {
	s := newScroller(20, 3)
	s.SetContent(strings.Repeat("line\n", 50))
	if !s.scrollable() {
		t.Fatal("content taller than viewport should be scrollable")
	}
	if !s.vp.AtTop() {
		t.Fatal("SetContent should reset to top")
	}
	// scroll up at the top stays clamped at top
	s.Update(tea.KeyMsg{Type: tea.KeyUp})
	if !s.vp.AtTop() {
		t.Fatal("scroll up at top should clamp")
	}
	// G jumps to bottom
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if !s.vp.AtBottom() {
		t.Fatal("G should jump to bottom")
	}
	// scrolling down at the bottom stays clamped
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !s.vp.AtBottom() {
		t.Fatal("scroll down at bottom should clamp")
	}
	// g jumps back to top
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if !s.vp.AtTop() {
		t.Fatal("g should jump to top")
	}
}

func TestViewContainsTitlesAndShortcuts(t *testing.T) {
	h := New(testRegistry(), MapResolver{"core.quit": "ctrl+c"}, 0)
	h.SetSize(120, 40)
	h.Open("editor")
	view := h.View()
	for _, want := range []string{"Quit", "Save", "ctrl+c", "Global", "Editor"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
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
	h.SetSize(400, 60) // very wide terminal
	h.Open("")
	// With 40 entries capped at two columns, the body packs column-major into
	// rows = ceil(40/2) = 20 — so the pane stays tall and narrow rather than
	// spreading across the 400-col terminal. Its width must not exceed what two
	// command columns plus chrome need (the title sets the floor).
	v := h.View()
	w := lipgloss.Width(v)
	colW := MinColumnWidth(h.allCells(), 0)
	if limit := 2*colW + gutter + frameH; w > limit && w > 60 {
		t.Fatalf("pane width %d exceeds two-column bound %d (title floor 60)", w, limit)
	}
	if hgt := lipgloss.Height(v); hgt < 20 {
		t.Fatalf("two-column body should stack ~20 rows tall, got height %d", hgt)
	}
}

func TestPaneFitsWithinTerminal(t *testing.T) {
	r := testRegistry()
	h := New(r, nil, 0)
	h.SetSize(80, 24)
	h.Open("editor")
	v := h.View()
	if lipgloss.Width(v) > 80 || lipgloss.Height(v) > 24 {
		t.Fatalf("pane %dx%d overflows 80x24 terminal", lipgloss.Width(v), lipgloss.Height(v))
	}
}
