package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/project"
)

// project_mru_group_test.go covers the group half of the MRU order in the app
// (0510, #2574): the Recent Projects column and the digit chords read the one
// project.MRUOrder, so an active group moves its members to the front of both
// at once.

// seedActiveGroup installs a group plus its active marker in the process-wide
// config and restores the previous one afterwards — the in-memory half of what
// project.UpsertGroup + SetActiveGroup persist.
func seedActiveGroup(t *testing.T, name string, roots ...string) {
	t.Helper()
	prev := config.Get()
	cfg := *prev
	cfg.Project.Groups = []config.ProjectGroup{{Name: name, Roots: roots}}
	cfg.Project.ActiveGroup = name
	config.Set(&cfg)
	t.Cleanup(func() { config.Set(prev) })
}

// TestRecentProjectsColumnPutsGroupMembersFirst is the column half of the
// acceptance: with a group active its members lead the column, in their MRU
// order, each carrying the `⦿ <group>` badge.
func TestRecentProjectsColumnPutsGroupMembersFirst(t *testing.T) {
	base := t.TempDir()
	alpha, beta := filepath.Join(base, "alpha"), filepath.Join(base, "beta")
	here := filepath.Join(base, "here")
	for _, d := range []string{alpha, beta, here} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(here)
	m := sized(t, 120, 40)
	// The group spans the project we stand in and beta, the *older* of the
	// two recent projects — so a passing order cannot be the plain MRU one.
	seedActiveGroup(t, "web", cwd(t), beta)
	seedHistory(t, cwd(t), alpha, beta)

	out, _ := m.Update(ShowRecentFilesMsg{})
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	i, j := strings.Index(view, "beta"), strings.Index(view, "alpha")
	if i < 0 || j < 0 || i > j {
		t.Errorf("column order = beta@%d alpha@%d, want the group member beta first:\n%s", i, j, view)
	}
	if !strings.Contains(view, "⦿ web") {
		t.Errorf("member row is missing the group badge:\n%s", view)
	}
}

// TestSwitchMRUFollowsGroupOrder is the chord half: ctrl+alt+1 lands on the
// first *row* of the reordered list, i.e. on a group member, even though a
// non-member is the more recently used project.
func TestSwitchMRUFollowsGroupOrder(t *testing.T) {
	base := t.TempDir()
	var dirs []string
	for _, name := range []string{"here", "other", "member"} {
		d := filepath.Join(base, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
	}
	t.Chdir(dirs[0])
	m := switchModel(t)
	seedActiveGroup(t, "web", cwd(t), dirs[2])
	// MRU without the group: other, member. With it, member leads.
	seedHistory(t, cwd(t), dirs[1], dirs[2])

	if got := mruProjectTargets(); len(got) != 2 || filepath.Base(got[0]) != "member" {
		t.Fatalf("targets = %v, want the group member first", got)
	}
	out, cmd := m.Update(SwitchProjectMRUMsg{Index: 1})
	m = runSwitch(t, out.(Model), cmd)
	if !sameDir(t, cwd(t), dirs[2]) {
		t.Fatalf("switchMRU1 landed in %s, want the group member %s", cwd(t), dirs[2])
	}
}

// TestActiveProjectGroupWithoutMarker is the no-group case both halves rest
// on: no marker, the zero group, no reordering and no badge.
func TestActiveProjectGroupWithoutMarker(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	prev := config.Get()
	cfg := *prev
	cfg.Project.ActiveGroup = ""
	config.Set(&cfg)
	t.Cleanup(func() { config.Set(prev) })

	if g := activeProjectGroup(); g.Name != "" || len(g.Roots) != 0 {
		t.Errorf("no marker must resolve to the zero group, got %+v", g)
	}
	if got := project.GroupBadge(activeProjectGroup(), "/code/ike"); got != "" {
		t.Errorf("no marker must badge nothing, got %q", got)
	}
}
