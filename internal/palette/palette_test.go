package palette

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/plugin"
	"ike/internal/registry"
)

// fakeSource is an in-memory CommandSource for command-mode tests.
type fakeSource struct{ cmds []registry.OwnedCommand }

func (f fakeSource) Commands() []registry.OwnedCommand { return f.cmds }

func owned(id, title string, scope plugin.Scope) registry.OwnedCommand {
	return registry.OwnedCommand{Owner: "test", Command: plugin.Command{ID: id, Title: title, Scope: scope}}
}

func runes(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Text: s} }

// fileMode returns an "@" mode backed by a fixed file list (no disk walk).
func fileMode(paths ...string) *FileMode {
	return &FileMode{walk: func(string) []string { return paths }}
}

func TestPrefixRouting(t *testing.T) {
	cmd := NewCommandMode(fakeSource{}, nil, false)
	file := fileMode()
	p := New(Config{DefaultPrefix: ':'}, cmd, file)

	cases := []struct {
		query    string
		wantMode Mode
		wantBody string
	}{
		{":write", cmd, "write"},
		{"@app", file, "app"},
		{"hello", cmd, "hello"}, // no prefix → default mode, whole query
		{"", cmd, ""},
	}
	for _, tc := range cases {
		p.query = tc.query
		m, body := p.mode()
		if m != tc.wantMode {
			t.Errorf("query %q: wrong mode", tc.query)
		}
		if body != tc.wantBody {
			t.Errorf("query %q: body = %q, want %q", tc.query, body, tc.wantBody)
		}
	}
}

func TestCommandContextRanking(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		owned("z.global", "Global Thing", plugin.GlobalScope()),
		owned("a.editor", "Editor Thing", plugin.PaneScope("editor")),
		owned("m.explorer", "Explorer Thing", plugin.PaneScope("explorer")),
	}}
	cmd := NewCommandMode(src, nil, false)

	items := cmd.Results("", Context{ContextID: "editor"})
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	// In-context (editor) first, then global, then off-context (explorer).
	want := []string{"Editor Thing", "Global Thing", "Explorer Thing"}
	for i, w := range want {
		if items[i].Title != w {
			t.Fatalf("rank %d = %q, want %q (%v)", i, items[i].Title, w,
				[]string{items[0].Title, items[1].Title, items[2].Title})
		}
	}
}

func TestCommandHideOffContext(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		owned("a.editor", "Editor Thing", plugin.PaneScope("editor")),
		owned("m.explorer", "Explorer Thing", plugin.PaneScope("explorer")),
	}}
	cmd := NewCommandMode(src, nil, true) // hide off-context

	items := cmd.Results("", Context{ContextID: "editor"})
	if len(items) != 1 || items[0].Title != "Editor Thing" {
		t.Fatalf("off-context command not hidden: %+v", items)
	}
}

func TestCommandActivateDispatch(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		owned("example.hello", "Say Hello", plugin.GlobalScope()),
		owned("example.bye", "Say Goodbye", plugin.GlobalScope()),
	}}
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(src, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{ContextID: "editor"})

	// Filter down to "hello".
	p.Update(runes(":hello"))
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should emit a command")
	}
	msg := cmd()
	run, ok := msg.(RunCommandMsg)
	if !ok {
		t.Fatalf("want RunCommandMsg, got %T", msg)
	}
	if run.ID != "example.hello" {
		t.Fatalf("activated %q, want example.hello", run.ID)
	}
	if p.IsOpen() {
		t.Fatal("palette should close after activation")
	}
}

func TestFileOpenMsg(t *testing.T) {
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(fakeSource{}, nil, false),
		fileMode("main.go", "internal/app/app.go"))
	p.SetSize(80, 24)
	p.Open(Context{ContextID: "editor", Root: "."})

	p.Update(runes("@app/app"))
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should emit an open-file command")
	}
	open, ok := cmd().(OpenFileMsg)
	if !ok {
		t.Fatalf("want OpenFileMsg, got %T", cmd())
	}
	if open.Path != "internal/app/app.go" {
		t.Fatalf("opened %q, want internal/app/app.go", open.Path)
	}
}

func TestEscCloses(t *testing.T) {
	p := New(Config{}, NewCommandMode(fakeSource{}, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{})
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Fatal("esc should not emit a command")
	}
	if p.IsOpen() {
		t.Fatal("esc should close the palette")
	}
}

func TestNavigation(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		owned("a", "Alpha", plugin.GlobalScope()),
		owned("b", "Bravo", plugin.GlobalScope()),
		owned("c", "Charlie", plugin.GlobalScope()),
	}}
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(src, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{ContextID: "editor"})

	if p.selected != 0 {
		t.Fatalf("initial selection = %d, want 0", p.selected)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if p.selected != 2 {
		t.Fatalf("after two downs selection = %d, want 2", p.selected)
	}
	// Clamp at the bottom.
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if p.selected != 2 {
		t.Fatalf("selection should clamp at 2, got %d", p.selected)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.selected != 1 {
		t.Fatalf("after up selection = %d, want 1", p.selected)
	}
	// Typing resets selection to the top.
	p.Update(runes("x"))
	if p.selected != 0 {
		t.Fatalf("typing should reset selection, got %d", p.selected)
	}
}

func TestLockedFileModeNoSwitching(t *testing.T) {
	fm := fileMode("a.go", "b.txt")
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(fakeSource{}, nil, false), fm)
	p.SetSize(80, 24)
	p.OpenAnchored(Context{Root: "."}, '@', 5, 5, 40)

	if !p.Anchored() {
		t.Fatal("OpenAnchored should anchor the box")
	}
	if x, y := p.AnchorPos(); x != 5 || y != 5 {
		t.Fatalf("anchor = (%d,%d), want (5,5)", x, y)
	}
	if m, _ := p.mode(); m != Mode(fm) {
		t.Fatal("should be locked to file mode")
	}
	// A leading ":" must not switch modes while locked — it is part of the query.
	p.Update(runes(":x"))
	m, body := p.mode()
	if m != Mode(fm) {
		t.Fatal("locked mode must not switch on ':'")
	}
	if body != ":x" {
		t.Fatalf("locked body = %q, want \":x\"", body)
	}
}

func TestOpenLockedCenteredFileMode(t *testing.T) {
	fm := fileMode("a.go", "b.txt")
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(fakeSource{}, nil, false), fm)
	p.SetSize(80, 24)
	p.OpenLocked(Context{Root: "."}, '@')

	if p.Anchored() {
		t.Fatal("OpenLocked should stay centered, not anchored")
	}
	if m, _ := p.mode(); m != Mode(fm) {
		t.Fatal("should be locked to file mode")
	}
	// A leading ":" must not switch modes while locked — it is part of the query.
	p.Update(runes(":x"))
	if m, _ := p.mode(); m != Mode(fm) {
		t.Fatal("locked mode must not switch on ':'")
	}
	// An unknown prefix falls back to the default centered palette, unlocked.
	p.OpenLocked(Context{Root: "."}, '!')
	if p.locked != nil {
		t.Fatal("unknown prefix should fall back to the unlocked palette")
	}
}

func TestNoResults(t *testing.T) {
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(fakeSource{}, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{ContextID: "editor"})
	p.Update(runes(":zzz"))
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter with no results should be a no-op")
	}
	if !p.IsOpen() {
		t.Fatal("palette should stay open when activating nothing")
	}
}

// liveStub is a LiveMode whose QueryChanged records the settled queries.
type liveStub struct {
	rows    []Item
	queries []string
}

func (l *liveStub) Prefix() rune        { return '$' }
func (l *liveStub) Placeholder() string { return "live…" }
func (l *liveStub) Results(q string, cx Context) []Item {
	return l.rows
}
func (l *liveStub) QueryChanged(q string, cx Context) tea.Cmd {
	l.queries = append(l.queries, q)
	return nil
}

// TestLiveModeDebounce guards the #295 plumbing: query edits schedule a
// debounce tick, only the newest generation re-queries, and Refresh
// recomputes rows from the mode's cache.
func TestLiveModeDebounce(t *testing.T) {
	live := &liveStub{}
	p := New(Config{}, live)
	p.SetSize(80, 24)
	p.OpenLocked(Context{}, '$')

	var tick tea.Cmd
	for _, r := range "ab" {
		tick = p.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	if tick == nil {
		t.Fatal("editing a live mode's query must schedule a tick")
	}
	// The first edit's tick is stale; only gen 2 re-queries.
	if cmd := p.LiveTick(LiveTickMsg{Gen: 1}); cmd != nil {
		t.Fatal("a stale tick must be dropped")
	}
	p.LiveTick(LiveTickMsg{Gen: 2})
	if len(live.queries) != 1 || live.queries[0] != "ab" {
		t.Fatalf("the settled tick must re-query once, got %v", live.queries)
	}

	// Fresh rows land in the cache; Refresh makes them visible.
	live.rows = []Item{{Title: "alpha"}}
	p.Refresh()
	if len(p.items) != 1 || p.items[0].Title != "alpha" {
		t.Fatalf("Refresh must recompute from the cache, got %+v", p.items)
	}
}

// completerMode is a Mode implementing Completer for tab tests (#542).
type completerMode struct{ out string }

func (m completerMode) Prefix() rune                   { return '#' }
func (m completerMode) Placeholder() string            { return "" }
func (m completerMode) Results(string, Context) []Item { return nil }
func (m completerMode) Complete(query string) string {
	if m.out == "" {
		return query
	}
	return m.out
}

func TestTabAsksModeToComplete(t *testing.T) {
	p := New(Config{}, completerMode{out: "~/Development/"})
	p.SetSize(80, 24)
	p.Open(Context{})
	p.Update(runes("#~/Dev"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.query != "#~/Development/" {
		t.Fatalf("tab must extend the query body keeping the prefix, got %q", p.query)
	}
}

func TestTabInertWithoutCompletion(t *testing.T) {
	p := New(Config{}, completerMode{})
	p.SetSize(80, 24)
	p.Open(Context{})
	p.Update(runes("#~/zzz"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.query != "#~/zzz" {
		t.Fatalf("tab with nothing to complete must be inert, got %q", p.query)
	}
}

func TestTabInertOnNonCompleterMode(t *testing.T) {
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(fakeSource{}, nil, false))
	p.SetSize(80, 24)
	p.Open(Context{})
	p.Update(runes(":wri"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.query != ":wri" {
		t.Fatalf("tab on a non-completer mode must be inert, got %q", p.query)
	}
}
