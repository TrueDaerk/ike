package palette

import (
	"path/filepath"
	"testing"
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
