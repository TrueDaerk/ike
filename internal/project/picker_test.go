package project

import (
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
	if keys, ok := r.Binding("project.switch"); !ok || keys != "alt+shift+p" {
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
	if got := compactPath(home + "/code/ike"); got != "~/code/ike" {
		t.Errorf("home should collapse to ~, got %q", got)
	}
	long := "/very" + strings.Repeat("/deeply/nested", 6) + "/proj"
	got := compactPath(long)
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
