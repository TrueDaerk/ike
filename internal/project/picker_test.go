package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/palette"
	"ike/internal/registry"
)

// --- command.go ---

func TestSwitchCommandRegistered(t *testing.T) {
	r := registry.New()
	r.Add(commands{})

	c, ok := r.Command("project.switch")
	if !ok {
		t.Fatal("project.switch should be registered")
	}
	if c.Owner != "project" || c.Title == "" || c.Run == nil || !c.Scope.Global {
		t.Errorf("command wrong: %+v", c)
	}
	if keys, ok := r.Binding("project.switch"); !ok || keys != "cmd+shift+p" {
		t.Errorf("default keymap slot missing, got %q, %v", keys, ok)
	}
	if len(r.Conflicts()) != 0 {
		t.Errorf("registration should be conflict-free: %v", r.Conflicts())
	}
}

func TestGlobalRegistryHasSwitchCommand(t *testing.T) {
	// init() registers into the process-wide registry the real app queries.
	if _, ok := registry.Global().Command("project.switch"); !ok {
		t.Fatal("project.switch should self-register via init()")
	}
}

// --- picker.go ---

func fixedHistory() []Entry {
	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	return []Entry{
		{Path: "/code/ike", Name: "ike", LastOpened: t0.Add(2 * time.Hour)},
		{Path: "/code/website", Name: "website", LastOpened: t0.Add(time.Hour)},
		{Path: "/work/intra", Name: "intra", LastOpened: t0},
	}
}

// newPicker builds a PickerMode over history with its projects base pinned
// to a fresh empty temp dir (#1808), so a query never accidentally resolves
// against whatever the test machine's real projects directory happens to
// contain.
func newPicker(t *testing.T, history func() []Entry) (*PickerMode, string) {
	t.Helper()
	base := t.TempDir()
	m := NewPickerMode(history)
	m.projectsDir = func() (string, error) { return base, nil }
	return m, base
}

func pickerItems(t *testing.T, query string) []palette.Item {
	m, _ := newPicker(t, fixedHistory)
	return m.Results(query, palette.Context{})
}

func TestPickerEmptyQueryListsNewestFirst(t *testing.T) {
	items := pickerItems(t, "")
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %+v", items)
	}
	want := []string{"ike", "website", "intra"}
	for i, w := range want {
		if items[i].Title != w {
			t.Errorf("items[%d] = %q, want %q", i, items[i].Title, w)
		}
	}
	if msg, ok := items[0].Msg.(PickedMsg); !ok || msg.Path != "/code/ike" {
		t.Errorf("item msg should carry the entry path, got %#v", items[0].Msg)
	}
	if items[0].Detail != "/code/ike" {
		t.Errorf("detail should show the path, got %q", items[0].Detail)
	}
}

func TestPickerFuzzyFiltersAndAppendsPathItem(t *testing.T) {
	m, base := newPicker(t, fixedHistory)
	items := m.Results("ik", palette.Context{})
	// "ik" matches ike by name (and possibly others by path); the raw-path
	// affordance is always last.
	if len(items) < 2 {
		t.Fatalf("expected matches plus path item, got %+v", items)
	}
	if items[0].Title != "ike" || len(items[0].Spans) == 0 {
		t.Errorf("best match should be ike with highlight spans, got %+v", items[0])
	}
	last := items[len(items)-1]
	if last.Title != "Open \"ik\"…" {
		t.Errorf("last item should be the path affordance, got %q", last.Title)
	}
	// A relative query resolves against the configured projects directory,
	// not the raw typed text (#1808).
	want := filepath.Join(base, "ik")
	if msg, ok := last.Msg.(PickedMsg); !ok || msg.Path != want {
		t.Errorf("path item should resolve against the projects dir, got %#v, want %q", last.Msg, want)
	}
}

func TestPickerMatchesPathWhenNameMisses(t *testing.T) {
	items := pickerItems(t, "work")
	if len(items) != 2 { // intra (path match) + path affordance
		t.Fatalf("expected path match + affordance, got %+v", items)
	}
	if items[0].Title != "intra" || len(items[0].Spans) != 0 {
		t.Errorf("path match should list intra without name spans, got %+v", items[0])
	}
}

func TestPickerNoHistoryStillOffersPathEntry(t *testing.T) {
	m, _ := newPicker(t, func() []Entry { return nil })
	if items := m.Results("", palette.Context{}); len(items) != 0 {
		t.Errorf("empty query, empty history should list nothing, got %+v", items)
	}
	items := m.Results("/some/dir", palette.Context{})
	if len(items) != 1 {
		t.Fatalf("typed path should yield the affordance, got %+v", items)
	}
	if msg, ok := items[0].Msg.(PickedMsg); !ok || msg.Path != "/some/dir" {
		t.Errorf("absolute query must resolve unchanged, got %#v", items[0].Msg)
	}
}

func TestCompactPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := CompactPath(home + "/code/ike"); got != "~/code/ike" {
		t.Errorf("home should collapse to ~, got %q", got)
	}
	long := "/very" + strings.Repeat("/deeply/nested", 6) + "/proj"
	got := CompactPath(long)
	if len([]rune(got)) > maxDetailWidth {
		t.Errorf("compacted path too wide: %q", got)
	}
	if !strings.Contains(got, "…") || !strings.HasSuffix(got, "/proj") {
		t.Errorf("middle truncation should keep head and tail, got %q", got)
	}
}

func TestPickerDefaultsToLiveConfig(t *testing.T) {
	// NewPickerMode(nil) must not panic and reads the process-wide config.
	m := NewPickerMode(nil)
	_ = m.Results("", palette.Context{})
	if m.Prefix() != PickerPrefix || m.Placeholder() == "" {
		t.Errorf("mode metadata wrong: %q %q", m.Prefix(), m.Placeholder())
	}
}

// pickerTree builds a fixture with Development/ and Downloads/ directories
// plus a stray file.
func pickerTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"Development", "Downloads"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPickerPathQueryListsDirectories(t *testing.T) {
	root := pickerTree(t)
	m := NewPickerMode(func() []Entry { return nil })
	q := filepath.Join(root, "D")
	items := m.Results(q, palette.Context{})
	// Two directory candidates + the raw affordance last; the file is not offered.
	if len(items) != 3 {
		t.Fatalf("items = %+v, want 2 dir candidates + affordance", items)
	}
	wantFirst := "Open " + filepath.Join(root, "Development") + string(filepath.Separator)
	if items[0].Title != wantFirst {
		t.Fatalf("items[0] = %q, want %q", items[0].Title, wantFirst)
	}
	if msg, ok := items[0].Msg.(PickedMsg); !ok || msg.Path != filepath.Join(root, "Development")+string(filepath.Separator) {
		t.Fatalf("candidate msg = %#v", items[0].Msg)
	}
	if items[2].Title != "Open \""+q+"\"…" {
		t.Fatalf("raw affordance must stay last, got %q", items[2].Title)
	}
}

func TestPickerRelativeQueryNoMatchOffersOnlyAffordance(t *testing.T) {
	m, base := newPicker(t, func() []Entry { return nil })
	items := m.Results("ike", palette.Context{})
	if len(items) != 1 {
		t.Fatalf("query with no matching dir must only offer the raw affordance, got %+v", items)
	}
	want := filepath.Join(base, "ike")
	if msg, ok := items[0].Msg.(PickedMsg); !ok || msg.Path != want {
		t.Fatalf("affordance should resolve against the projects dir, got %#v, want %q", items[0].Msg, want)
	}
}

// TestPickerRelativeQueryBrowsesProjectsDir (#1808): a bare or dot-relative
// query browses the configured projects directory rather than the process
// working directory — the picker's own equivalent of newproject_prompt.go
// and clone_prompt.go, which already default there.
func TestPickerRelativeQueryBrowsesProjectsDir(t *testing.T) {
	base := pickerTree(t)
	m, _ := newPicker(t, func() []Entry { return nil })
	m.projectsDir = func() (string, error) { return base, nil }

	for _, q := range []string{"D", "./D"} {
		items := m.Results(q, palette.Context{})
		if len(items) != 3 { // Development + Downloads + affordance
			t.Fatalf("q=%q: items = %+v, want 2 dir candidates + affordance", q, items)
		}
		wantPath := filepath.Join(base, "Development")
		if msg, ok := items[0].Msg.(PickedMsg); !ok || msg.Path != wantPath {
			t.Fatalf("q=%q: candidate msg = %#v, want %q", q, items[0].Msg, wantPath)
		}
	}
}

// TestPickerAbsoluteAndHomeQueriesIgnoreProjectsDir (#1808): absolute and
// ~-prefixed input keeps browsing wherever it points, unaffected by the
// configured projects directory.
func TestPickerAbsoluteAndHomeQueriesIgnoreProjectsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, base := newPicker(t, func() []Entry { return nil }) // base != home

	items := m.Results("/abs/path", palette.Context{})
	if msg, ok := items[len(items)-1].Msg.(PickedMsg); !ok || msg.Path != "/abs/path" {
		t.Fatalf("absolute query must resolve unchanged, got %#v", items[len(items)-1].Msg)
	}

	items = m.Results("~/elsewhere", palette.Context{})
	last := items[len(items)-1] // history-fuzzy items, if any, precede the raw affordance
	want := filepath.Join(home, "elsewhere")
	if msg, ok := last.Msg.(PickedMsg); !ok || msg.Path != want {
		t.Fatalf("~ query must resolve against home, not %q, got %#v, want %q", base, last.Msg, want)
	}
}

func TestPickerComplete(t *testing.T) {
	root := pickerTree(t)
	m := NewPickerMode(func() []Entry { return nil })
	got := m.Complete(filepath.Join(root, "Dev"))
	if want := filepath.Join(root, "Development") + string(filepath.Separator); got != want {
		t.Fatalf("Complete = %q, want %q", got, want)
	}

	// A relative query completes against the configured projects dir (#1808).
	m.projectsDir = func() (string, error) { return root, nil }
	if got := m.Complete("Dev"); got != "Development"+string(filepath.Separator) {
		t.Fatalf("relative query should complete against the projects dir, got %q", got)
	}
	if got := m.Complete("nope"); got != "nope" {
		t.Fatalf("no match must complete to itself, got %q", got)
	}
}

// TestPickerShowsLastOpenedTime (#842, #1114): rows carry the relative
// last-opened time in the right-aligned Time column; open workspaces keep
// their ● dot as the badge next to the name.
func TestPickerShowsLastOpenedTime(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Name: "alpha", Path: "/p/alpha", LastOpened: now.Add(-2 * time.Hour)},
		{Name: "beta", Path: "/p/beta", LastOpened: now.Add(-3 * 24 * time.Hour)},
	}
	pm := NewPickerMode(func() []Entry { return entries })
	pm.now = func() time.Time { return now }
	pm.SetOpen(func(path string) bool { return path == "/p/alpha" })

	items := pm.Results("", palette.Context{})
	if items[0].Badge != "●" || items[0].Time != "2h ago" {
		t.Fatalf("open entry badge/time = %q/%q, want \"●\"/\"2h ago\"", items[0].Badge, items[0].Time)
	}
	if items[1].Badge != "" || items[1].Time != "3d ago" {
		t.Fatalf("entry badge/time = %q/%q, want \"\"/\"3d ago\"", items[1].Badge, items[1].Time)
	}
	if aux, ok := items[1].Aux.(RemoveFromHistoryMsg); !ok || aux.Path != "/p/beta" {
		t.Fatalf("unloaded entry aux = %#v, want RemoveFromHistoryMsg", items[1].Aux)
	}
	// #1418: the close action carries its distinguishing glyph, removal the
	// default (empty = "✕").
	if aux, ok := items[0].Aux.(CloseWorkspaceMsg); !ok || aux.Path != "/p/alpha" {
		t.Fatalf("open entry aux = %#v, want CloseWorkspaceMsg", items[0].Aux)
	}
	if items[0].AuxGlyph != CloseAuxGlyph {
		t.Fatalf("open entry aux glyph = %q, want %q", items[0].AuxGlyph, CloseAuxGlyph)
	}
	if items[1].AuxGlyph != "" {
		t.Fatalf("unloaded entry aux glyph = %q, want default", items[1].AuxGlyph)
	}
}
