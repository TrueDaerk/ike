package help

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/version"
)

// labels lists the group labels of a snapshot in order.
func labels(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Label)
	}
	return out
}

// contextRegistry is the stub for the reduced context view (#2483): two
// curated global commands, one non-curated global, two pane scopes, and two
// file-type-gated commands (one global, one editor-scoped).
func contextRegistry() *registry.Registry {
	r := registry.New()
	r.Add(stubPlugin{id: "core", cmd: []plugin.Command{
		{ID: "core.quit", Title: "Quit", Scope: plugin.GlobalScope()},
		{ID: "settings.open", Title: "Settings", Scope: plugin.GlobalScope()},
		{ID: "palette.searchEverywhere", Title: "Search Everywhere", Scope: plugin.GlobalScope()},
		{ID: "editor.save", Title: "Save", Scope: plugin.PaneScope("editor")},
		{ID: "explorer.new", Title: "New File", Scope: plugin.PaneScope("explorer")},
		{ID: "json.jqPlayground", Title: "jq Playground…", Scope: plugin.GlobalScope(), Languages: []string{"json", "jsonc", "ndjson"}},
		{ID: "csv.columnProfile", Title: "CSV: Column Profile", Scope: plugin.PaneScope("editor"), Languages: []string{"csv", "tsv", "psv"}},
	}})
	return r
}

// TestContextSnapshotShowsFocusedContextAndCuratedGlobalOnly (#2483): the
// context view is the focused context's own group — flagged as focused — plus
// the hand-curated Global essentials. No other context is listed, and the
// global section carries only the curated commands, not the full global dump.
func TestContextSnapshotShowsFocusedContextAndCuratedGlobalOnly(t *testing.T) {
	groups := ContextSnapshot(contextRegistry(), nil, "explorer", "")
	if got, want := strings.Join(labels(groups), ","), "explorer,"+GlobalEssentialsLabel; got != want {
		t.Fatalf("context groups = %q, want %q", got, want)
	}
	if !groups[0].Focused {
		t.Fatal("the leading group must be flagged as the focused context")
	}
	if groups[1].Focused {
		t.Fatal("the global section must not carry the focused flag")
	}
	var ids []string
	for _, e := range groups[1].Entries {
		ids = append(ids, e.ID)
	}
	if got, want := strings.Join(ids, ","), "palette.searchEverywhere,settings.open"; got != want {
		t.Fatalf("curated global = %q, want %q (curated order, no core.quit, no gated commands)", got, want)
	}

	// Focus the editor instead and its own group leads; the language-gated
	// csv command is dropped over an unclassified buffer.
	groups = ContextSnapshot(contextRegistry(), nil, "editor", "")
	if got, want := strings.Join(labels(groups), ","), "editor,"+GlobalEssentialsLabel; got != want {
		t.Fatalf("editor-focused groups = %q, want %q", got, want)
	}
	for _, e := range groups[0].Entries {
		if e.ID == "csv.columnProfile" {
			t.Fatal("a file-type-gated command must not show over an unclassified buffer")
		}
	}
}

// TestContextSnapshotDegradesWithoutFocus: an empty or "global" context id
// yields the plain full Snapshot ordering; a focused context owning no
// commands yields just the curated global section, over which a
// keyboard-owning mode's extra groups (#2237) can still lead.
func TestContextSnapshotDegradesWithoutFocus(t *testing.T) {
	for _, ctx := range []string{"", "global"} {
		groups := ContextSnapshot(contextRegistry(), nil, ctx, "")
		if got, want := strings.Join(labels(groups), ","), "global,editor,explorer"; got != want {
			t.Fatalf("context %q groups = %q, want %q", ctx, got, want)
		}
	}
	groups := ContextSnapshot(contextRegistry(), nil, "terminal", "")
	if got, want := strings.Join(labels(groups), ","), GlobalEssentialsLabel; got != want {
		t.Fatalf("commandless context groups = %q, want just %q", got, want)
	}
	for _, g := range groups {
		if g.Focused {
			t.Fatalf("a commandless context must not flag a focused group, %q does", g.Label)
		}
	}
}

// TestContextSnapshotGatesFileTypeCommands (#2483): commands gated on a
// language show only while the buffer's language matches — the global ones in
// their own "This file" section, the context-scoped ones inside their context
// group — and every gated row carries the family badge.
func TestContextSnapshotGatesFileTypeCommands(t *testing.T) {
	// A JSON buffer surfaces the jq playground, badged with its family name.
	groups := ContextSnapshot(contextRegistry(), nil, "editor", "json")
	if got, want := strings.Join(labels(groups), ","), "editor,This file (json),"+GlobalEssentialsLabel; got != want {
		t.Fatalf("json groups = %q, want %q", got, want)
	}
	ft := groups[1]
	if len(ft.Entries) != 1 || ft.Entries[0].ID != "json.jqPlayground" {
		t.Fatalf("file-type section = %+v, want exactly json.jqPlayground", ft.Entries)
	}
	if ft.Entries[0].Lang != "json" {
		t.Fatalf("gated entry badge = %q, want the family name json", ft.Entries[0].Lang)
	}

	// A Go buffer matches no gate: no file-type section, no gated command
	// anywhere.
	for _, g := range ContextSnapshot(contextRegistry(), nil, "editor", "go") {
		for _, e := range g.Entries {
			if e.ID == "json.jqPlayground" || e.ID == "csv.columnProfile" {
				t.Fatalf("gated command %s must not show over a go buffer", e.ID)
			}
		}
	}

	// A CSV buffer keeps the editor-scoped gated command inside the editor
	// group — badged — and repeats nothing in a file-type section.
	groups = ContextSnapshot(contextRegistry(), nil, "editor", "csv")
	if got, want := strings.Join(labels(groups), ","), "editor,"+GlobalEssentialsLabel; got != want {
		t.Fatalf("csv groups = %q, want %q", got, want)
	}
	var csv *Entry
	for i, e := range groups[0].Entries {
		if e.ID == "csv.columnProfile" {
			csv = &groups[0].Entries[i]
		}
	}
	if csv == nil || csv.Lang != "csv" {
		t.Fatalf("csv.columnProfile must sit badged in the editor group, got %+v", csv)
	}
}

// TestHelpOpensOnContextView (#2182, #2483): opening help with a pane focused
// shows that pane's bindings plus the curated global section — and nothing
// else; the other contexts are gone, not merely reordered.
func TestHelpOpensOnContextView(t *testing.T) {
	h := New(contextRegistry(), nil, 0)
	h.Snapshot("explorer", "")

	if h.view != viewContext {
		t.Fatalf("view = %v, want the context view", h.view)
	}
	if title := h.Title(); !strings.HasPrefix(title, "HELP "+version.Short()) || !strings.HasSuffix(title, "— Explorer context") {
		t.Fatalf("context view title = %q", title)
	}
	body := ansi.Strip(h.Render(80))
	if !strings.Contains(body, "Explorer — focused pane") {
		t.Fatalf("focused section heading missing:\n%s", body)
	}
	if !strings.Contains(body, "Global (essentials)") {
		t.Fatalf("curated global heading missing:\n%s", body)
	}
	if strings.Index(body, "Explorer — focused pane") > strings.Index(body, "Global (essentials)") {
		t.Fatalf("focused section must precede the global one:\n%s", body)
	}
	if strings.Contains(body, "Save") || strings.Contains(body, "Quit") {
		t.Fatalf("other contexts and non-curated globals must be gone:\n%s", body)
	}
	if !strings.Contains(body, "press tab for the full list") {
		t.Fatalf("context footer must advertise the toggle:\n%s", body)
	}

	// The same overlay focused on the editor leads with the editor instead.
	h.Snapshot("editor", "")
	body = ansi.Strip(h.Render(80))
	if !strings.Contains(body, "Editor — focused pane") {
		t.Fatalf("editor focus should lead with the editor section:\n%s", body)
	}
	if strings.Contains(body, "New File") {
		t.Fatalf("the explorer's commands must not show while the editor is focused:\n%s", body)
	}
}

// TestHelpContextViewRendersLanguageBadge (#2483): a gated command matching
// the focused buffer renders with its bracketed family badge, so the row reads
// as conditional on the file type.
func TestHelpContextViewRendersLanguageBadge(t *testing.T) {
	h := New(contextRegistry(), nil, 0)
	h.Snapshot("editor", "json")
	body := ansi.Strip(h.Render(100))
	if !strings.Contains(body, "jq Playground… [json]") {
		t.Fatalf("gated row must carry the [json] badge:\n%s", body)
	}
	h.Snapshot("editor", "go")
	if body := ansi.Strip(h.Render(100)); strings.Contains(body, "jq Playground") {
		t.Fatalf("gated row must vanish over a go buffer:\n%s", body)
	}
}

// TestContextViewTogglesToFullList (#2182, #2483): tab cycles context -> flat
// -> essentials -> context, and the flat list is the complete reference —
// every scope, global first, including the commands the context view hides.
func TestContextViewTogglesToFullList(t *testing.T) {
	h := New(contextRegistry(), nil, 0)
	h.Snapshot("editor", "")
	if h.view != viewContext {
		t.Fatalf("expected the context view on open, got %v", h.view)
	}

	if !h.HandleKey("tab") {
		t.Fatal("tab must be consumed")
	}
	if h.view != viewFlat {
		t.Fatalf("first tab = %v, want the flat list", h.view)
	}
	body := ansi.Strip(h.Render(80))
	if !strings.HasSuffix(h.Title(), "— commands & shortcuts") {
		t.Fatalf("flat view title = %q", h.Title())
	}
	if strings.Contains(body, "— focused pane") {
		t.Fatalf("the flat list keeps the classic headings:\n%s", body)
	}
	for _, want := range []string{"Quit", "New File", "jq Playground"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the flat list is the complete reference, missing %q:\n%s", want, body)
		}
	}
	if strings.Index(body, "Global") > strings.Index(body, "Editor") {
		t.Fatalf("flat list is global-first:\n%s", body)
	}

	h.HandleKey("tab")
	if h.view != viewEssentials {
		t.Fatalf("second tab = %v, want essentials", h.view)
	}
	h.HandleKey("tab")
	if h.view != viewContext {
		t.Fatalf("third tab = %v, want back to the context view", h.view)
	}
}

// TestContextViewSkippedWithoutFocus verifies the cycle degrades to the
// pre-#2182 two-view toggle when no context is focused.
func TestContextViewSkippedWithoutFocus(t *testing.T) {
	h := New(essentialsRegistry(), nil, 0)
	h.Snapshot("", "")
	if h.view != viewEssentials {
		t.Fatalf("no focus should open on essentials, got %v", h.view)
	}
	h.HandleKey("tab")
	if h.view != viewFlat {
		t.Fatalf("tab without a focused context = %v, want the flat list", h.view)
	}
	h.HandleKey("tab")
	if h.view != viewEssentials {
		t.Fatalf("tab should return to essentials, got %v", h.view)
	}
}

// TestContextViewLayoutStaysResponsive (#2182): the two-column cap and the
// column packing apply to the context view exactly as to the flat one.
func TestContextViewLayoutStaysResponsive(t *testing.T) {
	h := New(testRegistry(), nil, 0)
	h.Snapshot("explorer", "")
	colW := MinColumnWidth(h.allCells(h.ctxGroups), 0) + colSlack
	wide := ansi.Strip(h.Render(400))
	// The footer legend is a free-running line; the packed sections are what
	// the two-column cap governs.
	for _, line := range strings.Split(wide, "\n") {
		if strings.Contains(line, "press tab") {
			continue
		}
		if w := lipgloss.Width(strings.TrimRight(line, " ")); w > 2*colW+gutter {
			t.Fatalf("context view line width %d exceeds the two-column bound %d: %q", w, 2*colW+gutter, line)
		}
	}
	narrow := ansi.Strip(h.Render(20))
	if !strings.Contains(narrow, "New File") {
		t.Fatalf("narrow render should still list the focused context's commands:\n%s", narrow)
	}
}

// TestFilterSearchesEveryContextFromContextView (#2182): a filter typed in the
// context view searches all scopes, including contexts other than the focused
// one and the commands the reduced view hides.
func TestFilterSearchesEveryContextFromContextView(t *testing.T) {
	h := New(testRegistry(), nil, 0)
	h.Snapshot("explorer", "")
	h.SetFilter("save") // an editor command while the explorer is focused
	if body := ansi.Strip(h.Render(80)); !strings.Contains(body, "Save") {
		t.Fatalf("filter must reach other contexts:\n%s", body)
	}
}

// TestFocusedExtraGroupLeadsContextView (#2237): a mode that owns the keyboard
// without owning a registry scope — the jq/yq playground — contributes its keys
// as an extra group flagged Focused. In the context view it must *lead*, ahead
// of the global bindings, the way a focused pane's registered scope does; in
// the flat view the extras keep trailing, and unflagged extras trail in both.
func TestFocusedExtraGroupLeadsContextView(t *testing.T) {
	h := New(testRegistry(), nil, 0)
	h.SetExtra(
		Group{Label: "blocked (dependency not landed)", Entries: []Entry{{ID: "x", Title: "Blocked Thing"}}},
		Group{Label: "jq playground — query line", Focused: true, Entries: []Entry{{ID: "p", Title: "Close and open the palette", Shortcut: "esc esc"}}},
	)
	h.Snapshot("playground", "")

	if h.view != viewContext {
		t.Fatalf("a Focused extra group makes a context view, got %v", h.view)
	}
	if h.ctxGroups[0].Label != "jq playground — query line" {
		t.Fatalf("context view leads with %q, want the focused extra group", h.ctxGroups[0].Label)
	}
	last := h.ctxGroups[len(h.ctxGroups)-1].Label
	if last != "blocked (dependency not landed)" {
		t.Fatalf("an unflagged extra still trails, got %q last", last)
	}
	if h.flatGroups[0].Label == "jq playground — query line" {
		t.Fatal("the flat sheet keeps its own ordering; extras trail there")
	}
	if body := ansi.Strip(h.Render(80)); !strings.Contains(body, "esc esc") {
		t.Fatalf("the focused extra group must render:\n%s", body)
	}
}
