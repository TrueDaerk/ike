package menu

import "testing"

// TestCommandPaths (#2548): every referenced command id maps to its "Menu ›
// Entry" path, the first occurrence wins for an id listed twice, and empty
// ids (separators) are skipped.
func TestCommandPaths(t *testing.T) {
	menus := []Menu{
		{Title: "File", Items: []Item{
			{Title: "Switch Project", Command: "project.switch"},
			{Title: "", Command: ""},
		}},
		{Title: "View", Items: []Item{
			{Title: "Projects…", Command: "project.switch"},
			{Title: "Terminal", Command: "terminal.toggle"},
		}},
	}
	got := CommandPaths(menus)
	if want := "File" + PathSeparator + "Switch Project"; got["project.switch"] != want {
		t.Fatalf("project.switch = %q, want %q", got["project.switch"], want)
	}
	if want := "View" + PathSeparator + "Terminal"; got["terminal.toggle"] != want {
		t.Fatalf("terminal.toggle = %q, want %q", got["terminal.toggle"], want)
	}
	if _, ok := got[""]; ok || len(got) != 2 {
		t.Fatalf("paths = %v, want exactly the two referenced ids", got)
	}
}

// TestDefaultsCommandPathsCoverEveryEntry: the shipped menus yield a path for
// each of their command ids, so the palette's fallback can match every menu
// entry by the words the menu bar uses.
func TestDefaultsCommandPathsCoverEveryEntry(t *testing.T) {
	paths := CommandPaths(Defaults())
	for _, m := range Defaults() {
		for _, it := range m.Items {
			if it.Command == "" {
				continue
			}
			if paths[it.Command] == "" {
				t.Errorf("%s › %s (%s) has no path", m.Title, it.Title, it.Command)
			}
		}
	}
}
