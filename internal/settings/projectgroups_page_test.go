package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// testGroupOps mirrors internal/project's group rules for the page tests: the
// package cannot import internal/project (the cycle projectgroups_page.go
// documents), so the seam is filled here and the real wiring is guarded in
// internal/app (projectgroups_settings_test.go).
func testGroupOps(opts config.Options) ProjectGroupOps {
	write := func(groups []config.ProjectGroup) error {
		raw := make([]map[string]any, len(groups))
		for i, g := range groups {
			created := g.Created
			if created == "" {
				created = time.Now().UTC().Format(time.RFC3339)
			}
			raw[i] = map[string]any{"name": g.Name, "roots": g.Roots, "created": created}
		}
		return config.WriteKey(opts, config.UserScope, "project.groups", raw)
	}
	return ProjectGroupOps{
		ResolveRoot: func(text string) (string, error) {
			p := strings.TrimSpace(text)
			if p == "" {
				return "", fmt.Errorf("project path is empty — enter a directory path")
			}
			if p == "~" || strings.HasPrefix(p, "~"+string(filepath.Separator)) {
				home, _ := os.UserHomeDir()
				p = filepath.Join(home, p[1:])
			} else if !filepath.IsAbs(p) {
				p = filepath.Join(config.Get().Project.Directory, p)
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				return "", err
			}
			info, err := os.Stat(abs)
			switch {
			case os.IsNotExist(err):
				return "", fmt.Errorf("%s does not exist — check the path", abs)
			case err != nil:
				return "", err
			case !info.IsDir():
				return "", fmt.Errorf("%s is not a directory", abs)
			}
			return filepath.Clean(abs), nil
		},
		Validate: func(name string, roots []string) (string, []string, error) {
			if strings.ContainsAny(name, `/\`) {
				return "", nil, fmt.Errorf("group name %q contains a path separator — use a plain name", name)
			}
			out, seen := make([]string, 0, len(roots)), map[string]bool{}
			for _, r := range roots {
				if !seen[r] {
					seen[r], out = true, append(out, r)
				}
			}
			if len(out) == 0 {
				return "", nil, fmt.Errorf("group %q has no roots — add at least one project directory", name)
			}
			return name, out, nil
		},
		Write:  write,
		Remove: func(name string) error { return removeTestGroup(write, name) },
		Compact: func(p string) string {
			if home, err := os.UserHomeDir(); err == nil {
				if rel, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
					return "~" + string(filepath.Separator) + rel
				}
			}
			return p
		},
	}
}

// removeTestGroup drops one group by name and rewrites the list.
func removeTestGroup(write func([]config.ProjectGroup) error, name string) error {
	var out []config.ProjectGroup
	for _, g := range config.Get().Project.Groups {
		if !strings.EqualFold(g.Name, name) {
			out = append(out, g)
		}
	}
	return write(out)
}

// groupsPage builds the page over an isolated user/project config pair.
func groupsPage(t *testing.T) (*ProjectGroupsPage, *stubHost, config.Options) {
	t.Helper()
	restoreConfig(t)
	opts := config.Options{
		UserPath:    filepath.Join(t.TempDir(), "settings.toml"),
		ProjectRoot: t.TempDir(),
	}
	p := NewProjectGroupsPage(opts, testGroupOps(opts))
	h := &stubHost{}
	p.SetSubPanelHost(h)
	return p, h, opts
}

func groupFormOf(t *testing.T, h *stubHost) *groupForm {
	t.Helper()
	f, ok := h.top().(*groupForm)
	if !ok {
		t.Fatalf("expected an open group form sub-panel, got %T", h.top())
	}
	return f
}

func typeGroup(f *groupForm, s string) {
	for _, r := range s {
		f.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
}

// addGroup drives the full add flow: open the form, type name and roots, save.
func addGroup(t *testing.T, p *ProjectGroupsPage, h *stubHost, name string, roots ...string) {
	t.Helper()
	p.Update(key("a"))
	f := groupFormOf(t, h)
	typeGroup(f, name)
	for i, r := range roots {
		if i > 0 {
			f.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
		} else {
			f.Update(key("tab"))
		}
		typeGroup(f, r)
	}
	apply(t, f.Update(key("enter")))
	if h.top() != nil {
		t.Fatal("a successful save must pop the form")
	}
}

// TestProjectGroupsAddEditDelete guards #2573: the page's CRUD round-trips
// [[project.groups]] through the user layer and never through the project one.
func TestProjectGroupsAddEditDelete(t *testing.T) {
	p, h, opts := groupsPage(t)
	api, ui := t.TempDir(), t.TempDir()
	addGroup(t, p, h, "web", api, ui)

	got := config.Get().Project.Groups
	if len(got) != 1 || got[0].Name != "web" || len(got[0].Roots) != 2 {
		t.Fatalf("groups after add = %+v", got)
	}
	if got[0].Roots[0] != api || got[0].Roots[1] != ui {
		t.Fatalf("roots after add = %v, want %v", got[0].Roots, []string{api, ui})
	}
	// User scope: ~/.ike/settings.toml holds it, <root>/.ike/settings.toml
	// never does — a group spans projects.
	data, err := os.ReadFile(opts.UserPath)
	if err != nil || !strings.Contains(string(data), "web") {
		t.Fatalf("user settings file = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(opts.ProjectRoot, ".ike", "settings.toml")); !os.IsNotExist(err) {
		t.Fatalf("a group must never reach the project layer (stat err %v)", err)
	}

	// Edit: rename, keeping the roots and the list position.
	p.Update(key("enter"))
	f := groupFormOf(t, h)
	f.Update(key("backspace"))
	f.Update(key("backspace"))
	f.Update(key("backspace"))
	typeGroup(f, "frontend")
	apply(t, f.Update(key("enter")))
	got = config.Get().Project.Groups
	if len(got) != 1 || got[0].Name != "frontend" || len(got[0].Roots) != 2 {
		t.Fatalf("groups after rename = %+v", got)
	}

	// Delete goes through the shared confirm.
	p.Update(key("d"))
	apply(t, confirmVia(t, h))
	if got := config.Get().Project.Groups; len(got) != 0 {
		t.Fatalf("groups after delete = %+v", got)
	}
}

// TestProjectGroupsRenameKeepsPosition: editing the first of two groups keeps
// it first — the page writes the list as a list.
func TestProjectGroupsRenameKeepsPosition(t *testing.T) {
	p, h, _ := groupsPage(t)
	addGroup(t, p, h, "alpha", t.TempDir())
	addGroup(t, p, h, "beta", t.TempDir())
	p.sel = 0
	p.Update(key("enter"))
	f := groupFormOf(t, h)
	typeGroup(f, "-two")
	apply(t, f.Update(key("enter")))
	got := config.Get().Project.Groups
	if len(got) != 2 || got[0].Name != "alpha-two" || got[1].Name != "beta" {
		t.Fatalf("groups after rename = %+v", got)
	}
}

// TestProjectGroupsFormValidation: every rejected form keeps the panel open
// and says what is wrong.
func TestProjectGroupsFormValidation(t *testing.T) {
	p, h, _ := groupsPage(t)
	root := t.TempDir()
	addGroup(t, p, h, "web", root)

	cases := []struct {
		name       string
		fill       func(f *groupForm)
		wantNoteIn string
	}{
		{"empty name", func(f *groupForm) {}, "name is required"},
		{"duplicate name", func(f *groupForm) {
			typeGroup(f, "WEB")
			f.Update(key("tab"))
			typeGroup(f, root)
		}, `name already used by "web"`},
		{"no roots", func(f *groupForm) { typeGroup(f, "empty") }, "add at least one project root"},
		{"missing directory", func(f *groupForm) {
			typeGroup(f, "gone")
			f.Update(key("tab"))
			typeGroup(f, filepath.Join(root, "nope"))
		}, "root 1:"},
		{"path separator in the name", func(f *groupForm) {
			typeGroup(f, "a/b")
			f.Update(key("tab"))
			typeGroup(f, root)
		}, "path separator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p.Update(key("a"))
			f := groupFormOf(t, h)
			tc.fill(f)
			if cmd := f.Update(key("enter")); cmd != nil {
				t.Fatal("an invalid form must not write")
			}
			if h.top() != f {
				t.Fatal("an invalid form must stay open")
			}
			if !strings.Contains(f.note, tc.wantNoteIn) {
				t.Fatalf("note = %q, want it to contain %q", f.note, tc.wantNoteIn)
			}
			f.Update(key("esc"))
		})
	}
	if got := config.Get().Project.Groups; len(got) != 1 {
		t.Fatalf("no rejected form may change the stored list, got %+v", got)
	}
}

// TestProjectGroupsFormAcceptsHomeAndProjectRelative guards the two path
// conveniences: "~/x" expands and a bare name resolves against the projects
// directory (project.directory, the clone/new-project rule).
func TestProjectGroupsFormAcceptsHomeAndProjectRelative(t *testing.T) {
	p, h, _ := groupsPage(t)
	// A projects directory with one project inside it.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := config.Get()
	c.Project.Directory = dir
	config.Set(c)

	addGroup(t, p, h, "rel", "api")
	got := config.Get().Project.Groups
	if len(got) != 1 || got[0].Roots[0] != filepath.Join(dir, "api") {
		t.Fatalf("relative root = %+v, want %s", got, filepath.Join(dir, "api"))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	p.Update(key("a"))
	f := groupFormOf(t, h)
	typeGroup(f, "home")
	f.Update(key("tab"))
	typeGroup(f, "~")
	apply(t, f.Update(key("enter")))
	got = config.Get().Project.Groups
	if len(got) != 2 || got[1].Roots[0] != filepath.Clean(home) {
		t.Fatalf("~ root = %+v, want %s", got, home)
	}
}

// TestProjectGroupsFormRootRows: +/alt+enter add a row, -/alt+backspace remove
// an empty one, tab cycles every field and a paste lands in the focused root.
func TestProjectGroupsFormRootRows(t *testing.T) {
	p, h, _ := groupsPage(t)
	p.Update(key("a"))
	f := groupFormOf(t, h)
	if len(f.roots) != 1 {
		t.Fatalf("a new form starts with one root row, got %d", len(f.roots))
	}
	f.Update(key("tab")) // onto root 1, still empty
	f.Update(key("+"))
	if len(f.roots) != 2 || f.field != 2 {
		t.Fatalf("+ must add a row and focus it: roots=%d field=%d", len(f.roots), f.field)
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if len(f.roots) != 3 {
		t.Fatalf("alt+enter must add a row, got %d", len(f.roots))
	}
	f.Update(key("-"))
	if len(f.roots) != 2 {
		t.Fatalf("- on an empty row must remove it, got %d", len(f.roots))
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if len(f.roots) != 1 {
		t.Fatalf("alt+backspace on an empty row must remove it, got %d", len(f.roots))
	}

	// Paste into the focused root field (#2002), then tab wraps through name
	// and root.
	if !f.Paste("/tmp/pasted") {
		t.Fatal("paste must land in the focused field")
	}
	if f.roots[0] != "/tmp/pasted" {
		t.Fatalf("pasted root = %q", f.roots[0])
	}
	// A filled row keeps "+" and "-" as text: a directory may be named c++.
	f.Update(key("+"))
	if len(f.roots) != 1 || !strings.HasSuffix(f.roots[0], "+") {
		t.Fatalf("+ on a filled row must type: roots=%v", f.roots)
	}
	f.Update(key("tab"))
	if f.field != 0 {
		t.Fatalf("tab must wrap back to the name field, got %d", f.field)
	}
}

// TestProjectGroupsPageRender: the list names each group with its root count
// and first root; an empty page says how to add one.
func TestProjectGroupsPageRender(t *testing.T) {
	p, h, _ := groupsPage(t)
	if v := p.View(80, 12); !strings.Contains(v, "press a to add one") {
		t.Fatalf("empty state must say how to add a group:\n%s", v)
	}
	a, b := t.TempDir(), t.TempDir()
	addGroup(t, p, h, "web", a, b)
	v := p.View(120, 12)
	for _, want := range []string{"web", "2 roots", filepath.Base(a)} {
		if !strings.Contains(v, want) {
			t.Fatalf("view must contain %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, filepath.Base(b)) {
		t.Fatalf("only the first root belongs on the row:\n%s", v)
	}
}

// TestProjectGroupsOpenVerb guards the "o" verb: it dispatches for the row's
// group, and reports itself when nothing is wired up.
func TestProjectGroupsOpenVerb(t *testing.T) {
	p, h, _ := groupsPage(t)
	addGroup(t, p, h, "web", t.TempDir())
	if cmd := p.Update(key("o")); cmd != nil {
		t.Fatal("without a dispatcher o must not produce a command")
	}
	if p.note == "" {
		t.Fatal("without a dispatcher o must explain itself")
	}
	var opened string
	p.SetGroupOpen(func(name string) tea.Cmd {
		opened = name
		return func() tea.Msg { return nil }
	})
	if cmd := p.Update(key("o")); cmd == nil {
		t.Fatal("o must dispatch project.group.open")
	}
	if opened != "web" {
		t.Fatalf("opened %q, want web", opened)
	}
}

// TestProjectGroupsPageActions: the delete/open verbs are offered only with a
// row under the cursor.
func TestProjectGroupsPageActions(t *testing.T) {
	p, h, _ := groupsPage(t)
	for _, a := range p.Actions() {
		if a.Key == "o" && a.Enabled != nil && a.Enabled() {
			t.Fatal("o must be disabled on an empty list")
		}
	}
	addGroup(t, p, h, "web", t.TempDir())
	var keys []string
	for _, a := range p.Actions() {
		if a.Enabled == nil || a.Enabled() {
			keys = append(keys, a.Key)
		}
	}
	if strings.Join(keys, ",") != "a,enter,d,o" {
		t.Fatalf("actions = %v", keys)
	}
}
