package app

import (
	"testing"

	"ike/internal/config"
)

// workspace_evict_group_test.go covers the group cap protection (0510,
// #2570): while a group is active the background cap grows to hold every
// member, and the eviction sweep drops non-members first.

// setActiveGroupConfig installs a config with one group and marks it active,
// restoring the previous global config afterwards.
func setActiveGroupConfig(t *testing.T, maxWS int, name string, roots []string) {
	t.Helper()
	prev := config.Get()
	t.Cleanup(func() { config.Set(prev) })
	c := &config.Config{}
	c.Project.MaxWorkspaces = maxWS
	c.Project.Groups = []config.ProjectGroup{{Name: name, Roots: roots}}
	c.Project.ActiveGroup = name
	config.Set(c)
}

func TestMaxWorkspacesGrowsToTheActiveGroup(t *testing.T) {
	members := []string{"/a", "/b", "/c", "/d"}
	setActiveGroupConfig(t, 2, "web", members)
	if got := maxWorkspaces(); got != len(members) {
		t.Errorf("cap should rise to the member count, got %d want %d", got, len(members))
	}

	// A configured cap above the member count wins — the group only raises it.
	setActiveGroupConfig(t, 9, "web", members)
	if got := maxWorkspaces(); got != 9 {
		t.Errorf("a larger configured cap should stand, got %d", got)
	}

	// Marker without a matching group: the plain configured cap applies.
	prev := config.Get()
	t.Cleanup(func() { config.Set(prev) })
	c := &config.Config{}
	c.Project.MaxWorkspaces = 2
	c.Project.ActiveGroup = "ghost"
	config.Set(c)
	if got := maxWorkspaces(); got != 2 {
		t.Errorf("a stale marker should not raise the cap, got %d", got)
	}
}

func TestEvictionOrderPrefersNonMembers(t *testing.T) {
	setActiveGroupConfig(t, 3, "web", []string{"/m1", "/m2"})

	// Background is LRU-first; the two members sit at the front of it.
	bg := []string{"/m1", "/x", "/m2", "/y"}
	got := evictionOrder(bg)
	want := []string{"/x", "/y", "/m1", "/m2"}
	if len(got) != len(want) {
		t.Fatalf("order should keep every root, got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("members must be evicted last: got %v want %v", got, want)
		}
	}
}

func TestEvictionOrderUnchangedWithoutAGroup(t *testing.T) {
	prev := config.Get()
	t.Cleanup(func() { config.Set(prev) })
	config.Set(&config.Config{})

	bg := []string{"/a", "/b", "/c"}
	got := evictionOrder(bg)
	for i := range bg {
		if got[i] != bg[i] {
			t.Fatalf("without a group the LRU order stands: got %v want %v", got, bg)
		}
	}
	if len(activeGroupMembers()) != 0 {
		t.Error("no group should mean no members")
	}
}
