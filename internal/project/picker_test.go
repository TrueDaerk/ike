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

func pickerItems(query string) []palette.Item {
	m := NewPickerMode(func() []Entry { return fixedHistory() })
	return m.Results(query, palette.Context{})
}

func TestPickerEmptyQueryListsNewestFirst(t *testing.T) {
	items := pickerItems("")
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
	items := pickerItems("ik")
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
	if msg, ok := last.Msg.(PickedMsg); !ok || msg.Path != "ik" {
		t.Errorf("path item should carry the raw query, got %#v", last.Msg)
	}
}

func TestPickerMatchesPathWhenNameMisses(t *testing.T) {
	items := pickerItems("work")
	if len(items) != 2 { // intra (path match) + path affordance
		t.Fatalf("expected path match + affordance, got %+v", items)
	}
	if items[0].Title != "intra" || len(items[0].Spans) != 0 {
		t.Errorf("path match should list intra without name spans, got %+v", items[0])
	}
}

func TestPickerNoHistoryStillOffersPathEntry(t *testing.T) {
	m := NewPickerMode(func() []Entry { return nil })
	if items := m.Results("", palette.Context{}); len(items) != 0 {
		t.Errorf("empty query, empty history should list nothing, got %+v", items)
	}
	items := m.Results("/some/dir", palette.Context{})
	if len(items) != 1 {
		t.Fatalf("typed path should yield the affordance, got %+v", items)
	}
	if msg, ok := items[0].Msg.(PickedMsg); !ok || msg.Path != "/some/dir" {
		t.Errorf("affordance should carry the typed path, got %#v", items[0].Msg)
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

func TestPickerNonPathQueryHasNoDirCandidates(t *testing.T) {
	m := NewPickerMode(func() []Entry { return nil })
	items := m.Results("ike", palette.Context{})
	if len(items) != 1 {
		t.Fatalf("non-path query must only offer the raw affordance, got %+v", items)
	}
}

func TestPickerComplete(t *testing.T) {
	root := pickerTree(t)
	m := NewPickerMode(func() []Entry { return nil })
	got := m.Complete(filepath.Join(root, "Dev"))
	if want := filepath.Join(root, "Development") + string(filepath.Separator); got != want {
		t.Fatalf("Complete = %q, want %q", got, want)
	}
	if got := m.Complete("ike"); got != "ike" {
		t.Fatalf("non-path query must complete to itself, got %q", got)
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
}
