package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// group.go owns the project-group content (Epic 0510, #2569 §1): the typed
// Group, its validation rules, the lookups the switchers use, and the writers
// that persist the list and the active-group marker.
//
// Like the recent-projects history, groups persist to the *user* layer, not
// the project layer config.DefaultScope would pick for a `project.*` key: a
// group spans projects on this machine, so a per-project copy would fracture
// it. The list semantics are config's default for lists — replace, never
// append.
//
// This file stays a leaf: it validates and persists, it never mutates a
// subsystem. The root model orchestrates opening, closing and cycling by msgs.

// Group is one named project group: a name plus an ordered list of member
// roots. Roots are absolute and cleaned; their order is the open order and the
// group.next/prev cycle order. Created is informational.
type Group struct {
	Name    string
	Roots   []string
	Created time.Time
}

// fromConfigGroup decodes a persisted [[project.groups]] entry. An unparseable
// `created` yields the zero time — the entry stays usable.
func fromConfigGroup(g config.ProjectGroup) Group {
	t, _ := time.Parse(time.RFC3339, g.Created)
	roots := make([]string, len(g.Roots))
	copy(roots, g.Roots)
	return Group{Name: g.Name, Roots: roots, Created: t}
}

// toConfig encodes the group into the persisted [[project.groups]] shape, with
// `created` as RFC3339 in UTC.
func (g Group) toConfig() config.ProjectGroup {
	return config.ProjectGroup{
		Name:    g.Name,
		Roots:   g.Roots,
		Created: g.Created.UTC().Format(time.RFC3339),
	}
}

// groupRaw is the map shape config.WriteKey persists for one group entry.
func groupRaw(c config.ProjectGroup) map[string]any {
	return map[string]any{"name": c.Name, "roots": c.Roots, "created": c.Created}
}

// Groups returns cfg's project groups as typed values, in stored order. A nil
// config yields no groups, so callers can read a not-yet-loaded config.
func Groups(cfg *config.Config) []Group {
	if cfg == nil {
		return nil
	}
	out := make([]Group, len(cfg.Project.Groups))
	for i, g := range cfg.Project.Groups {
		out[i] = fromConfigGroup(g)
	}
	return out
}

// FindGroup returns the group stored under name, matched case-insensitively
// (names are unique case-insensitively, so at most one can match). An empty
// name never matches.
func FindGroup(cfg *config.Config, name string) (Group, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, false
	}
	for _, g := range Groups(cfg) {
		if strings.EqualFold(g.Name, name) {
			return g, true
		}
	}
	return Group{}, false
}

// Contains reports whether root is one of the group's members. Roots are
// compared as cleaned absolute paths, so the caller may pass an unnormalised
// one; the zero Group (no group active) contains nothing.
func (g Group) Contains(root string) bool {
	key := cleanRoot(root)
	if key == "" {
		return false
	}
	for _, r := range g.Roots {
		if cleanRoot(r) == key {
			return true
		}
	}
	return false
}

// GroupBadge is the marker a member row carries in the badge column of every
// recent-projects list (0510, #2574): `⦿ web`, the same glyph the status
// line's group slot and the group picker use. It is "" for a non-member and
// for a zero Group, so a caller can hand it every row unconditionally.
func GroupBadge(g Group, root string) string {
	if g.Name == "" || !g.Contains(root) {
		return ""
	}
	return "⦿ " + g.Name
}

// GroupContaining returns the first group — in list order — that has root
// among its members. Roots are compared as cleaned absolute paths, so the
// caller may pass an unnormalised one.
func GroupContaining(cfg *config.Config, root string) (Group, bool) {
	if cleanRoot(root) == "" {
		return Group{}, false
	}
	for _, g := range Groups(cfg) {
		if g.Contains(root) {
			return g, true
		}
	}
	return Group{}, false
}

// cleanRoot resolves a root to its absolute, cleaned form without touching the
// disk: a leading `~` expands, the rest is made absolute. It is the comparison
// key for membership tests — Validate does the same resolution plus the
// existence checks.
func cleanRoot(root string) string {
	p := strings.TrimSpace(root)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		p = filepath.Join(home, p[1:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

// ValidateGroup checks a group about to be stored and returns its normalised
// form: the name trimmed, the roots resolved to absolute cleaned paths and
// deduped in list order. The rules (epic #2569 §1): the name is non-empty and
// carries no path separator; it does not collide case-insensitively with a
// *different* stored group (the same name is the group being edited, which
// upsert replaces); at least one root remains; and every root passes Validate
// at write time. cfg may be nil to skip the collision check.
func ValidateGroup(cfg *config.Config, g Group) (Group, error) {
	name := strings.TrimSpace(g.Name)
	if name == "" {
		return Group{}, fmt.Errorf("group name is empty — enter a name")
	}
	if strings.ContainsAny(name, `/\`) {
		return Group{}, fmt.Errorf("group name %q contains a path separator — use a plain name", name)
	}
	for _, other := range Groups(cfg) {
		if strings.EqualFold(other.Name, name) && other.Name != name {
			return Group{}, fmt.Errorf("group %q collides with the existing group %q — names are unique regardless of case", name, other.Name)
		}
	}

	roots := make([]string, 0, len(g.Roots))
	seen := map[string]bool{}
	for _, r := range g.Roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		abs, err := Validate(r)
		if err != nil {
			return Group{}, err
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		roots = append(roots, abs)
	}
	if len(roots) == 0 {
		return Group{}, fmt.Errorf("group %q has no roots — add at least one project directory", name)
	}

	created := g.Created
	if created.IsZero() {
		created = time.Now()
	}
	return Group{Name: name, Roots: roots, Created: created}, nil
}

// ResolveGroupRoots splits a group's members into the ones that exist on disk
// (in list order) and the ones that do not. The open chain opens the present
// members and reports the missing ones once — the stored group is never
// edited, since a checkout can be back tomorrow.
func ResolveGroupRoots(g Group) (present, missing []string) {
	for _, r := range g.Roots {
		if abs, err := Validate(r); err == nil {
			present = append(present, abs)
		} else {
			missing = append(missing, r)
		}
	}
	return present, missing
}

// UpsertGroup validates g and writes it into the persisted group list at user
// scope: an existing group of the same name (case-insensitively) is replaced
// in place, keeping its list position; a new one is appended. An invalid group
// returns the validation error and leaves the stored list untouched.
func UpsertGroup(opts config.Options, g Group) error {
	cfg, _ := config.Load(opts)
	valid, err := ValidateGroup(cfg, g)
	if err != nil {
		return err
	}
	groups := Groups(cfg)
	replaced := false
	for i, existing := range groups {
		if strings.EqualFold(existing.Name, valid.Name) {
			groups[i] = valid
			replaced = true
			break
		}
	}
	if !replaced {
		groups = append(groups, valid)
	}
	return writeGroups(opts, groups)
}

// RemoveGroup deletes the group named name (case-insensitively) from the
// persisted list. A name that is not stored is a no-op. When the removed group
// is the active one the marker is cleared too — a marker pointing at nothing
// would survive every startup check.
func RemoveGroup(opts config.Options, name string) error {
	cfg, _ := config.Load(opts)
	var out []Group
	removed := false
	for _, g := range Groups(cfg) {
		if strings.EqualFold(g.Name, name) {
			removed = true
			continue
		}
		out = append(out, g)
	}
	if !removed {
		return nil
	}
	if err := writeGroups(opts, out); err != nil {
		return err
	}
	if cfg != nil && strings.EqualFold(cfg.Project.ActiveGroup, name) {
		return ClearActiveGroup(opts)
	}
	return nil
}

// WriteGroups persists a whole persisted-shape list at user scope after
// validating every entry — the settings list editor's write path (#2573),
// which edits the list *as a list*: a rename keeps the group's position, where
// an UpsertGroup of the new name would append a second entry. An invalid entry
// leaves the stored list untouched.
func WriteGroups(opts config.Options, groups []config.ProjectGroup) error {
	valid := make([]Group, 0, len(groups))
	for _, cg := range groups {
		// Validate against the list being written, not the stored one: a
		// rename must not collide with the entry it replaces.
		v, err := ValidateGroup(nil, fromConfigGroup(cg))
		if err != nil {
			return err
		}
		for _, seen := range valid {
			if strings.EqualFold(seen.Name, v.Name) {
				return fmt.Errorf("group %q is listed twice — names are unique regardless of case", v.Name)
			}
		}
		valid = append(valid, v)
	}
	return writeGroups(opts, valid)
}

// ValidateGroupRoot resolves one root as typed into the settings form and
// checks it (#2573): a leading `~` expands, an absolute path stands, and a
// bare name is a project inside the project directory — the clone/new-project
// rule (ProjectsDir, `project.directory`). The returned path is absolute and
// cleaned; the error is project.Validate's, naming what is wrong with it.
func ValidateGroupRoot(text string) (string, error) {
	p := strings.TrimSpace(text)
	if p == "" {
		return "", fmt.Errorf("project path is empty — enter a directory path")
	}
	abs := p
	if !filepath.IsAbs(p) && p != "~" && !strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		dir, err := ProjectsDir()
		if err != nil {
			return "", err
		}
		abs = filepath.Join(dir, p)
	}
	return Validate(abs)
}

// writeGroups persists the whole list through config's typed setter (list
// semantics: replace) at user scope.
func writeGroups(opts config.Options, groups []Group) error {
	raw := make([]map[string]any, len(groups))
	for i, g := range groups {
		raw[i] = groupRaw(g.toConfig())
	}
	return config.WriteKey(opts, config.UserScope, "project.groups", raw)
}

// ActiveGroup returns the group cfg's project.active_group marker names, if it
// still exists in the list.
func ActiveGroup(cfg *config.Config) (Group, bool) {
	if cfg == nil {
		return Group{}, false
	}
	return FindGroup(cfg, cfg.Project.ActiveGroup)
}

// SetActiveGroup writes the active-group marker at user scope. The name is
// stored as the group spells it, so the status segment reads the group's own
// capitalisation.
func SetActiveGroup(opts config.Options, name string) error {
	return config.WriteKey(opts, config.UserScope, "project.active_group", name)
}

// ClearActiveGroup drops the active-group marker (group.close, and the startup
// reconciliation below). It writes the empty string rather than removing the
// key so the user layer keeps saying "no group" explicitly.
func ClearActiveGroup(opts config.Options) error {
	return config.WriteKey(opts, config.UserScope, "project.active_group", "")
}

// ReconcileActiveGroup applies the startup rule (#2569 §1): a stored
// project.active_group survives a restart only when the process root is a
// member of that group — the marker re-establishes the status segment, the MRU
// ordering and find-in-group, it never claims a session that has moved on.
// It returns the group name still in force ("" when none) and any write error.
// Nothing is written when the marker is already absent.
func ReconcileActiveGroup(opts config.Options, root string) (string, error) {
	cfg, _ := config.Load(opts)
	if cfg == nil || strings.TrimSpace(cfg.Project.ActiveGroup) == "" {
		return "", nil
	}
	if g, ok := FindGroup(cfg, cfg.Project.ActiveGroup); ok {
		key := cleanRoot(root)
		for _, r := range g.Roots {
			if cleanRoot(r) == key {
				return g.Name, nil
			}
		}
	}
	return "", ClearActiveGroup(opts)
}

// GroupSavedMsg reports an UpsertGroupCmd outcome; Err is nil on success.
type GroupSavedMsg struct {
	Name string
	Err  error
}

// GroupRemovedMsg reports a RemoveGroupCmd outcome; Err is nil on success.
type GroupRemovedMsg struct {
	Name string
	Err  error
}

// ActiveGroupMsg reports a SetActiveGroupCmd / ClearActiveGroupCmd outcome.
// Name is "" when the marker was cleared.
type ActiveGroupMsg struct {
	Name string
	Err  error
}

// UpsertGroupCmd wraps UpsertGroup as a tea.Cmd so the Update loop never
// blocks on the validation stats or the config write (the RecordOpenCmd rule).
func UpsertGroupCmd(opts config.Options, g Group) tea.Cmd {
	return func() tea.Msg {
		return GroupSavedMsg{Name: g.Name, Err: UpsertGroup(opts, g)}
	}
}

// RemoveGroupCmd wraps RemoveGroup as a tea.Cmd, mirroring
// RemoveFromHistoryCmd.
func RemoveGroupCmd(opts config.Options, name string) tea.Cmd {
	return func() tea.Msg {
		return GroupRemovedMsg{Name: name, Err: RemoveGroup(opts, name)}
	}
}

// SetActiveGroupCmd wraps SetActiveGroup as a tea.Cmd (group.open's marker).
func SetActiveGroupCmd(opts config.Options, name string) tea.Cmd {
	return func() tea.Msg {
		return ActiveGroupMsg{Name: name, Err: SetActiveGroup(opts, name)}
	}
}

// ClearActiveGroupCmd wraps ClearActiveGroup as a tea.Cmd (group.close).
func ClearActiveGroupCmd(opts config.Options) tea.Cmd {
	return func() tea.Msg {
		return ActiveGroupMsg{Err: ClearActiveGroup(opts)}
	}
}
