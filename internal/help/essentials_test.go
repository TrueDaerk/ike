package help

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/plugin"
	"ike/internal/registry"
)

// essentialsRegistry registers a few commands that appear in the curated
// spec plus one that does not, so view switching is observable.
func essentialsRegistry() *registry.Registry {
	r := registry.New()
	r.Add(stubPlugin{id: "core", cmd: []plugin.Command{
		{ID: "palette.searchEverywhere", Title: "Search Everywhere", Scope: plugin.GlobalScope()},
		{ID: "editor.write", Title: "Save File", Scope: plugin.PaneScope("editor")},
		{ID: "settings.open", Title: "Settings", Scope: plugin.GlobalScope()},
		{ID: "obscure.command", Title: "Obscure Command", Scope: plugin.GlobalScope()},
	}})
	return r
}

func TestEssentialsSnapshotJoinsAndDrops(t *testing.T) {
	res := MapResolver{"palette.searchEverywhere": "cmd+shift+a"}
	groups := EssentialsSnapshot(essentialsRegistry(), res)

	// Only groups with at least one registered ID survive; unregistered
	// curated IDs are dropped without a panic.
	total := 0
	seen := map[string]string{}
	for _, g := range groups {
		total += len(g.Entries)
		for _, e := range g.Entries {
			seen[e.ID] = e.Shortcut
		}
	}
	if total != 3 {
		t.Fatalf("essentials should keep exactly the 3 registered curated commands, got %d", total)
	}
	if _, ok := seen["obscure.command"]; ok {
		t.Fatal("non-curated command must not appear in essentials")
	}
	if seen["palette.searchEverywhere"] != "cmd+shift+a" {
		t.Fatalf("resolver join missing, shortcut = %q", seen["palette.searchEverywhere"])
	}
}

func TestHelpOpensOnEssentialsAndTabToggles(t *testing.T) {
	h := New(essentialsRegistry(), nil, 0)
	h.Snapshot("")

	if title := h.Title(); title != "HELP — essentials" {
		t.Fatalf("default view title = %q, want essentials", title)
	}
	v := ansi.Strip(h.Render(80))
	if strings.Contains(v, "Obscure Command") {
		t.Fatalf("essentials view must not list non-curated commands: %q", v)
	}
	if !strings.Contains(v, "3 of 4 commands") || !strings.Contains(v, "tab for the full list") {
		t.Fatalf("essentials footer missing: %q", v)
	}

	// tab -> full view, listing everything.
	if !h.HandleKey("tab") {
		t.Fatal("tab must be consumed")
	}
	if title := h.Title(); title != "HELP — commands & shortcuts" {
		t.Fatalf("full view title = %q", title)
	}
	v = ansi.Strip(h.Render(80))
	if !strings.Contains(v, "Obscure Command") {
		t.Fatalf("full view should list every command: %q", v)
	}
	if !strings.Contains(v, "tab for essentials") {
		t.Fatalf("full view footer missing: %q", v)
	}

	// tab again -> back to essentials.
	h.HandleKey("tab")
	if strings.Contains(ansi.Strip(h.Render(80)), "Obscure Command") {
		t.Fatal("second tab should return to essentials")
	}

	// Non-tab keys are not consumed.
	if h.HandleKey("down") {
		t.Fatal("help must only consume tab")
	}
}

func TestFilterSearchesFullSetAndDisablesTab(t *testing.T) {
	h := New(essentialsRegistry(), nil, 0)
	h.Snapshot("")

	// A filter matches commands outside the curated set.
	h.SetFilter("obscure")
	v := ansi.Strip(h.Render(80))
	if !strings.Contains(v, "Obscure Command") {
		t.Fatalf("filter must search the full set: %q", v)
	}
	if !strings.Contains(v, "searching all commands") {
		t.Fatalf("filter footer missing: %q", v)
	}

	// tab is a no-op mid-filter but still consumed (never a scroll key).
	if !h.HandleKey("tab") {
		t.Fatal("tab should be consumed while filtering")
	}
	h.SetFilter("")
	if h.showAll {
		t.Fatal("tab during filter must not have toggled the view")
	}

	// Clearing the filter returns to the prior (essentials) view.
	if strings.Contains(ansi.Strip(h.Render(80)), "Obscure Command") {
		t.Fatal("clearing the filter should restore the essentials view")
	}
}

func TestSnapshotResetsToEssentials(t *testing.T) {
	h := New(essentialsRegistry(), nil, 0)
	h.Snapshot("")
	h.HandleKey("tab") // full view
	h.Snapshot("")     // re-open
	if h.showAll {
		t.Fatal("re-snapshot (open) must reset to the essentials view")
	}
}

func TestEssentialsDegradesToFullViewOnStubRegistry(t *testing.T) {
	// A registry with no curated command at all (the existing test stub) must
	// fall back to the full view rather than rendering an empty pane.
	h := New(testRegistry(), nil, 0)
	h.Snapshot("")
	if !h.showAll {
		t.Fatal("no resolved essentials should degrade to the full view")
	}
	if v := ansi.Strip(h.Render(80)); !strings.Contains(v, "Quit") {
		t.Fatalf("degraded view should render the full snapshot: %q", v)
	}
}
