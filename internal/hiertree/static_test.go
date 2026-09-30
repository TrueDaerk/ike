package hiertree

import (
	"strings"
	"testing"
)

// static_test.go covers the in-memory side of the tree (#2840): the
// synchronous Static fetch, Refresh keeping expansion and selection across a
// rebuild, ExpandDeep, the mouse helpers and the path-less row rendering.

// staticTree builds a tree over a nested name map: "a" → ["a/1", "a/2"], …
func staticTree(kids map[string][]string, roots ...string) *Tree[item] {
	tr := &Tree[item]{}
	children := func(it item) []Row[item] {
		var out []Row[item]
		for _, n := range kids[it.name] {
			out = append(out, Row[item]{Entry: Entry{Name: n, Line: -1}, Item: item{n}})
		}
		return out
	}
	var rs []Row[item]
	for _, r := range roots {
		rs = append(rs, Row[item]{Entry: Entry{Name: r, Line: -1}, Item: item{r}})
	}
	tr.Refresh(rs, func(it item) string { return it.name })
	tr.fetch = Static(tr, children)
	return tr
}

func names(tr *Tree[item]) []string {
	var out []string
	for _, r := range tr.Visible() {
		out = append(out, strings.Repeat(" ", r.Depth())+r.Entry.Name)
	}
	return out
}

func TestStaticExpandsInline(t *testing.T) {
	tr := staticTree(map[string][]string{"a": {"a/1", "a/2"}}, "a", "b")
	if got := names(tr); strings.Join(got, ",") != "a,b" {
		t.Fatalf("fresh tree = %v", got)
	}
	if cmd := tr.Expand(tr.Roots()[0]); cmd != nil {
		t.Fatal("a static fetch returns no command")
	}
	if got := names(tr); strings.Join(got, ",") != "a, a/1, a/2,b" {
		t.Fatalf("after expand = %v", got)
	}
	if len(tr.pending) != 0 {
		t.Fatal("static expansion left a request pending")
	}
	tr.Expand(tr.Roots()[1])
	if r := tr.Roots()[1]; !r.Leaf() || !r.Expanded() {
		t.Fatalf("b must be a loaded leaf, got leaf=%v expanded=%v", r.Leaf(), r.Expanded())
	}
}

func TestRefreshKeepsExpansionAndSelection(t *testing.T) {
	kids := map[string][]string{"t1": {"d1"}, "d1": {"f1", "f2"}, "t2": {"d2"}}
	tr := staticTree(kids, "t1", "t2")
	key := func(it item) string { return it.name }
	tr.ExpandDeep(tr.Roots()[0])
	if got := names(tr); strings.Join(got, ",") != "t1, d1,  f1,  f2,t2" {
		t.Fatalf("deep expand = %v", got)
	}
	tr.SetCursor(3) // f2
	if tr.Current().Entry.Name != "f2" {
		t.Fatalf("cursor on %q", tr.Current().Entry.Name)
	}

	// A refresh that appends a third turn and a file under d1.
	kids["d1"] = []string{"f1", "f2", "f3"}
	roots := []Row[item]{
		{Entry: Entry{Name: "t1"}, Item: item{"t1"}},
		{Entry: Entry{Name: "t2"}, Item: item{"t2"}},
		{Entry: Entry{Name: "t3"}, Item: item{"t3"}},
	}
	if cmd := tr.Refresh(roots, key); cmd != nil {
		t.Fatal("static refresh returns no command")
	}
	if got := names(tr); strings.Join(got, ",") != "t1, d1,  f1,  f2,  f3,t2,t3" {
		t.Fatalf("after refresh = %v", got)
	}
	if tr.Current().Entry.Name != "f2" {
		t.Fatalf("selection moved to %q", tr.Current().Entry.Name)
	}

	// The selected row vanishes: the cursor stays clamped where it was.
	kids["d1"] = []string{"f1"}
	tr.Refresh(roots, key)
	if got := names(tr); strings.Join(got, ",") != "t1, d1,  f1,t2,t3" {
		t.Fatalf("after shrink = %v", got)
	}
	if tr.Current().Entry.Name != "t2" {
		t.Fatalf("clamped cursor on %q, want t2 (same index)", tr.Current().Entry.Name)
	}
}

func TestToggleAndWheel(t *testing.T) {
	kids := map[string][]string{"a": {"1", "2", "3", "4", "5", "6"}}
	tr := staticTree(kids, "a")
	root := tr.Roots()[0]
	tr.Toggle(root)
	if !root.Expanded() {
		t.Fatal("toggle must expand a collapsed row")
	}
	tr.Toggle(root)
	if root.Expanded() {
		t.Fatal("toggle must collapse an expanded row")
	}
	tr.Toggle(root)
	tr.Wheel(3, 3)
	if tr.Top() != 3 {
		t.Fatalf("top = %d after wheel", tr.Top())
	}
	if tr.Cursor() < 3 {
		t.Fatalf("cursor %d left behind the window", tr.Cursor())
	}
}

func TestRenderWithoutPathOrLine(t *testing.T) {
	tr := &Tree[item]{}
	tr.Refresh([]Row[item]{
		{Entry: Entry{Name: "turn", Detail: "12:00"}, Item: item{"turn"}},
		{Entry: Entry{Name: "file", Path: "/p/x.go", Line: -1}, Item: item{"file"}},
		{Entry: Entry{Name: "at", Path: "/p/y.go", Line: 4}, Item: item{"at"}},
	}, func(it item) string { return it.name })
	got := render(tr, 5)
	want := []string{"▸ turn 12:00", "▸ file  /p/x.go", "▸ at  /p/y.go:5"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}
