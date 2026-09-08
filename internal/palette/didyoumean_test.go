package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/plugin"
	"ike/internal/registry"
)

// didyoumean_test.go covers the ":" mode's second tier and the inert rows it
// lists (#2548): a query the titles cannot answer falls back to aliases,
// binding labels, chord text and menu paths under a "did you mean" separator;
// nothing at all yields a hint row; and the palette core never selects,
// activates or clicks the chrome rows.

// titledResolver is a BindingResolver that also implements BindingTitler.
type titledResolver struct {
	keys   map[string]string
	titles map[string]string
}

func (r titledResolver) Binding(id string) (string, bool) {
	k, ok := r.keys[id]
	return k, ok
}

func (r titledResolver) BindingTitle(id string) (string, bool) {
	t, ok := r.titles[id]
	return t, ok
}

func aliased(id, title string, aliases ...string) registry.OwnedCommand {
	c := owned(id, title, plugin.GlobalScope())
	c.Aliases = aliases
	return c
}

// rowTitles flattens the titles of a result list for failure messages.
func rowTitles(items []Item) string {
	var out []string
	for _, it := range items {
		s := it.Title
		if it.Inert {
			s = "[" + s + "]"
		}
		out = append(out, s)
	}
	return strings.Join(out, " | ")
}

// splitTiers returns the rows before and after the "did you mean" separator;
// ok is false when no separator is listed.
func splitTiers(items []Item) (primary, alt []Item, ok bool) {
	for i, it := range items {
		if it.Inert && it.Title == DidYouMeanSeparator {
			return items[:i], items[i+1:], true
		}
	}
	return items, nil, false
}

func TestDidYouMeanAliasSurfaces(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		aliased("project.close", "Close Project", "quit", "exit"),
		aliased("terminal.popup", "Popup Terminal", "terminal", "shell"),
		owned("editor.write", "Save File", plugin.GlobalScope()),
	}}
	cmd := NewCommandMode(src, nil, false)

	items := cmd.Results("quit", Context{ContextID: "editor"})
	primary, alt, ok := splitTiers(items)
	if !ok {
		t.Fatalf("want a did-you-mean separator, got %s", rowTitles(items))
	}
	if len(primary) != 0 {
		t.Fatalf("no title matches 'quit', primary tier must be empty: %s", rowTitles(items))
	}
	if len(alt) != 1 || alt[0].Title != "Close Project" {
		t.Fatalf("second tier = %s, want Close Project alone", rowTitles(alt))
	}
	if alt[0].Badge != "quit" {
		t.Fatalf("badge = %q, want the matched alias", alt[0].Badge)
	}
	if run, isRun := alt[0].Msg.(RunCommandMsg); !isRun || run.ID != "project.close" {
		t.Fatalf("second-tier row must dispatch its command: %+v", alt[0].Msg)
	}
	if alt[0].Inert {
		t.Fatal("a suggestion row must be activatable, not inert")
	}
}

func TestDidYouMeanBindingLabelChordAndMenuPath(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		owned("nav.lastEdit", "Jump Back", plugin.GlobalScope()),
		owned("project.switch", "Open Picker", plugin.GlobalScope()),
		owned("lsp.hover", "Quick Doc", plugin.GlobalScope()),
	}}
	res := titledResolver{
		keys:   map[string]string{"nav.lastEdit": "cmd+shift+backspace", "lsp.hover": "ctrl+q"},
		titles: map[string]string{"nav.lastEdit": "Last edit location"},
	}
	cmd := NewCommandMode(src, res, false)
	cmd.SetMenuPaths(map[string]string{"project.switch": "File › Switch Project"})
	cx := Context{ContextID: "editor"}

	cases := []struct{ query, want, badge string }{
		{"last edit", "Jump Back", "Last edit location"},           // keymap binding label
		{"switch project", "Open Picker", "File › Switch Project"}, // menu path
		{"ctrl+q", "Quick Doc", "ctrl+q"},                          // chord text
	}
	for _, tc := range cases {
		items := cmd.Results(tc.query, cx)
		_, alt, ok := splitTiers(items)
		if !ok || len(alt) == 0 {
			t.Fatalf("query %q: want a did-you-mean tier, got %s", tc.query, rowTitles(items))
		}
		if alt[0].Title != tc.want || alt[0].Badge != tc.badge {
			t.Fatalf("query %q: top suggestion = %q/%q, want %q/%q", tc.query, alt[0].Title, alt[0].Badge, tc.want, tc.badge)
		}
	}
}

// TestDidYouMeanKeepsPrimaryRankingAndExcludesListed: the primary tier's
// order is untouched by the fallback, rows already listed never repeat below
// the separator, and a full primary tier suppresses the fallback entirely.
func TestDidYouMeanKeepsPrimaryRankingAndExcludesListed(t *testing.T) {
	cmds := []registry.OwnedCommand{
		aliased("a.saveAll", "Save All", "save"),
		aliased("b.saveAs", "Save As", "save"),
		aliased("c.export", "Export Document", "save"),
	}
	cmd := NewCommandMode(fakeSource{cmds: cmds}, nil, false)
	cx := Context{ContextID: "editor"}

	items := cmd.Results("save", cx)
	primary, alt, ok := splitTiers(items)
	if !ok {
		t.Fatalf("want a separator: %s", rowTitles(items))
	}
	if got := rowTitles(primary); got != rowTitles(cmd.PrimaryResults("save", cx)) {
		t.Fatalf("primary tier changed by the fallback: %s", got)
	}
	if len(primary) != 2 {
		t.Fatalf("two titles match 'save': %s", rowTitles(items))
	}
	if len(alt) != 1 || alt[0].Title != "Export Document" {
		t.Fatalf("second tier must hold only the unlisted alias match: %s", rowTitles(alt))
	}

	// Enough title matches: no fallback tier at all, even though every
	// command carries a matching alias.
	var many []registry.OwnedCommand
	for _, name := range []string{"Save All", "Save As", "Save Copy", "Save Session", "Save Layout"} {
		many = append(many, aliased("x."+name, name, "save"))
	}
	many = append(many, aliased("y.export", "Export", "save"))
	full := NewCommandMode(fakeSource{cmds: many}, nil, false).Results("save", cx)
	if _, _, has := splitTiers(full); has || len(full) != DidYouMeanBelow {
		t.Fatalf("a primary tier of %d rows must not grow a fallback: %s", DidYouMeanBelow, rowTitles(full))
	}
}

func TestDidYouMeanHintRowWhenNothingMatches(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		aliased("project.close", "Close Project", "quit"),
	}}
	cmd := NewCommandMode(src, nil, false)
	items := cmd.Results("zzzzqx", Context{ContextID: "editor"})
	if len(items) != 1 || !items[0].Inert || items[0].Title != NoMatchHint {
		t.Fatalf("want the single no-match hint row, got %s", rowTitles(items))
	}
	if items[0].Msg != nil {
		t.Fatal("the hint row must carry nothing to dispatch")
	}
	// An empty query lists everything and never grows chrome rows.
	if all := cmd.Results("", Context{ContextID: "editor"}); len(all) != 1 || all[0].Inert {
		t.Fatalf("empty query must list the plain commands: %s", rowTitles(all))
	}
}

func TestDidYouMeanRespectsHideOff(t *testing.T) {
	c := owned("m.explorer", "Explorer Thing", plugin.PaneScope("explorer"))
	c.Aliases = []string{"tree"}
	src := fakeSource{cmds: []registry.OwnedCommand{c}}
	cx := Context{ContextID: "editor"}

	if items := NewCommandMode(src, nil, true).Results("tree", cx); len(items) != 1 || items[0].Title != NoMatchHint {
		t.Fatalf("hideOff must drop the off-context suggestion: %s", rowTitles(items))
	}
	if items := NewCommandMode(src, nil, false).Results("tree", cx); len(items) != 2 || items[1].Title != "Explorer Thing" {
		t.Fatalf("ranked mode keeps the off-context suggestion: %s", rowTitles(items))
	}
}

// TestSearchEverywhereSkipsDidYouMean: the composed mode reads the primary
// tier only — a ": did you mean" separator has no place among file rows.
func TestSearchEverywhereSkipsDidYouMean(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		aliased("project.close", "Close Project", "quit"),
	}}
	cmdMode := NewCommandMode(src, nil, false)
	all := NewSearchAllMode(cmdMode, fileMode("quitter.go"))
	cx := Context{ContextID: "editor", Root: "."}
	for _, q := range []string{"quit", ":quit"} {
		for _, it := range all.Results(q, cx) {
			if it.Inert {
				t.Fatalf("query %q: search everywhere listed a chrome row: %q", q, it.Title)
			}
		}
	}
}

// TestPaletteSkipsInertRows: the selection never rests on the separator —
// recompute lands past it when nothing precedes it, arrow keys step over it in
// both directions, enter on a hint-only list keeps the palette open, and a
// click on the separator does nothing.
func TestPaletteSkipsInertRows(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		owned("editor.write", "Save File", plugin.GlobalScope()),
		aliased("doc.export", "Export", "save"),
		aliased("doc.publish", "Publish", "save"),
	}}
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(src, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{ContextID: "editor"})
	p.Update(runes(":save"))

	// Save File | [did you mean] | Export | Publish
	if got := rowTitles(p.items); got != "Save File | [did you mean] | Export | Publish" {
		t.Fatalf("list = %s", got)
	}
	if p.selected != 0 {
		t.Fatalf("selection starts on the first real row, got %d", p.selected)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if p.selected != 2 {
		t.Fatalf("down must step over the separator to Export, got %d", p.selected)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.selected != 0 {
		t.Fatalf("up must step back over the separator to Save File, got %d", p.selected)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.selected != 3 {
		t.Fatalf("up from the top wraps to Publish, got %d", p.selected)
	}
	// A click on the separator row (list row index 1 → y = 1 border + 2 chrome + 1).
	if cmd := p.Click(4, 4); cmd != nil || !p.IsOpen() {
		t.Fatal("clicking the separator must neither activate nor close")
	}
	// Enter on a suggestion dispatches it.
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // wraps to Save File
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // Export
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a suggestion should emit")
	}
	if run, ok := cmd().(RunCommandMsg); !ok || run.ID != "doc.export" {
		t.Fatalf("activated %+v, want save.export", cmd())
	}

	// Hint-only list: enter is inert and the palette stays open.
	p.Open(Context{ContextID: "editor"})
	p.Update(runes(":zzqx"))
	if len(p.items) != 1 || !p.items[0].Inert {
		t.Fatalf("want the hint row alone: %s", rowTitles(p.items))
	}
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || !p.IsOpen() {
		t.Fatal("enter on the hint row must keep the palette open")
	}
	if view := p.View(); !strings.Contains(view, "no command matches") {
		t.Fatalf("view must render the hint row:\n%s", view)
	}
}

// TestDidYouMeanSeparatorRenders: the separator is drawn as a dim rule row
// without a selection marker, and the suggestion beneath shows its badge.
func TestDidYouMeanSeparatorRenders(t *testing.T) {
	src := fakeSource{cmds: []registry.OwnedCommand{
		aliased("project.close", "Close Project", "quit"),
	}}
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(src, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{ContextID: "editor"})
	p.Update(runes(":quit"))
	view := p.View()
	for _, want := range []string{DidYouMeanSeparator, "Close Project", "quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "❯ "+DidYouMeanSeparator) || strings.Contains(view, "❯   "+DidYouMeanSeparator) {
		t.Fatalf("separator must not carry the selection marker:\n%s", view)
	}
}
