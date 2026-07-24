package palette

import (
	"charm.land/lipgloss/v2"
	"path/filepath"
	"strings"
	"testing"

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
	if p.query != "hello" {
		t.Fatalf("insert at cursor: query = %q, want %q", p.query, "hello")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	p.Update(runes(" world"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if p.query != "hello " {
		t.Fatalf("word delete: query = %q, want %q", p.query, "hello ")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if p.query != "ello " {
		t.Fatalf("forward delete at home: query = %q, want %q", p.query, "ello ")
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
		row := p.row(Item{Title: long, Detail: "cmd+shift+a"}, selected, 60)
		if h := lipgloss.Height(row); h != 1 {
			t.Fatalf("selected=%v: row height = %d, want 1:\n%s", selected, h, row)
		}
		if w := lipgloss.Width(row); w > 60 {
			t.Fatalf("selected=%v: row width = %d, want <= 60", selected, w)
		}
		side := p.sideRow(Item{Title: long}, selected, 60)
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
	line := ansi.Strip(p.row(it, false, 40))
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
	line := ansi.Strip(p.row(it, false, 24))
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
	line := ansi.Strip(p.row(it, false, 18))
	if strings.Contains(line, "ago") {
		t.Fatalf("the time must drop below the minimum width, got %q", line)
	}
	if !strings.Contains(line, "✕") {
		t.Fatalf("the ✕ must stay, got %q", line)
	}
}

// TestSideRowTimeColumn (#1114): the side column applies the same layout —
// right-aligned time before the "✕", dropped when the column is too narrow.
func TestSideRowTimeColumn(t *testing.T) {
	p := New(Config{})
	it := Item{Title: "proj", Time: "2h ago", Aux: RunCommandMsg{ID: "x"}}
	line := ansi.Strip(p.sideRow(it, false, 30))
	if !strings.HasSuffix(line, "2h ago ✕ ") {
		t.Fatalf("side time must sit right-aligned before the ✕, got %q", line)
	}
	narrow := ansi.Strip(p.sideRow(Item{Title: "longprojectname", Time: "2h ago", Aux: RunCommandMsg{ID: "x"}}, false, 16))
	if strings.Contains(narrow, "ago") {
		t.Fatalf("side time must drop in a too-narrow column, got %q", narrow)
	}
}
