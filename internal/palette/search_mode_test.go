package palette

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestSearchAllComparableScoresFollowSourceTier(t *testing.T) {
	// Scores within one band (searchAllScoreBand) count as comparable: the
	// source order decides — command > file > symbol (#1421) — even when the
	// lower-tier match scores slightly higher.
	cmds := stubMode{prefix: ':', items: []Item{{Title: "cmd", Score: 48}}}
	files := stubMode{prefix: '@', items: []Item{{Title: "file.go", Score: 55}}}
	syms := stubMode{prefix: '#', items: []Item{{Title: "sym", Score: 52}}}
	items := NewSearchAllMode(cmds, files, syms).Results("x", Context{})
	want := []string{": cmd", "@ file.go", "# sym"}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d", len(items), len(want))
	}
	for i, w := range want {
		if items[i].Title != w {
			t.Errorf("items[%d] = %q, want %q", i, items[i].Title, w)
		}
	}
}

func TestSearchAllClearlyStrongerMatchBeatsTier(t *testing.T) {
	// A full band apart is no longer comparable: the stronger file match
	// outranks the weak command match despite the lower tier.
	cmds := stubMode{prefix: ':', items: []Item{{Title: "cmd", Score: 20}}}
	files := stubMode{prefix: '@', items: []Item{{Title: "file.go", Score: 20 + 2*searchAllScoreBand}}}
	items := NewSearchAllMode(cmds, files).Results("x", Context{})
	if len(items) != 2 || items[0].Title != "@ file.go" || items[1].Title != ": cmd" {
		t.Fatalf("order = %+v, want the clearly stronger file first", items)
	}
}

func TestSearchAllSymbolsRankAfterFilesInBand(t *testing.T) {
	files := stubMode{prefix: '@', items: []Item{{Title: "file.go", Score: 50}}}
	syms := stubMode{prefix: '#', items: []Item{{Title: "sym", Score: 50}}}
	items := NewSearchAllMode(files, syms).Results("x", Context{})
	if len(items) != 2 || items[0].Title != "@ file.go" || items[1].Title != "# sym" {
		t.Fatalf("order = %+v, want file before symbol on equal scores", items)
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

// recordingMode is a Mode that remembers the last query body it was asked for,
// so prefix stripping can be asserted.
type recordingMode struct {
	prefix rune
	items  []Item
	last   string
	live   []string // queries seen through QueryChanged
}

func (r *recordingMode) Prefix() rune        { return r.prefix }
func (r *recordingMode) Placeholder() string { return "" }
func (r *recordingMode) Results(query string, _ Context) []Item {
	r.last = query
	return r.items
}
func (r *recordingMode) QueryChanged(query string, _ Context) tea.Cmd {
	r.live = append(r.live, query)
	return nil
}

func TestSearchAllCommandPrefixScopesToCommands(t *testing.T) {
	cmds := &recordingMode{prefix: ':', items: []Item{{Title: "Save All", Score: 90}}}
	files := &recordingMode{prefix: '@', items: []Item{{Title: "save.go", Score: 95}}}
	items := NewSearchAllMode(cmds, files).Results(":sa", Context{})
	if len(items) != 1 || items[0].Title != ": Save All" {
		t.Fatalf("items = %+v, want only the command row", items)
	}
	if cmds.last != "sa" {
		t.Errorf("command mode queried with %q, want the prefix stripped", cmds.last)
	}
}

func TestSearchAllFilePrefixScopesToFiles(t *testing.T) {
	cmds := &recordingMode{prefix: ':', items: []Item{{Title: "Save All", Score: 90}}}
	files := &recordingMode{prefix: '@', items: []Item{{Title: "save.go", Score: 10}}}
	syms := &recordingMode{prefix: '$', items: []Item{{Title: "Save", Score: 99}}}
	items := NewSearchAllMode(cmds, files, syms).Results("@save", Context{})
	if len(items) != 1 || items[0].Title != "@ save.go" {
		t.Fatalf("items = %+v, want only the file row", items)
	}
	if files.last != "save" {
		t.Errorf("file mode queried with %q, want the prefix stripped", files.last)
	}
}

func TestSearchAllPrefixOnlyQueryListsThatSource(t *testing.T) {
	cmds := &recordingMode{prefix: ':', items: []Item{{Title: "Cmd A"}, {Title: "Cmd B"}}}
	files := &recordingMode{prefix: '@', items: []Item{{Title: "f.go"}}}
	items := NewSearchAllMode(cmds, files).Results(":", Context{})
	if len(items) != 2 || items[0].Title != ": Cmd A" || items[1].Title != ": Cmd B" {
		t.Fatalf("items = %+v, want the full command listing", items)
	}
	if cmds.last != "" {
		t.Errorf("command mode queried with %q, want the empty body", cmds.last)
	}
}

func TestSearchAllScopedResultsAreNotCapped(t *testing.T) {
	var many []Item
	for i := 0; i < searchAllPerKind+5; i++ {
		many = append(many, Item{Title: fmt.Sprintf("f%d.go", i), Score: 100 - i})
	}
	files := &recordingMode{prefix: '@', items: many}
	items := NewSearchAllMode(files).Results("@f", Context{})
	if len(items) != len(many) {
		t.Fatalf("got %d items, want all %d (no per-kind cap when scoped)", len(items), len(many))
	}
}

func TestSearchAllUnknownPrefixStaysLiteral(t *testing.T) {
	cmds := &recordingMode{prefix: ':', items: []Item{{Title: "Cmd", Score: 1}}}
	files := &recordingMode{prefix: '@', items: []Item{{Title: "f.go", Score: 1}}}
	items := NewSearchAllMode(cmds, files).Results("?x", Context{})
	if len(items) != 2 {
		t.Fatalf("got %d items, want both sources composed", len(items))
	}
	if cmds.last != "?x" || files.last != "?x" {
		t.Errorf("queries = %q/%q, want the raw query passed through", cmds.last, files.last)
	}
}

func TestSearchAllScopedQueryChangedForwardsStrippedBodyOnly(t *testing.T) {
	cmds := &recordingMode{prefix: ':'}
	syms := &recordingMode{prefix: '$'}
	m := NewSearchAllMode(cmds, syms)

	m.QueryChanged("$sym", Context{})
	if len(syms.live) != 1 || syms.live[0] != "sym" {
		t.Fatalf("symbol queries = %v, want [sym]", syms.live)
	}
	if len(cmds.live) != 0 {
		t.Fatalf("command queries = %v, want none while scoped to symbols", cmds.live)
	}

	// Scoped to a non-live source: nothing re-queries.
	m.QueryChanged(":run", Context{})
	if len(syms.live) != 1 {
		t.Fatalf("symbol queries = %v, want no extra re-query while scoped to commands", syms.live)
	}
	if len(cmds.live) != 1 || cmds.live[0] != "run" {
		t.Fatalf("command queries = %v, want [run]", cmds.live)
	}

	// Unscoped: every live source sees the raw query.
	m.QueryChanged("plain", Context{})
	if len(syms.live) != 2 || syms.live[1] != "plain" {
		t.Fatalf("symbol queries = %v, want the raw query forwarded", syms.live)
	}
}
