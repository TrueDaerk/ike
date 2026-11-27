package palette

import (
	"path/filepath"
	"slices"
	"testing"

	"ike/internal/fuzzy"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// classCommands holds one command per match class for the query "gtl", all in
// the editor context, plus a global and an off-context command.
func classCommands() []registry.OwnedCommand {
	ed := plugin.PaneScope("editor")
	return []registry.OwnedCommand{
		owned("e.fuzzy", "Getting Tall", ed),
		owned("e.initials", "Go To Line", ed),
		owned("e.contains", "Run Gtl Check", ed),
		owned("e.prefix", "Gtlx Toggle", ed),
		owned("e.exact", "GTL", ed),
		owned("g.prefix", "Gtl Global", plugin.GlobalScope()),
		owned("o.exact", "Gtl", plugin.PaneScope("explorer")),
	}
}

// hotCommandMode is a command mode over classCommands whose weaker matches
// carry heavy frecency and usage.
func hotCommandMode(t *testing.T, hideOff bool) *CommandMode {
	t.Helper()
	f := LoadFrecency(filepath.Join(t.TempDir(), "cmdfrecency.json"))
	u := LoadUsage(filepath.Join(t.TempDir(), "usage.json"))
	for i := 0; i < 50; i++ {
		for _, id := range []string{"e.fuzzy", "e.initials", "e.contains"} {
			f.Record(id)
			u.Bump(id)
		}
	}
	c := NewCommandMode(fakeSource{cmds: classCommands()}, nil, hideOff)
	c.SetFrecency(f)
	c.SetUsage(u)
	return c
}

func TestCommandClassFixture(t *testing.T) {
	want := map[string]fuzzy.Class{
		"Getting Tall":  fuzzy.ClassFuzzy,
		"Go To Line":    fuzzy.ClassInitials,
		"Run Gtl Check": fuzzy.ClassContains,
		"Gtlx Toggle":   fuzzy.ClassPrefix,
		"GTL":           fuzzy.ClassExact,
	}
	opts := fuzzy.ClassOptions{Case: fuzzy.CaseNone, Permissive: true}
	for title, class := range want {
		if got, _, ok := fuzzy.Classify("gtl", title, opts); !ok || got != class {
			t.Errorf("%q: class %v (ok=%v), want %v", title, got, ok, class)
		}
	}
}

func TestCommandModeRanksByMatchClass(t *testing.T) {
	setMatchOrder(t, fuzzy.ClassNames()...)
	c := hotCommandMode(t, false)
	got := titles(c.PrimaryResults("gtl", Context{ContextID: "editor"}))
	want := []string{"GTL", "Gtlx Toggle", "Run Gtl Check", "Go To Line", "Getting Tall", "Gtl Global", "Gtl"}
	if !slices.Equal(got, want) {
		t.Fatalf("default order:\n got %q\nwant %q", got, want)
	}
}

func TestCommandModeFollowsConfiguredMatchOrder(t *testing.T) {
	setMatchOrder(t, fuzzy.ClassNames()...)
	c := hotCommandMode(t, false)
	cx := Context{ContextID: "editor"}
	c.PrimaryResults("gtl", cx)

	setMatchOrder(t, "fuzzy", "initials", "directory", "contains", "prefix", "exact")
	got := titles(c.PrimaryResults("gtl", cx))
	want := []string{"Getting Tall", "Go To Line", "Run Gtl Check", "Gtlx Toggle", "GTL", "Gtl Global", "Gtl"}
	if !slices.Equal(got, want) {
		t.Fatalf("permuted order:\n got %q\nwant %q", got, want)
	}
}

func TestCommandModeClassKeepsContextTiers(t *testing.T) {
	setMatchOrder(t, fuzzy.ClassNames()...)
	got := titles(hotCommandMode(t, true).PrimaryResults("gtl", Context{ContextID: "editor"}))
	if slices.Contains(got, "Gtl") {
		t.Fatalf("hideOff must drop the off-context command: %q", got)
	}
	if got[len(got)-1] != "Gtl Global" {
		t.Fatalf("global prefix match must list below every in-context one: %q", got)
	}
}
