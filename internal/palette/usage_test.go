package palette

import (
	"path/filepath"
	"testing"

	"ike/internal/plugin"
	"ike/internal/registry"
)

// usageSource is a stub CommandSource with three global commands.
type usageSource struct{}

func (usageSource) Commands() []registry.OwnedCommand {
	mk := func(id, title string) registry.OwnedCommand {
		return registry.OwnedCommand{Owner: "test", Command: plugin.Command{
			ID: id, Title: title, Scope: plugin.Scope{Global: true},
		}}
	}
	return []registry.OwnedCommand{
		mk("a.alpha", "Alpha"),
		mk("b.beta", "Beta"),
		mk("c.gamma", "Gamma"),
	}
}

func TestUsagePersistsAcrossLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmdusage.json")
	u := LoadUsage(path)
	u.Bump("b.beta")
	u.Bump("b.beta")
	u.Bump("a.alpha")
	re := LoadUsage(path)
	if got := re.Count("b.beta"); got != 2 {
		t.Fatalf("persisted count = %d, want 2", got)
	}
	if got := re.Count("c.gamma"); got != 0 {
		t.Fatalf("unbumped count = %d, want 0", got)
	}
	// Nil receiver and empty id stay inert.
	var nilU *Usage
	nilU.Bump("x")
	if nilU.Count("x") != 0 {
		t.Fatal("nil Usage must count 0")
	}
}

func TestCommandModeRanksByUsageOnEqualScore(t *testing.T) {
	u := LoadUsage(filepath.Join(t.TempDir(), "cmdusage.json"))
	u.Bump("c.gamma")
	u.Bump("c.gamma")
	u.Bump("b.beta")
	c := NewCommandMode(usageSource{}, nil, false)
	c.SetUsage(u)
	// Empty query: every score is equal, so usage decides the listing order.
	items := c.Results("", Context{})
	if len(items) != 3 {
		t.Fatalf("results = %d, want 3", len(items))
	}
	if items[0].Title != "Gamma" || items[1].Title != "Beta" || items[2].Title != "Alpha" {
		t.Fatalf("usage order wrong: %q %q %q", items[0].Title, items[1].Title, items[2].Title)
	}
	// A better fuzzy match still beats a higher usage count.
	items = c.Results("alpha", Context{})
	if len(items) == 0 || items[0].Title != "Alpha" {
		t.Fatalf("match quality must win over usage, got %+v", items)
	}
}
