package hiertree

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

// item is the stand-in protocol item: the tests only need to see which row
// a fetch was issued for.
type item struct{ name string }

// recorder captures the expansion requests the tree issues.
type recorder struct {
	reqs []struct {
		reqID int
		name  string
	}
}

func (r *recorder) fetch(reqID int, it item) tea.Cmd {
	r.reqs = append(r.reqs, struct {
		reqID int
		name  string
	}{reqID, it.name})
	return nil
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func row(name, path string, line int) Row[item] {
	return Row[item]{Entry: Entry{Name: name, Path: path, Line: line}, Item: item{name}}
}

// open builds a tree on one root whose expansion is in flight.
func open(t *testing.T) (*Tree[item], *recorder) {
	t.Helper()
	tr := &Tree[item]{}
	rec := &recorder{}
	tr.Open([]Row[item]{row("Root", "/proj/a.go", 3)}, rec.fetch)
	return tr, rec
}

// press routes a key with no-op host callbacks; the host-side wiring (enter
// closing, tab flipping a direction) is the host packages' concern.
func press(tr *Tree[item], key string) (tea.Cmd, bool) {
	return tr.Key(key, 5, func(Row[item]) tea.Cmd { return nil }, func() tea.Cmd { return nil })
}

// render is the plain-text rows at width 40 and the given height.
func render(tr *Tree[item], height int) []string {
	rows := tr.RenderRows(40, height, theme.DefaultPalette(), nil, "empty")
	for i, r := range rows {
		rows[i] = plain(r)
	}
	return rows
}

func TestOpenExpandsFirstRoot(t *testing.T) {
	tr, rec := open(t)
	if len(rec.reqs) != 1 || rec.reqs[0].name != "Root" {
		t.Fatalf("open should fetch the first root, got %+v", rec.reqs)
	}
	if got := render(tr, 5); len(got) != 1 || got[0] != "… Root  /proj/a.go:4" {
		t.Fatalf("root row while loading = %q", got)
	}
}

func TestEmptyTreeRendersNotice(t *testing.T) {
	tr := &Tree[item]{}
	if cmd := tr.Open(nil, nil); cmd != nil {
		t.Fatal("no roots: nothing to expand")
	}
	if got := render(tr, 5); len(got) != 1 || got[0] != "empty" {
		t.Fatalf("empty tree = %q, want the notice", got)
	}
	if tr.Current() != nil {
		t.Error("empty tree has no current row")
	}
	// Tree keys on an empty tree are consumed without effect.
	for _, k := range []string{"enter", "right", "left"} {
		if _, ok := press(tr, k); !ok {
			t.Errorf("%q should be consumed on an empty tree", k)
		}
	}
}

func TestApplyFillsChildrenAndMarkers(t *testing.T) {
	tr, rec := open(t)
	tr.Apply(rec.reqs[0].reqID, false, []Row[item]{
		row("leaf", "/proj/b.go", 8),
		row("other", "/proj/c.go", 2),
	})
	got := render(tr, 5)
	want := []string{
		"▾ Root  /proj/a.go:4",
		"  ▸ leaf  /proj/b.go:9",
		"  ▸ other  /proj/c.go:3",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	// An empty reply makes a leaf: "▾" while unfolded, "·" once collapsed.
	press(tr, "down")
	press(tr, "right")
	tr.Apply(rec.reqs[1].reqID, false, nil)
	if got := render(tr, 5)[1]; got != "  ▾ leaf  /proj/b.go:9" {
		t.Fatalf("unfolded leaf = %q", got)
	}
	press(tr, "left")
	if got := render(tr, 5)[1]; got != "  · leaf  /proj/b.go:9" {
		t.Fatalf("collapsed leaf = %q", got)
	}
}

func TestDetailAndDisplayPath(t *testing.T) {
	tr, _ := open(t)
	tr.nodes[0].Entry.Detail = "func()"
	rows := tr.RenderRows(40, 5, theme.DefaultPalette(),
		func(p string) string { return strings.TrimPrefix(p, "/proj/") }, "empty")
	if got := plain(rows[0]); got != "… Root func()  a.go:4" {
		t.Fatalf("row = %q", got)
	}
}

func TestExpandFetchesLazily(t *testing.T) {
	tr, rec := open(t)
	tr.Apply(rec.reqs[0].reqID, false, []Row[item]{row("child", "/proj/b.go", 8)})
	press(tr, "down")
	press(tr, "right")
	if len(rec.reqs) != 2 || rec.reqs[1].name != "child" {
		t.Fatalf("expanding the child should fetch it, got %+v", rec.reqs)
	}
	// A second expand while loading must not refetch.
	press(tr, "space")
	if len(rec.reqs) != 2 {
		t.Fatalf("in-flight row refetched: %+v", rec.reqs)
	}
	// Nor does expanding an already-loaded row.
	tr.Apply(rec.reqs[1].reqID, false, nil)
	press(tr, "l")
	if len(rec.reqs) != 2 {
		t.Fatalf("loaded row refetched: %+v", rec.reqs)
	}
}

func TestCollapseAndParentJump(t *testing.T) {
	tr, rec := open(t)
	tr.Apply(rec.reqs[0].reqID, false, []Row[item]{
		row("main", "/proj/main.go", 8),
		row("other", "/proj/other.go", 2),
	})
	press(tr, "down")
	press(tr, "down") // on "other"
	press(tr, "left") // collapsed leaf: jump to parent (the root)
	if tr.Cursor() != 0 {
		t.Fatalf("left on a collapsed row should land on the parent, cursor = %d", tr.Cursor())
	}
	press(tr, "h") // collapse the root
	if got := render(tr, 5); len(got) != 1 || got[0] != "▸ Root  /proj/a.go:4" {
		t.Fatalf("collapsing the root should hide its children, got %q", got)
	}
	// Re-expanding a loaded row must not refetch and shows the cached children.
	press(tr, "right")
	if len(rec.reqs) != 1 {
		t.Fatalf("re-expanding a loaded row refetched: %+v", rec.reqs)
	}
	if got := render(tr, 5); len(got) != 3 {
		t.Fatalf("re-expand should show the cached children, got %q", got)
	}
	// Left on a root with no parent stays put.
	press(tr, "left")
	press(tr, "left")
	if tr.Cursor() != 0 {
		t.Fatalf("left on a collapsed root moved the cursor to %d", tr.Cursor())
	}
}

func TestEnterHandsRowToHost(t *testing.T) {
	tr, rec := open(t)
	tr.Apply(rec.reqs[0].reqID, false, []Row[item]{row("child", "/proj/b.go", 8)})
	press(tr, "down")
	var got Row[item]
	cmd, ok := tr.Key("enter", 5, func(r Row[item]) tea.Cmd {
		got = r
		return func() tea.Msg { return "jumped" }
	}, nil)
	if !ok || cmd == nil || cmd() != "jumped" {
		t.Fatal("enter should run the host's command")
	}
	if got.Entry.Path != "/proj/b.go" || got.Entry.Line != 8 || got.Item.name != "child" {
		t.Fatalf("enter handed the wrong row: %+v", got)
	}
}

func TestTabRebuildsAndDropsStaleReplies(t *testing.T) {
	tr, rec := open(t)
	staleID := rec.reqs[0].reqID
	_, ok := tr.Key("tab", 5, nil, func() tea.Cmd { return tr.Rebuild() })
	if !ok || len(rec.reqs) != 2 {
		t.Fatalf("tab should rebuild and refetch the root, got %+v", rec.reqs)
	}
	// The pre-rebuild reply must not land in the fresh tree.
	tr.Apply(staleID, false, []Row[item]{row("stale", "/proj/x.go", 0)})
	if got := render(tr, 5); len(got) != 1 {
		t.Fatalf("stale reply landed: %q", got)
	}
	// A reply the host marks stale is dropped too, but its request retired.
	tr.Apply(rec.reqs[1].reqID, true, []Row[item]{row("late", "/proj/y.go", 0)})
	if got := render(tr, 5); len(got) != 1 {
		t.Fatalf("stale-direction reply landed: %q", got)
	}
	if len(tr.pending) != 0 {
		t.Fatal("a stale reply must still retire its request")
	}
}

func TestClearDropsTree(t *testing.T) {
	tr, rec := open(t)
	tr.Clear()
	if got := render(tr, 5); got[0] != "empty" {
		t.Fatalf("cleared tree still renders %q", got)
	}
	tr.Apply(rec.reqs[0].reqID, false, []Row[item]{row("late", "/proj/y.go", 0)})
	if got := render(tr, 5); got[0] != "empty" {
		t.Fatalf("reply after clear landed: %q", got)
	}
}

func TestUnownedKeysFallThrough(t *testing.T) {
	tr, _ := open(t)
	for _, k := range []string{"esc", "q", "x"} {
		if _, ok := press(tr, k); ok {
			t.Errorf("%q must be left to the host", k)
		}
	}
}

func TestScrollWindowFollowsCursor(t *testing.T) {
	tr, rec := open(t)
	var kids []Row[item]
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		kids = append(kids, row(n, "/p/"+n, 0))
	}
	tr.Apply(rec.reqs[0].reqID, false, kids) // 7 visible rows
	if got := render(tr, 3); len(got) != 3 || !strings.HasPrefix(got[0], "▾ Root") {
		t.Fatalf("window at top = %q", got)
	}
	press(tr, "G")
	got := render(tr, 3)
	if len(got) != 3 || !strings.Contains(got[2], "f") || !strings.Contains(got[0], "d") {
		t.Fatalf("window should end on the last row, got %q", got)
	}
	press(tr, "up")
	if got := render(tr, 3); !strings.Contains(got[0], "d") {
		t.Fatalf("stepping up inside the window must not scroll, got %q", got)
	}
	// Page keys clamp, single steps wrap.
	press(tr, "pgdown")
	if tr.Cursor() != 6 {
		t.Fatalf("pgdown should clamp at the end, cursor = %d", tr.Cursor())
	}
	press(tr, "down")
	if tr.Cursor() != 0 {
		t.Fatalf("down on the last row should wrap, cursor = %d", tr.Cursor())
	}
	if got := render(tr, 3); !strings.HasPrefix(got[0], "▾ Root") {
		t.Fatalf("window should follow the cursor back to the top, got %q", got)
	}
}

func TestRenderClampsCursorAfterCollapse(t *testing.T) {
	tr, rec := open(t)
	tr.Apply(rec.reqs[0].reqID, false, []Row[item]{row("a", "/p/a", 0), row("b", "/p/b", 0)})
	press(tr, "G")
	tr.nodes[0].expanded = false // shrink the tree under the cursor
	got := render(tr, 5)
	if len(got) != 1 || tr.Cursor() != 0 {
		t.Fatalf("cursor should clamp to the remaining row, got %q cursor %d", got, tr.Cursor())
	}
	if tr.Current() == nil {
		t.Fatal("clamped cursor should select the root")
	}
}
