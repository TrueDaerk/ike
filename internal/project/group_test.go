package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/config"
)

// group_test.go covers the project-group data layer (0510, #2570): validation,
// the lookups, the persisted round-trip at user scope and the active-group
// marker with its startup reconciliation.

// cfgWith builds an in-memory config carrying the given groups, the shape the
// pure lookups read.
func cfgWith(groups ...config.ProjectGroup) *config.Config {
	c := &config.Config{}
	c.Project.Groups = groups
	return c
}

func TestValidateGroupRejectsBadNames(t *testing.T) {
	root := t.TempDir()
	stored := cfgWith(config.ProjectGroup{Name: "web", Roots: []string{root}})

	cases := []struct {
		name  string
		group Group
		want  string
	}{
		{"empty name", Group{Name: "  ", Roots: []string{root}}, "is empty"},
		{"path separator", Group{Name: "a/b", Roots: []string{root}}, "path separator"},
		{"case-insensitive duplicate", Group{Name: "WEB", Roots: []string{root}}, "collides"},
		{"no roots", Group{Name: "api"}, "no roots"},
		{"non-directory root", Group{Name: "api", Roots: []string{filepath.Join(root, "nope")}}, "does not exist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateGroup(stored, tc.group); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected an error containing %q, got %v", tc.want, err)
			}
		})
	}

	// A file, not a directory: the other half of the root check.
	file := filepath.Join(root, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateGroup(nil, Group{Name: "api", Roots: []string{file}}); err == nil ||
		!strings.Contains(err.Error(), "not a directory") {
		t.Errorf("a file root should be rejected, got %v", err)
	}
}

func TestValidateGroupNormalisesRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	code := filepath.Join(home, "code")
	if err := os.Mkdir(code, 0o755); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()

	// A `~`-prefixed root expands, the repeated root collapses, and an
	// unchanged name/creation stamp comes back on the normalised group.
	got, err := ValidateGroup(nil, Group{Name: " web ", Roots: []string{"~/code", other, "~/code", code}})
	if err != nil {
		t.Fatalf("ValidateGroup: %v", err)
	}
	if got.Name != "web" {
		t.Errorf("name should be trimmed, got %q", got.Name)
	}
	if want := []string{code, other}; len(got.Roots) != 2 || got.Roots[0] != want[0] || got.Roots[1] != want[1] {
		t.Errorf("roots should expand and dedupe in list order, got %v want %v", got.Roots, want)
	}
	if got.Created.IsZero() {
		t.Error("a new group should be stamped with a creation time")
	}
}

func TestValidateGroupAllowsEditingItself(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	stored := cfgWith(config.ProjectGroup{Name: "web", Roots: []string{rootA}})
	if _, err := ValidateGroup(stored, Group{Name: "web", Roots: []string{rootA, rootB}}); err != nil {
		t.Errorf("re-saving a group under its own name should pass, got %v", err)
	}
}

func TestGroupContainingReturnsFirstInListOrder(t *testing.T) {
	shared, only := t.TempDir(), t.TempDir()
	cfg := cfgWith(
		config.ProjectGroup{Name: "web", Roots: []string{only, shared}},
		config.ProjectGroup{Name: "api", Roots: []string{shared}},
	)
	g, ok := GroupContaining(cfg, shared)
	if !ok || g.Name != "web" {
		t.Errorf("expected the first group in list order, got %+v (%v)", g, ok)
	}
	if _, ok := GroupContaining(cfg, t.TempDir()); ok {
		t.Error("a non-member root should match no group")
	}
	if _, ok := GroupContaining(cfg, ""); ok {
		t.Error("an empty root should match no group")
	}
}

func TestFindGroupIsCaseInsensitive(t *testing.T) {
	cfg := cfgWith(config.ProjectGroup{Name: "Web", Roots: []string{"/a"}})
	if g, ok := FindGroup(cfg, "wEb"); !ok || g.Name != "Web" {
		t.Errorf("FindGroup should match case-insensitively and keep the stored spelling, got %+v (%v)", g, ok)
	}
	if _, ok := FindGroup(cfg, ""); ok {
		t.Error("an empty name should never match")
	}
	if _, ok := FindGroup(nil, "web"); ok {
		t.Error("a nil config should hold no groups")
	}
}

func TestResolveGroupRootsSplitsPresentAndMissing(t *testing.T) {
	a, c := t.TempDir(), t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")
	g := Group{Name: "web", Roots: []string{a, missing, c}}

	present, absent := ResolveGroupRoots(g)
	if len(present) != 2 || present[0] != a || present[1] != c {
		t.Errorf("present roots should keep list order, got %v", present)
	}
	if len(absent) != 1 || absent[0] != missing {
		t.Errorf("missing roots should be reported separately, got %v", absent)
	}
	// The stored entry is untouched: a checkout can be back tomorrow.
	if len(g.Roots) != 3 || g.Roots[1] != missing {
		t.Errorf("ResolveGroupRoots must not edit the group, got %v", g.Roots)
	}
}

func TestUpsertGroupRoundTripsAtUserScope(t *testing.T) {
	opts := testOpts(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	created := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	if err := UpsertGroup(opts, Group{Name: "web", Roots: []string{rootA}, Created: created}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertGroup(opts, Group{Name: "api", Roots: []string{rootB}, Created: created}); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(opts)
	groups := Groups(cfg)
	if len(groups) != 2 || groups[0].Name != "web" || groups[1].Name != "api" {
		t.Fatalf("groups should round-trip in list order, got %+v", groups)
	}
	if !groups[0].Created.Equal(created) {
		t.Errorf("created should round-trip as RFC3339 UTC, got %v", groups[0].Created)
	}
	data, err := os.ReadFile(opts.UserPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[[project.groups]]") {
		t.Errorf("expected [[project.groups]] tables, got:\n%s", data)
	}

	// Re-saving replaces in place, keeping the list position.
	if err := UpsertGroup(opts, Group{Name: "web", Roots: []string{rootA, rootB}, Created: created}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.Load(opts)
	groups = Groups(cfg)
	if len(groups) != 2 || groups[0].Name != "web" || len(groups[0].Roots) != 2 {
		t.Fatalf("upsert should replace in place, got %+v", groups)
	}

	// An invalid group leaves the stored list untouched.
	if err := UpsertGroup(opts, Group{Name: "a/b", Roots: []string{rootA}}); err == nil {
		t.Error("expected a validation error")
	}
	cfg, _ = config.Load(opts)
	if len(Groups(cfg)) != 2 {
		t.Errorf("a rejected group must not change the list, got %+v", Groups(cfg))
	}
}

// TestGroupWriteBackLandsInUserFile is the epic's scope rule: a group written
// while a project layer is in play still goes to ~/.ike/settings.toml, never
// into the project's file, because the set spans projects on this machine.
func TestGroupWriteBackLandsInUserFile(t *testing.T) {
	opts := testOpts(t)
	projRoot := t.TempDir()
	opts.ProjectRoot = projRoot
	if err := os.MkdirAll(filepath.Join(projRoot, ".ike"), 0o755); err != nil {
		t.Fatal(err)
	}
	projFile := filepath.Join(projRoot, ".ike", "settings.toml")
	if err := os.WriteFile(projFile, []byte("[editor]\ntab_width = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpsertGroup(opts, Group{Name: "web", Roots: []string{projRoot}}); err != nil {
		t.Fatal(err)
	}
	if err := SetActiveGroup(opts, "web"); err != nil {
		t.Fatal(err)
	}

	user, err := os.ReadFile(opts.UserPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(user), "[[project.groups]]") || !strings.Contains(string(user), "active_group") {
		t.Errorf("group and marker belong in the user file, got:\n%s", user)
	}
	proj, err := os.ReadFile(projFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(proj), "groups") || strings.Contains(string(proj), "active_group") {
		t.Errorf("the project file must stay untouched, got:\n%s", proj)
	}
}

func TestRemoveGroupClearsTheActiveMarker(t *testing.T) {
	opts := testOpts(t)
	root := t.TempDir()
	if err := UpsertGroup(opts, Group{Name: "web", Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	if err := SetActiveGroup(opts, "web"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveGroup(opts, "WEB"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(opts)
	if len(Groups(cfg)) != 0 {
		t.Errorf("group should be gone, got %+v", Groups(cfg))
	}
	if cfg.Project.ActiveGroup != "" {
		t.Errorf("removing the active group should clear the marker, got %q", cfg.Project.ActiveGroup)
	}
	// Removing an unknown name is a no-op.
	if err := RemoveGroup(opts, "nope"); err != nil {
		t.Errorf("removing an unknown group should be a no-op, got %v", err)
	}
}

func TestReconcileActiveGroupAtStartup(t *testing.T) {
	opts := testOpts(t)
	member, outsider := t.TempDir(), t.TempDir()
	if err := UpsertGroup(opts, Group{Name: "web", Roots: []string{member}}); err != nil {
		t.Fatal(err)
	}
	if err := SetActiveGroup(opts, "web"); err != nil {
		t.Fatal(err)
	}

	// The process root is a member: the marker is kept.
	if name, err := ReconcileActiveGroup(opts, member); err != nil || name != "web" {
		t.Fatalf("a valid marker should survive, got %q, %v", name, err)
	}
	cfg, _ := config.Load(opts)
	if cfg.Project.ActiveGroup != "web" {
		t.Errorf("marker should still be stored, got %q", cfg.Project.ActiveGroup)
	}

	// Started elsewhere: the stale marker is cleared.
	if name, err := ReconcileActiveGroup(opts, outsider); err != nil || name != "" {
		t.Fatalf("a stale marker should be cleared, got %q, %v", name, err)
	}
	cfg, _ = config.Load(opts)
	if cfg.Project.ActiveGroup != "" {
		t.Errorf("marker should be cleared on disk, got %q", cfg.Project.ActiveGroup)
	}
	if _, ok := ActiveGroup(cfg); ok {
		t.Error("no group should be active after the clear")
	}

	// A marker naming a group that no longer exists is cleared too.
	if err := SetActiveGroup(opts, "ghost"); err != nil {
		t.Fatal(err)
	}
	if name, err := ReconcileActiveGroup(opts, member); err != nil || name != "" {
		t.Fatalf("a marker without a group should be cleared, got %q, %v", name, err)
	}
}

// TestWriteGroupsEditsTheListAsAList guards the settings editor's write path
// (#2573): the whole list is replaced, so a rename keeps the group's position
// where an upsert of the new name would append a second entry.
func TestWriteGroupsEditsTheListAsAList(t *testing.T) {
	opts := testOpts(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	if err := WriteGroups(opts, []config.ProjectGroup{
		{Name: "web", Roots: []string{rootA}},
		{Name: "api", Roots: []string{rootB}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(opts)
	if groups := Groups(cfg); len(groups) != 2 || groups[0].Name != "web" {
		t.Fatalf("groups = %+v", groups)
	}
	// A rename in place.
	if err := WriteGroups(opts, []config.ProjectGroup{
		{Name: "frontend", Roots: []string{rootA}},
		{Name: "api", Roots: []string{rootB}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.Load(opts)
	groups := Groups(cfg)
	if len(groups) != 2 || groups[0].Name != "frontend" || groups[1].Name != "api" {
		t.Fatalf("a rename must keep the position, got %+v", groups)
	}
	if groups[0].Created.IsZero() {
		t.Error("a written group should carry a created timestamp")
	}
	// A duplicate inside the written list, and an invalid entry, are refused
	// whole — the stored list is left untouched.
	if err := WriteGroups(opts, []config.ProjectGroup{
		{Name: "dup", Roots: []string{rootA}},
		{Name: "DUP", Roots: []string{rootB}},
	}); err == nil {
		t.Error("a duplicate name in one list must be refused")
	}
	if err := WriteGroups(opts, []config.ProjectGroup{{Name: "none"}}); err == nil {
		t.Error("a group without roots must be refused")
	}
	cfg, _ = config.Load(opts)
	if len(Groups(cfg)) != 2 {
		t.Errorf("a rejected write must not change the list, got %+v", Groups(cfg))
	}
}

// TestValidateGroupRootResolvesLikeANewProject guards the form's path rule
// (#2573): a bare name is a project inside the project directory, "~" expands
// and an absolute path stands; a missing directory is refused.
func TestValidateGroupRootResolvesLikeANewProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := config.Get()
	c := &config.Config{}
	c.Project.Directory = dir
	config.Set(c)
	t.Cleanup(func() { config.Set(prev) })

	if got, err := ValidateGroupRoot("api"); err != nil || got != filepath.Join(dir, "api") {
		t.Fatalf("relative root = %q, %v", got, err)
	}
	if got, err := ValidateGroupRoot(dir); err != nil || got != dir {
		t.Fatalf("absolute root = %q, %v", got, err)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if got, err := ValidateGroupRoot("~"); err != nil || got != filepath.Clean(home) {
			t.Fatalf("~ root = %q, %v", got, err)
		}
	}
	if _, err := ValidateGroupRoot("gone"); err == nil {
		t.Fatal("a missing directory must be refused")
	}
	if _, err := ValidateGroupRoot("  "); err == nil {
		t.Fatal("an empty root must be refused")
	}
}

func TestGroupCmdsReportTheirOutcome(t *testing.T) {
	opts := testOpts(t)
	root := t.TempDir()

	saved, ok := UpsertGroupCmd(opts, Group{Name: "web", Roots: []string{root}})().(GroupSavedMsg)
	if !ok || saved.Err != nil || saved.Name != "web" {
		t.Fatalf("UpsertGroupCmd: %+v (%v)", saved, ok)
	}
	active, ok := SetActiveGroupCmd(opts, "web")().(ActiveGroupMsg)
	if !ok || active.Err != nil || active.Name != "web" {
		t.Fatalf("SetActiveGroupCmd: %+v (%v)", active, ok)
	}
	cleared, ok := ClearActiveGroupCmd(opts)().(ActiveGroupMsg)
	if !ok || cleared.Err != nil || cleared.Name != "" {
		t.Fatalf("ClearActiveGroupCmd: %+v (%v)", cleared, ok)
	}
	removed, ok := RemoveGroupCmd(opts, "web")().(GroupRemovedMsg)
	if !ok || removed.Err != nil {
		t.Fatalf("RemoveGroupCmd: %+v (%v)", removed, ok)
	}
	cfg, _ := config.Load(opts)
	if len(Groups(cfg)) != 0 {
		t.Errorf("group should be gone, got %+v", Groups(cfg))
	}
}
