package palette

import (
	"charm.land/lipgloss/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/ui"
)

// fakeSource is an in-memory CommandSource for command-mode tests.
type fakeSource struct{ cmds []registry.OwnedCommand }

func (f fakeSource) Commands() []registry.OwnedCommand { return f.cmds }

func owned(id, title string, scope plugin.Scope) registry.OwnedCommand {
	return registry.OwnedCommand{Owner: "test", Command: plugin.Command{ID: id, Title: title, Scope: scope}}
}

func runes(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Text: s} }

// fileMode returns an "@" mode backed by a fixed file list (no disk walk) and
// with the home-anchored filesystem fallback disabled (#1775), so results stay
// independent of the machine the test runs on. Tests that exercise the home
// fallback set the anchor themselves.
func fileMode(paths ...string) *FileMode {
	return &FileMode{walk: func(string) []string { return paths }, home: func() string { return "" }}
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
		p.query.Set(tc.query)
		m, body := p.mode()
		if m != tc.wantMode {
			t.Errorf("query %q: wrong mode", tc.query)
		}
		if body != tc.wantBody {
			t.Errorf("query %q: body = %q, want %q", tc.query, body, tc.wantBody)
		}
	}
}

// TestNewPanicsOnDuplicatePrefix guards against a mode silently overriding
// another's registration in byPrefix (#1878): '%' collided between
// RecentMode and the app's http-env mode, so cmd+e opened the wrong picker
// with no test catching it. New must now fail loudly instead.
func TestNewPanicsOnDuplicatePrefix(t *testing.T) {
	cmd := NewCommandMode(fakeSource{}, nil, false)
	dup := stubMode{prefix: ':'}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("New did not panic on duplicate prefix")
		}
	}()
	New(Config{}, cmd, dup)
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

// TestCommandLanguageGateRanksOffContext (#2483): a file-type-gated command
// (plugin.Command.Languages) ranks off-context unless the focused buffer's
// language matches its gate — the same applicability the help overlay shows.
func TestCommandLanguageGateRanksOffContext(t *testing.T) {
	jq := owned("json.jqPlayground", "jq Playground", plugin.GlobalScope())
	jq.Languages = []string{"json", "jsonc"}
	src := fakeSource{cmds: []registry.OwnedCommand{
		jq,
		owned("g.global", "Global Thing", plugin.GlobalScope()),
	}}
	cmd := NewCommandMode(src, nil, false)

	// Over a Go buffer the gated command sinks below the plain global one…
	items := cmd.Results("", Context{ContextID: "editor", Lang: "go"})
	if len(items) != 2 || items[0].Title != "Global Thing" {
		t.Fatalf("gated command should rank last over go: %+v", items)
	}
	// …and over a JSON buffer it ranks as the global command it is: both sit
	// in the global tier, ordered by title.
	items = cmd.Results("", Context{ContextID: "editor", Lang: "json"})
	if len(items) != 2 || items[0].Title != "Global Thing" || items[1].Title != "jq Playground" {
		t.Fatalf("want both commands in the global tier over json: %+v", items)
	}
	if hidden := NewCommandMode(src, nil, true).Results("", Context{ContextID: "editor", Lang: "go"}); len(hidden) != 1 {
		t.Fatalf("hideOff must drop the non-matching gated command: %+v", hidden)
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

// TestFileUsageMarking guards #1419: only selections from the two ranked
// windows — the unlocked palette's "@" source and Search Everywhere — carry
// CountUsage; the locked/anchored finders never do.
func TestFileUsageMarking(t *testing.T) {
	newP := func() *Palette {
		fm := fileMode("main.go")
		all := NewSearchAllMode(NewCommandMode(fakeSource{}, nil, false), fm)
		p := New(Config{DefaultPrefix: ':'}, NewCommandMode(fakeSource{}, nil, false), fm, all)
		p.SetSize(80, 24)
		return p
	}
	cx := Context{ContextID: "editor", Root: "."}

	activate := func(t *testing.T, p *Palette) OpenFileMsg {
		t.Helper()
		cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("enter should emit an open-file command")
		}
		open, ok := cmd().(OpenFileMsg)
		if !ok {
			t.Fatalf("want OpenFileMsg, got %T", cmd())
		}
		return open
	}

	// Run a Command window, "@" source: counts.
	p := newP()
	p.Open(cx)
	p.Update(runes("@main"))
	if !activate(t, p).CountUsage {
		t.Fatal("unlocked palette file selection must set CountUsage")
	}

	// Search Everywhere: counts.
	p = newP()
	p.OpenLocked(cx, SearchAllPrefix)
	p.Update(runes("main"))
	if !activate(t, p).CountUsage {
		t.Fatal("search-everywhere file selection must set CountUsage")
	}

	// Locked "@" finder (go-to-file): never counts.
	p = newP()
	p.OpenLocked(cx, '@')
	p.Update(runes("main"))
	if activate(t, p).CountUsage {
		t.Fatal("locked go-to-file selection must not set CountUsage")
	}

	// Anchored "@" finder: never counts.
	p = newP()
	p.OpenAnchored(cx, '@', 0, 0, 40)
	p.Update(runes("main"))
	if activate(t, p).CountUsage {
		t.Fatal("anchored finder selection must not set CountUsage")
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
	// The dismissal is recorded for the host to take (#2399): the mode that
	// was listing and how much query was typed, nothing else. It is taken
	// once — a second call has nothing to report.
	d, ok := p.TakeDismissal()
	if !ok {
		t.Fatal("esc should record a dismissal")
	}
	if d.Prefix != ':' || d.QueryLen != 0 {
		t.Fatalf("dismissal = %+v, want the command mode with an empty query", d)
	}
	if _, ok := p.TakeDismissal(); ok {
		t.Fatal("a dismissal must only be reported once")
	}
	// An activation is not a dismissal.
	p.Open(Context{})
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := p.TakeDismissal(); ok {
		t.Fatal("enter must not record a dismissal")
	}
	// The typed query's length travels with it, the query itself never does.
	p.Open(Context{})
	p.Update(runes(":ab"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if d, _ := p.TakeDismissal(); d.QueryLen != 2 {
		t.Fatalf("query length = %d, want 2 (the body without the prefix)", d.QueryLen)
	}
}

// TestDismissalCarriesOpenDuration is the #2408 addition: a dismissal reports
// how long the box stood, so an export can tell a glance-and-esc from a minute
// of fruitless searching. Each open is timed on its own.
func TestDismissalCarriesOpenDuration(t *testing.T) {
	p := New(Config{}, NewCommandMode(fakeSource{}, nil, false), fileMode())
	now := time.Unix(1_700_000_000, 0)
	p.now = func() time.Time { return now }
	p.SetSize(80, 24)

	p.Open(Context{})
	now = now.Add(3 * time.Second)
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	d, ok := p.TakeDismissal()
	if !ok || d.Open != 3*time.Second {
		t.Fatalf("dismissal = (%+v, %v), want a 3s open", d, ok)
	}

	// The second open starts its own clock — durations never accumulate.
	p.Open(Context{})
	now = now.Add(time.Second)
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if d, _ := p.TakeDismissal(); d.Open != time.Second {
		t.Fatalf("second open = %v, want 1s", d.Open)
	}
}

// TestDismissalCarriesResultCount is the #2490 addition: a dismissal reports
// how many rows the list was showing, so an export can tell a query that
// matched nothing ("typed a command name that does not exist") from one that
// matched and was abandoned — the query length alone cannot.
func TestDismissalCarriesResultCount(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		owned("a", "Alpha", plugin.GlobalScope()),
		owned("b", "Bravo", plugin.GlobalScope()),
	}}
	p := New(Config{}, NewCommandMode(src, nil, false), fileMode())
	p.SetSize(80, 24)

	// A query nobody matches: zero rows on a non-empty query.
	p.Open(Context{})
	p.Update(runes(":zzzznope"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	d, ok := p.TakeDismissal()
	if !ok || d.QueryLen == 0 || d.Results != 0 {
		t.Fatalf("dismissal = (%+v, %v), want a typed query with 0 results", d, ok)
	}

	// A matching query reports the match count.
	p.Open(Context{})
	p.Update(runes(":Alpha"))
	want := len(p.items)
	if want == 0 {
		t.Fatal("fixture must match at least one command")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if d, _ := p.TakeDismissal(); d.Results != want {
		t.Fatalf("results = %d, want %d", d.Results, want)
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
	// Wrap-around at the bottom (#1666).
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if p.selected != 0 {
		t.Fatalf("down on the last entry should wrap to 0, got %d", p.selected)
	}
	// …and at the top.
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.selected != 2 {
		t.Fatalf("up on the first entry should wrap to 2, got %d", p.selected)
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

// TestRefreshRowsKeepsSelection guards the in-place variant (#2178): the
// project picker's rows grow their git badge while the user is already
// arrowing through the list, so re-listing must not reset the cursor — and
// must still clamp when the new list is shorter.
func TestRefreshRowsKeepsSelection(t *testing.T) {
	live := &liveStub{rows: []Item{{Title: "a"}, {Title: "b"}, {Title: "c"}}}
	p := New(Config{}, live)
	p.SetSize(80, 24)
	p.OpenLocked(Context{}, '$')
	p.selected, p.top = 2, 1

	// Same rows, enriched: the selection stays where the user put it.
	live.rows = []Item{{Title: "a", Badge: "⎇ main"}, {Title: "b"}, {Title: "c"}}
	p.RefreshRows()
	if p.selected != 2 || p.top != 1 {
		t.Errorf("selection moved to %d/%d, want 2/1", p.selected, p.top)
	}
	if p.items[0].Badge != "⎇ main" {
		t.Errorf("rows were not recomputed: %+v", p.items)
	}
	// A shorter list clamps instead of pointing past the end.
	live.rows = []Item{{Title: "a"}}
	p.RefreshRows()
	if p.selected != 0 || p.top != 0 {
		t.Errorf("selection = %d/%d after shrinking, want 0/0", p.selected, p.top)
	}
	// Closed is a no-op, like Refresh.
	p.Close()
	live.rows = nil
	p.RefreshRows()
	if len(p.items) != 1 {
		t.Errorf("RefreshRows recomputed while closed: %+v", p.items)
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
	if p.query.Text != "#~/Development/" {
		t.Fatalf("tab must extend the query body keeping the prefix, got %q", p.query.Text)
	}
}

func TestTabInertWithoutCompletion(t *testing.T) {
	p := New(Config{}, completerMode{})
	p.SetSize(80, 24)
	p.Open(Context{})
	p.Update(runes("#~/zzz"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.query.Text != "#~/zzz" {
		t.Fatalf("tab with nothing to complete must be inert, got %q", p.query.Text)
	}
}

func TestTabInertOnNonCompleterMode(t *testing.T) {
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(fakeSource{}, nil, false))
	p.SetSize(80, 24)
	p.Open(Context{})
	p.Update(runes(":wri"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.query.Text != ":wri" {
		t.Fatalf("tab on a non-completer mode must be inert, got %q", p.query.Text)
	}
}

// TestTabCompletesFromSelectionInAnchoredFinder guards #1775: in the anchored
// '@' finder tab adopts the selected fuzzy candidate as the query, so typing a
// fragment and pressing tab yields the full path — the shell/JetBrains
// behavior. The completed query still selects the same file.
func TestTabCompletesFromSelectionInAnchoredFinder(t *testing.T) {
	f := fileMode("internal/app/app.go", "internal/palette/palette.go")
	p := New(Config{DefaultPrefix: '@'}, f)
	p.SetSize(80, 24)
	p.OpenAnchored(Context{Root: "/proj"}, '@', 2, 3, 40)
	p.Update(runes("appgo"))
	if len(p.items) == 0 {
		t.Fatal("query must produce a candidate to complete from")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.query.Text != "internal/app/app.go" {
		t.Fatalf("tab must adopt the selected candidate, got %q", p.query.Text)
	}
	if p.query.Cur != len([]rune(p.query.Text)) {
		t.Fatalf("cursor = %d, want end of the completed query", p.query.Cur)
	}
	if len(p.items) == 0 || p.items[0].Title != "internal/app/app.go" {
		t.Fatalf("completed query must still select the file, got %v", p.items)
	}
}

// TestTabCompletesFromSelectionDescendsDirectory guards #1775: adopting a
// directory row keeps its trailing separator, so the completed query is a path
// query and the list already shows the directory's contents.
func TestTabCompletesFromSelectionDescendsDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "outside-dir", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := fileMode()
	p := New(Config{DefaultPrefix: '@'}, f)
	p.SetSize(80, 24)
	p.OpenAnchored(Context{Root: root}, '@', 2, 3, 40)
	p.Update(runes("outside"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	wantQuery := filepath.Join(root, "outside-dir") + string(filepath.Separator)
	if p.query.Text != wantQuery {
		t.Fatalf("tab on a directory row = %q, want %q", p.query.Text, wantQuery)
	}
	if len(p.items) != 1 || !strings.HasSuffix(p.items[0].Title, "inner"+string(filepath.Separator)) {
		t.Fatalf("completed directory query must list its contents, got %v", p.items)
	}
}

// TestSideModeTabStillTogglesColumns guards #1775 against a regression of
// #778: with a left column open tab switches focus, it never completes.
func TestSideModeTabStillTogglesColumns(t *testing.T) {
	p := New(Config{DefaultPrefix: '@'}, fileMode("a.go"))
	p.SetSize(80, 24)
	p.Open(Context{Root: "/proj"})
	p.sideItems = []Item{{Title: "left"}}
	p.Update(runes("a"))
	p.sideItems = []Item{{Title: "left"}}
	before := p.query.Text
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !p.sideFocus || p.query.Text != before {
		t.Fatalf("side-column tab must toggle focus, not complete: focus=%v query=%q", p.sideFocus, p.query.Text)
	}
}

// TestCursorEditing guards #763: arrows move the cursor, typing inserts at
// it, alt+backspace deletes the word before it.
func TestCursorEditing(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		owned("example.hello", "Say Hello", plugin.GlobalScope()),
	}}
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(src, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{ContextID: "editor"})

	p.Update(runes("helo"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	p.Update(runes("l"))
	if p.query.Text != "hello" {
		t.Fatalf("insert at cursor: query = %q, want %q", p.query.Text, "hello")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	p.Update(runes(" world"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if p.query.Text != "hello " {
		t.Fatalf("word delete: query = %q, want %q", p.query.Text, "hello ")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if p.query.Text != "ello " {
		t.Fatalf("forward delete at home: query = %q, want %q", p.query.Text, "ello ")
	}
}

// TestQueryViewCursorPosition: the cursor renders inside the body, offset by
// the mode prefix.
func TestQueryViewCursorPosition(t *testing.T) {
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(fakeSource{}, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{})
	p.Update(runes("@abc"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // cursor between b and c
	v := p.queryView(40)
	if !strings.Contains(v, "abc") && !strings.Contains(v, "c") {
		t.Fatalf("queryView lost text: %q", v)
	}
	if strings.Contains(v, "@") {
		t.Fatalf("queryView must strip the prefix: %q", v)
	}
}

func TestPaletteResizeChordsAdjustWidthAndRows(t *testing.T) {
	var cmds []registry.OwnedCommand
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		cmds = append(cmds, owned("cmd."+id, "Command "+id, plugin.GlobalScope()))
	}
	p := New(Config{DefaultPrefix: ':', MaxResults: 10}, NewCommandMode(fakeSource{cmds}, nil, false), fileMode())
	store := filepath.Join(t.TempDir(), "winsize.json")
	s := ui.LoadWinSizes(store)
	p.SetSizeStore(s)
	p.SetSize(120, 40)
	p.Open(Context{})

	baseW := p.boxWidth()
	baseRows := p.visibleRows()
	p.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl | tea.ModShift})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModCtrl | tea.ModShift})
	if got := p.boxWidth(); got != baseW+4 {
		t.Fatalf("box width = %d, want %d", got, baseW+4)
	}
	if got := p.visibleRows(); got != baseRows+1 {
		t.Fatalf("visible rows = %d, want %d", got, baseRows+1)
	}
	// Shrinking floors at 3 rows and the minimum box width.
	for i := 0; i < 30; i++ {
		p.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl | tea.ModShift})
		p.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl | tea.ModShift})
	}
	if got := p.visibleRows(); got != 3 {
		t.Fatalf("rows floor = %d, want 3", got)
	}
	if got := p.boxWidth(); got != minBoxWidth {
		t.Fatalf("width floor = %d, want %d", got, minBoxWidth)
	}
	// The deltas persist for the next session.
	re := ui.LoadWinSizes(store)
	if dw, dh := re.Get("palette"); dw >= 0 || dh >= 0 {
		t.Fatalf("persisted deltas = (%d,%d), want negative", dw, dh)
	}
}

// auxMode serves rows for the #820 aux-action tests: one plain entry and one
// marked "open in memory" with a close aux action.
type auxMode struct{}

func (auxMode) Prefix() rune        { return '&' }
func (auxMode) Placeholder() string { return "aux" }
func (auxMode) Results(string, Context) []Item {
	return []Item{
		{Title: "plain", Msg: OpenFileMsg{Path: "plain"}},
		{Title: "open-proj", Badge: "●", Msg: OpenFileMsg{Path: "proj"}, Aux: RunCommandMsg{ID: "close-ws"}},
	}
}

// TestAuxActionKeyAndBadge guards #820: shift+delete emits the selected
// row's Aux msg keeping the palette open; rows render badge and ✕.
func TestAuxActionKeyAndBadge(t *testing.T) {
	p := New(Config{}, NewCommandMode(fakeSource{}, nil, false), fileMode(), auxMode{})
	p.SetSize(100, 40)
	p.OpenLocked(Context{}, '&')

	view := p.View()
	if !strings.Contains(view, "●") || !strings.Contains(view, "✕") {
		t.Fatalf("marked row must render badge and aux glyph:\n%s", view)
	}

	// shift+delete on the plain row: inert.
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModShift}); cmd != nil {
		t.Fatal("aux on a plain row must be inert")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("aux key must emit the Aux msg")
	}
	if msg, ok := cmd().(RunCommandMsg); !ok || msg.ID != "close-ws" {
		t.Fatalf("aux msg = %#v, want RunCommandMsg{close-ws}", cmd())
	}
	if !p.IsOpen() {
		t.Fatal("aux action must keep the palette open")
	}
}

// TestAuxActionCmdBackspaceAlias guards #1418: cmd+backspace fires the aux
// action like shift+delete, so the chord works without a forward-delete key.
func TestAuxActionCmdBackspaceAlias(t *testing.T) {
	p := New(Config{}, NewCommandMode(fakeSource{}, nil, false), fileMode(), auxMode{})
	p.SetSize(100, 40)
	p.OpenLocked(Context{}, '&')

	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper})
	if cmd == nil {
		t.Fatal("cmd+backspace must emit the Aux msg")
	}
	if msg, ok := cmd().(RunCommandMsg); !ok || msg.ID != "close-ws" {
		t.Fatalf("aux msg = %#v, want RunCommandMsg{close-ws}", cmd())
	}
	if !p.IsOpen() {
		t.Fatal("aux action must keep the palette open")
	}
	// Plain backspace still edits the query, never fires the aux action.
	p.Update(runes("x"))
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}); cmd != nil {
		if _, ok := cmd().(RunCommandMsg); ok {
			t.Fatal("plain backspace must not fire the aux action")
		}
	}
}

// auxGlyphMode carries a close-style row with its own aux glyph (#1418).
type auxGlyphMode struct{}

func (auxGlyphMode) Prefix() rune        { return '^' }
func (auxGlyphMode) Placeholder() string { return "" }
func (auxGlyphMode) Results(string, Context) []Item {
	return []Item{
		{Title: "open-proj", Badge: "●", Msg: OpenFileMsg{Path: "proj"}, Aux: RunCommandMsg{ID: "close-ws"}, AuxGlyph: "⏏"},
		{Title: "cold-proj", Msg: OpenFileMsg{Path: "cold"}, Aux: RunCommandMsg{ID: "remove"}},
	}
}

// TestAuxGlyphOverride guards #1418: a row's AuxGlyph replaces the default
// "✕" in the aux zone, so close-workspace rows are visually distinct.
func TestAuxGlyphOverride(t *testing.T) {
	p := New(Config{}, NewCommandMode(fakeSource{}, nil, false), fileMode(), auxGlyphMode{})
	p.SetSize(100, 40)
	p.OpenLocked(Context{}, '^')

	view := p.View()
	if !strings.Contains(view, "⏏") {
		t.Fatalf("close row must render its own aux glyph:\n%s", view)
	}
	if !strings.Contains(view, "✕") {
		t.Fatalf("removal row must keep the default aux glyph:\n%s", view)
	}
}

// TestAuxActionClick guards #820's mouse path: a click on a row activates
// it; a click on its ✕ zone runs the aux action and keeps the palette open.
func TestAuxActionClick(t *testing.T) {
	p := New(Config{}, NewCommandMode(fakeSource{}, nil, false), fileMode(), auxMode{})
	p.SetSize(100, 40)
	p.OpenLocked(Context{}, '&')
	inner := p.boxWidth() - 4

	// Click the aux zone of row 1 (rows start at box-relative y=3).
	cmd := p.Click(2+inner-1, 3+1)
	if cmd == nil {
		t.Fatal("aux-zone click must emit a command")
	}
	if msg, ok := cmd().(RunCommandMsg); !ok || msg.ID != "close-ws" {
		t.Fatalf("aux click msg = %#v", cmd())
	}
	if !p.IsOpen() {
		t.Fatal("aux click must keep the palette open")
	}

	// Click row 0 outside the aux zone: activates and closes.
	cmd = p.Click(4, 3)
	if cmd == nil {
		t.Fatal("row click must activate")
	}
	if msg, ok := cmd().(OpenFileMsg); !ok || msg.Path != "plain" {
		t.Fatalf("row click msg = %#v", cmd())
	}
	if p.IsOpen() {
		t.Fatal("row activation must close the palette")
	}
}

// TestRowsNeverWrap guards #971: an overlong item title must render as
// exactly one line, selected (background-padded) or not — the height, not
// the per-line width, is the real assertion since MaxWidth used to wrap.
func TestRowsNeverWrap(t *testing.T) {
	p := New(Config{})
	long := strings.Repeat("wiki/architecture/highlighting-", 8) + ".md"
	for _, selected := range []bool{false, true} {
		row := p.row(Item{Title: long, Detail: "cmd+shift+a"}, selected, true, 60)
		if h := lipgloss.Height(row); h != 1 {
			t.Fatalf("selected=%v: row height = %d, want 1:\n%s", selected, h, row)
		}
		if w := lipgloss.Width(row); w > 60 {
			t.Fatalf("selected=%v: row width = %d, want <= 60", selected, w)
		}
		side := p.sideRow(Item{Title: long}, selected, true, 60)
		if h := lipgloss.Height(side); h != 1 {
			t.Fatalf("selected=%v: sideRow height = %d, want 1", selected, h)
		}
	}
}

// TestRowTimeColumnRightAligned (#1114): the Time column pins to the right
// edge before the "✕" zone with clear separation from the title, and the
// full row spans exactly the given width.
func TestRowTimeColumnRightAligned(t *testing.T) {
	p := New(Config{})
	it := Item{Title: "name", Time: "5m ago", Aux: RunCommandMsg{ID: "x"}}
	line := ansi.Strip(p.row(it, false, true, 40))
	if !strings.HasSuffix(line, "5m ago ✕") {
		t.Fatalf("time must sit right-aligned before the ✕, got %q", line)
	}
	if lipgloss.Width(line) != 40 {
		t.Fatalf("row width = %d, want 40", lipgloss.Width(line))
	}
	if !strings.Contains(line, "name    ") {
		t.Fatalf("title and time must be clearly separated, got %q", line)
	}
}

// TestRowNarrowTruncatesTitleKeepsTime (#1114): at narrow widths the title
// truncates with an ellipsis while the time and ✕ stay intact.
func TestRowNarrowTruncatesTitleKeepsTime(t *testing.T) {
	p := New(Config{})
	it := Item{Title: "averyverylongfilename.go", Time: "5m ago", Aux: RunCommandMsg{ID: "x"}}
	line := ansi.Strip(p.row(it, false, true, 24))
	if !strings.HasSuffix(line, "5m ago ✕") {
		t.Fatalf("time and ✕ must survive narrow widths, got %q", line)
	}
	if !strings.Contains(line, "…") {
		t.Fatalf("the title must truncate with an ellipsis, got %q", line)
	}
}

// TestRowVeryNarrowDropsTime (#1114): below the minimum the time drops so
// the name keeps a readable width; the ✕ stays actionable.
func TestRowVeryNarrowDropsTime(t *testing.T) {
	p := New(Config{})
	it := Item{Title: "somefilename.go", Time: "5m ago", Aux: RunCommandMsg{ID: "x"}}
	line := ansi.Strip(p.row(it, false, true, 18))
	if strings.Contains(line, "ago") {
		t.Fatalf("the time must drop below the minimum width, got %q", line)
	}
	if !strings.Contains(line, "✕") {
		t.Fatalf("the ✕ must stay, got %q", line)
	}
}

// TestSideRowLongTitleKeepsAux guards #1531: an over-wide project name in the
// side column truncates with an ellipsis while the badge, time and aux glyph
// stay intact — the row must span exactly the column width, never beyond.
func TestSideRowLongTitleKeepsAux(t *testing.T) {
	p := New(Config{})
	it := Item{
		Title:    "averyveryverylongprojectname-exceeding-column",
		Time:     "2h ago",
		Badge:    "●",
		Aux:      RunCommandMsg{ID: "close-ws"},
		AuxGlyph: "⏏",
	}
	for _, w := range []int{18, 26, 34} {
		line := ansi.Strip(p.sideRow(it, false, true, w))
		if got := ansi.StringWidth(line); got != w {
			t.Fatalf("width %d: row spans %d cells, got %q", w, got, line)
		}
		if !strings.Contains(line, "…") {
			t.Fatalf("width %d: title must truncate with an ellipsis, got %q", w, line)
		}
		if !strings.HasSuffix(line, "⏏ ") {
			t.Fatalf("width %d: aux glyph must survive truncation, got %q", w, line)
		}
		if !strings.Contains(line, "●") {
			t.Fatalf("width %d: badge must survive truncation, got %q", w, line)
		}
	}
}

// TestHighlightMeasuresDisplayCells guards #1531: highlight truncates and
// measures in display cells, so wide runes (CJK) do not overflow the row and
// push the right column out.
func TestHighlightMeasuresDisplayCells(t *testing.T) {
	p := New(Config{})
	styled, w := highlight("非常に長い日本語のプロジェクト名", nil, p.accentColor(), 10)
	if got := ansi.StringWidth(ansi.Strip(styled)); got != w {
		t.Fatalf("reported width %d != rendered width %d", w, got)
	}
	if w > 10 {
		t.Fatalf("width = %d, want <= 10", w)
	}
	if !strings.HasSuffix(styled, "…") {
		t.Fatalf("wide-rune title must truncate with an ellipsis, got %q", styled)
	}

	it := Item{Title: "非常に長い日本語のプロジェクト名", Time: "2h ago", Badge: "●", Aux: RunCommandMsg{ID: "x"}, AuxGlyph: "⏏"}
	line := ansi.Strip(p.sideRow(it, false, true, 26))
	if got := ansi.StringWidth(line); got != 26 {
		t.Fatalf("wide-rune row spans %d cells, want 26: %q", got, line)
	}
	if !strings.HasSuffix(line, "⏏ ") {
		t.Fatalf("aux glyph must survive a wide-rune title, got %q", line)
	}
	if !strings.Contains(line, "2h ago") {
		t.Fatalf("time must survive a wide-rune title, got %q", line)
	}
}

// TestRowWideRunesKeepRightColumn guards #1531 for the main list: row() uses
// the same highlight, so a wide-rune title must not push the detail chip,
// time or aux zone past the row width.
func TestRowWideRunesKeepRightColumn(t *testing.T) {
	p := New(Config{})
	it := Item{Title: "非常に長い日本語のファイル名です.go", Time: "5m ago", Aux: RunCommandMsg{ID: "x"}}
	line := ansi.Strip(p.row(it, false, true, 30))
	if got := ansi.StringWidth(line); got > 30 {
		t.Fatalf("row spans %d cells, want <= 30: %q", got, line)
	}
	if !strings.HasSuffix(line, "5m ago ✕") {
		t.Fatalf("time and ✕ must survive a wide-rune title, got %q", line)
	}
}

// TestRowMarkerFollowsFocus guards #1532: the accent "❯" belongs to the
// focused column only — the unfocused column's selected row keeps a marker,
// but dimmed (border color), so the two are never identical.
func TestRowMarkerFollowsFocus(t *testing.T) {
	p := New(Config{})
	focused := p.rowMarker(true, true)
	unfocused := p.rowMarker(true, false)
	if ansi.Strip(focused) != "❯ " || ansi.Strip(unfocused) != "❯ " {
		t.Fatalf("selected rows must keep the marker glyph, got %q / %q",
			ansi.Strip(focused), ansi.Strip(unfocused))
	}
	if focused == unfocused {
		t.Fatal("focused and unfocused markers must differ in styling")
	}
	if p.rowMarker(false, true) != "  " || p.rowMarker(false, false) != "  " {
		t.Fatal("unselected rows must render no marker")
	}
}

// TestRowRendersDimMarkerWhenUnfocused (#1532): row and sideRow thread the
// focus through to the marker; the rest of the line is unaffected.
func TestRowRendersDimMarkerWhenUnfocused(t *testing.T) {
	p := New(Config{})
	it := Item{Title: "name", Time: "5m ago", Aux: RunCommandMsg{ID: "x"}}
	for name, render := range map[string]func(bool) string{
		"row":     func(f bool) string { return p.row(it, true, f, 40) },
		"sideRow": func(f bool) string { return p.sideRow(it, true, f, 30) },
	} {
		focused, unfocused := render(true), render(false)
		if focused == unfocused {
			t.Fatalf("%s: focused and unfocused selected rows must differ", name)
		}
		if ansi.Strip(focused) != ansi.Strip(unfocused) {
			t.Fatalf("%s: focus must only restyle the marker, got %q vs %q",
				name, ansi.Strip(focused), ansi.Strip(unfocused))
		}
	}
}

// TestSideColumnKeepsSelectionWhenUnfocused (#1532): the side column's
// selected row stays visible (dim marker + background band) while the main
// list holds the focus — previously it showed no selection at all.
func TestSideColumnKeepsSelectionWhenUnfocused(t *testing.T) {
	m := sideRecentMode()
	p := New(Config{}, m, fileMode("a.go", "b.go"))
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	if p.sideFocus {
		t.Fatal("precondition: focus starts on the files column")
	}
	side := p.sideView(24)
	if !strings.Contains(ansi.Strip(side), "❯") {
		t.Fatalf("unfocused side column must keep a (dim) selection marker:\n%s", side)
	}
	dim := p.rowMarker(true, false)
	if !strings.Contains(side, dim) {
		t.Fatal("unfocused side column's marker must use the dim style")
	}
	accent := p.rowMarker(true, true)
	if strings.Contains(side, accent) {
		t.Fatal("unfocused side column must not show the accent marker")
	}

	// Focus flip: the accent marker moves into the side column, the main
	// list's selected row falls back to the dim marker.
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !p.sideFocus {
		t.Fatal("tab must focus the side column")
	}
	if side = p.sideView(24); !strings.Contains(side, accent) {
		t.Fatal("focused side column must show the accent marker")
	}
	if main := p.list(40, !p.sideFocus); !strings.Contains(main, dim) || strings.Contains(main, accent) {
		t.Fatalf("main list must dim its marker while the side column holds the focus:\n%s", main)
	}
}

// TestSideRowTimeColumn (#1114): the side column applies the same layout —
// right-aligned time before the "✕", dropped when the column is too narrow.
func TestSideRowTimeColumn(t *testing.T) {
	p := New(Config{})
	it := Item{Title: "proj", Time: "2h ago", Aux: RunCommandMsg{ID: "x"}}
	line := ansi.Strip(p.sideRow(it, false, true, 30))
	if !strings.HasSuffix(line, "2h ago ✕ ") {
		t.Fatalf("side time must sit right-aligned before the ✕, got %q", line)
	}
	narrow := ansi.Strip(p.sideRow(Item{Title: "longprojectname", Time: "2h ago", Aux: RunCommandMsg{ID: "x"}}, false, true, 16))
	if strings.Contains(narrow, "ago") {
		t.Fatalf("side time must drop in a too-narrow column, got %q", narrow)
	}
}

// TestPageNavigationClamps guards #1666: pgup/pgdn move the selection by one
// visible result window and clamp at the ends instead of wrapping.
func TestPageNavigationClamps(t *testing.T) {
	var cmds []registry.OwnedCommand
	for i := 0; i < 30; i++ {
		cmds = append(cmds, owned("c"+strconv.Itoa(i), "Cmd "+strconv.Itoa(i), plugin.GlobalScope()))
	}
	p := New(Config{DefaultPrefix: ':', MaxResults: 5}, NewCommandMode(fakeSource{cmds: cmds}, nil, false), fileMode())
	p.SetSize(80, 40)
	p.Open(Context{ContextID: "editor"})
	if len(p.items) != 30 {
		t.Fatalf("items = %d, want 30", len(p.items))
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if p.selected != 5 {
		t.Fatalf("pgdn selection = %d, want 5 (one window of 5)", p.selected)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if p.selected != 0 {
		t.Fatalf("pgup selection = %d, want 0", p.selected)
	}
	// Clamping, not wrapping — the distinguishing property against the
	// single-step keys.
	p.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if p.selected != 0 {
		t.Fatalf("pgup on the first row must clamp, got %d", p.selected)
	}
	for i := 0; i < 10; i++ {
		p.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if p.selected != 29 {
		t.Fatalf("pgdn past the end must clamp at 29, got %d", p.selected)
	}
}

// TestSingleResultNavigationStaysPut guards #1666: wrap-around on a one-entry
// list is a no-op, not an index error.
func TestSingleResultNavigationStaysPut(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{owned("a", "Alpha", plugin.GlobalScope())}}
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(src, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{ContextID: "editor"})
	for _, code := range []rune{tea.KeyDown, tea.KeyUp, tea.KeyPgDown, tea.KeyPgUp} {
		p.Update(tea.KeyPressMsg{Code: code})
		if p.selected != 0 {
			t.Fatalf("selection = %d, want 0", p.selected)
		}
	}
}

// panelStub is a stubMode supporting the Find-panel hand-off (#2055).
type panelStub struct{ stubMode }

func (panelStub) PanelTitle(query string) string { return "Find: " + query }

// TestPanelResultsReportsListedRows covers the hand-off seam: a PanelMode
// reports its heading plus the rows currently listed, and reading them leaves
// the overlay open — the caller closes it once it has taken them over.
func TestPanelResultsReportsListedRows(t *testing.T) {
	mode := panelStub{stubMode{prefix: '&', items: []Item{
		{Title: "a.go:3"}, {Title: "b.go:9"},
	}}}
	p := New(Config{}, mode)
	p.SetSize(100, 40)
	p.OpenLocked(Context{Root: "."}, '&')
	p.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	title, items, ok := p.PanelResults()
	if !ok {
		t.Fatal("a PanelMode with rows must offer the hand-off")
	}
	if title != "Find: x" {
		t.Fatalf("title = %q, want the mode's heading for the query body", title)
	}
	if len(items) != 2 || items[0].Title != "a.go:3" {
		t.Fatalf("items = %+v, want the listed rows in order", items)
	}
	if !p.IsOpen() {
		t.Fatal("reading the rows must not close the overlay")
	}
}

// TestPanelResultsDeclinesPlainModes: a mode without PanelMode, and an empty
// list, both decline — the binding stays inert there.
func TestPanelResultsDeclinesPlainModes(t *testing.T) {
	plain := stubMode{prefix: ':', items: []Item{{Title: "Save All"}}}
	p := New(Config{}, plain)
	p.SetSize(100, 40)
	p.OpenLocked(Context{Root: "."}, ':')
	if _, _, ok := p.PanelResults(); ok {
		t.Fatal("a mode without PanelMode must decline the hand-off")
	}

	empty := panelStub{stubMode{prefix: '&'}}
	q := New(Config{}, empty)
	q.SetSize(100, 40)
	q.OpenLocked(Context{Root: "."}, '&')
	if _, _, ok := q.PanelResults(); ok {
		t.Fatal("an empty list must decline the hand-off")
	}
}
