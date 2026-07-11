package palette

import (
	"fmt"
	"testing"
)

// stubMode is a fixed-prefix Mode returning canned items regardless of query.
type stubMode struct {
	prefix rune
	items  []Item
}

func (s stubMode) Prefix() rune                   { return s.prefix }
func (s stubMode) Placeholder() string            { return "" }
func (s stubMode) Results(string, Context) []Item { return s.items }

func TestSearchAllInterleavesByScore(t *testing.T) {
	cmds := stubMode{prefix: ':', items: []Item{
		{Title: "Save All", Score: 90, Msg: RunCommandMsg{ID: "editor.saveAll"}},
		{Title: "Settings", Score: 40, Msg: RunCommandMsg{ID: "settings.open"}},
	}}
	files := stubMode{prefix: '@', items: []Item{
		{Title: "save.go", Score: 70, Msg: OpenFileMsg{Path: "save.go"}},
	}}
	m := NewSearchAllMode(cmds, files)
	items := m.Results("sa", Context{})
	want := []string{": Save All", "@ save.go", ": Settings"}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d", len(items), len(want))
	}
	for i, w := range want {
		if items[i].Title != w {
			t.Errorf("items[%d] = %q, want %q", i, items[i].Title, w)
		}
	}
}

func TestSearchAllTiesKeepCommandsFirst(t *testing.T) {
	cmds := stubMode{prefix: ':', items: []Item{{Title: "cmd", Score: 50}}}
	files := stubMode{prefix: '@', items: []Item{{Title: "file", Score: 50}}}
	items := NewSearchAllMode(cmds, files).Results("x", Context{})
	if len(items) != 2 || items[0].Title != ": cmd" || items[1].Title != "@ file" {
		t.Fatalf("tie order = %+v, want command before file", items)
	}
}

func TestSearchAllCapsPerKind(t *testing.T) {
	var many []Item
	for i := 0; i < searchAllPerKind+5; i++ {
		many = append(many, Item{Title: fmt.Sprintf("f%d.go", i), Score: 100 - i})
	}
	cmds := stubMode{prefix: ':', items: []Item{{Title: "cmd", Score: 1}}}
	files := stubMode{prefix: '@', items: many}
	items := NewSearchAllMode(cmds, files).Results("f", Context{})
	if len(items) != searchAllPerKind+1 {
		t.Fatalf("got %d items, want %d (files capped) + 1 command", len(items), searchAllPerKind+1)
	}
	// The command must survive even though every file outscores it.
	last := items[len(items)-1]
	if last.Title != ": cmd" {
		t.Fatalf("last item = %q, want the command row", last.Title)
	}
}

func TestSearchAllShiftsSpansForKindGlyph(t *testing.T) {
	files := stubMode{prefix: '@', items: []Item{{Title: "abc", Score: 1, Spans: []int{0, 2}}}}
	items := NewSearchAllMode(files).Results("ac", Context{})
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if got := items[0].Spans; len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Fatalf("spans = %v, want [2 4] (shifted past %q)", got, "@ ")
	}
}

func TestSearchAllPreservesUnderlyingMsgAndDetail(t *testing.T) {
	cmds := stubMode{prefix: ':', items: []Item{
		{Title: "Save All", Detail: "ctrl+shift+s", Score: 2, Msg: RunCommandMsg{ID: "editor.saveAll"}},
	}}
	files := stubMode{prefix: '@', items: []Item{
		{Title: "a/b.go", Score: 1, Msg: OpenFileMsg{Path: "/root/a/b.go"}},
	}}
	items := NewSearchAllMode(cmds, files).Results("q", Context{})
	if got, ok := items[0].Msg.(RunCommandMsg); !ok || got.ID != "editor.saveAll" {
		t.Fatalf("command msg = %+v, want RunCommandMsg{editor.saveAll}", items[0].Msg)
	}
	if items[0].Detail != "ctrl+shift+s" {
		t.Fatalf("command detail = %q, want the binding chip kept", items[0].Detail)
	}
	if got, ok := items[1].Msg.(OpenFileMsg); !ok || got.Path != "/root/a/b.go" {
		t.Fatalf("file msg = %+v, want OpenFileMsg{/root/a/b.go}", items[1].Msg)
	}
}

func TestSearchAllEmptyQueryListsRecentsFirst(t *testing.T) {
	cmds := stubMode{prefix: ':', items: []Item{{Title: "Some Command", Score: 0}}}
	files := stubMode{prefix: '@', items: []Item{{Title: "walk.go", Score: 0}}}
	rec := stubMode{prefix: '%', items: []Item{{Title: "recent.go", Score: 0, Msg: OpenFileMsg{Path: "recent.go"}}}}
	m := NewSearchAllMode(cmds, files)
	m.SetRecents(rec)

	items := m.Results("", Context{})
	if len(items) != 2 {
		t.Fatalf("empty query: got %d items, want recents + commands", len(items))
	}
	if items[0].Title != "% recent.go" || items[1].Title != ": Some Command" {
		t.Fatalf("empty query order = %q, %q; want recents first, commands after", items[0].Title, items[1].Title)
	}

	// A typed query composes commands and files as before — recents step aside.
	items = m.Results("x", Context{})
	for _, it := range items {
		if it.Title == "% recent.go" {
			t.Fatal("recents must not join ranked non-empty queries")
		}
	}
}

func TestSearchAllEmptyQueryWithoutRecentsKeepsListing(t *testing.T) {
	cmds := stubMode{prefix: ':', items: []Item{{Title: "Cmd", Score: 0}}}
	files := stubMode{prefix: '@', items: []Item{{Title: "f.go", Score: 0}}}
	m := NewSearchAllMode(cmds, files)
	m.SetRecents(stubMode{prefix: '%'}) // MRU empty (fresh session)

	items := m.Results("", Context{})
	if len(items) != 2 {
		t.Fatalf("empty MRU must fall back to the plain listing, got %d items", len(items))
	}
}
