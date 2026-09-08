package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/allfind"
	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/project"
	"ike/internal/search"
)

// allfind_group_test.go covers project.findInGroup (0510, #2575): the
// all-projects search form restricted to the active group's members, the
// no-group refusal, and the promise that a group run leaves the remembered
// project.find_all.excluded_roots alone.

// TestFindInGroupCommandRegistered: the command exists and carries its chords.
func TestFindInGroupCommandRegistered(t *testing.T) {
	m := newSized()
	cmd, ok := m.reg.Command("project.findInGroup")
	if !ok {
		t.Fatal("project.findInGroup must be a registry command")
	}
	if cmd.Title != "Find in Project Group…" {
		t.Errorf("title = %q", cmd.Title)
	}
	// One default chord; the ctrl secondary is the delivered form off macOS,
	// the same cmd→ctrl fold project.findInAllProjects rides.
	var bound []keymap.Chord
	for _, b := range keymap.DefaultsFor(keymap.PresetJetBrains, "darwin") {
		if b.Command == "project.findInGroup" {
			bound = append(bound, b.Chord)
		}
	}
	if len(bound) != 1 || bound[0].String() != "cmd+alt+shift+d" {
		t.Fatalf("default chords = %v, want cmd+alt+shift+d", bound)
	}
	if got := keymap.NormalizeChord(bound[0], "linux").String(); got != "ctrl+alt+shift+d" {
		t.Errorf("secondary chord = %s, want ctrl+alt+shift+d", got)
	}
	if !terminalGlobalCommands["project.findInGroup"] {
		t.Error("a focused terminal must not swallow the group-search chord")
	}
}

// TestFindInGroupWithoutGroupNotifies: without an active group the command
// opens nothing and says so.
func TestFindInGroupWithoutGroupNotifies(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(OpenFindInProjectGroupMsg{})
	m = tm.(Model)
	if m.allFind.IsOpen() {
		t.Fatal("no group open — the form must stay closed")
	}
	if !strings.Contains(lastNotification(t, m), "no project group open") {
		t.Fatalf("notification = %q, want the no-group message", lastNotification(t, m))
	}
}

// TestGroupFindProjectsMapsMembers: group order, names from the history where
// known, every member checked, a vanished root marked missing.
func TestGroupFindProjectsMapsMembers(t *testing.T) {
	g := project.Group{Name: "web", Roots: []string{"/api", "/ui", "/gone"}}
	entries := []project.Entry{{Path: "/api", Name: "api-service"}}
	stat := func(path string) (os.FileInfo, error) {
		if path == "/gone" {
			return nil, errors.New("missing")
		}
		return os.Stat(".") // any real directory
	}
	got := groupFindProjects(g, entries, stat)
	if len(got) != 3 {
		t.Fatalf("got %d members", len(got))
	}
	if got[0].Root != "/api" || got[0].Name != "api-service" || got[0].Missing || got[0].Excluded {
		t.Errorf("history-named member wrong: %+v", got[0])
	}
	if got[1].Name != "ui" {
		t.Errorf("unknown member must fall back to its directory name: %+v", got[1])
	}
	if !got[2].Missing {
		t.Errorf("vanished member must be marked missing: %+v", got[2])
	}
	for _, p := range got {
		if p.Excluded {
			t.Errorf("every member starts checked, %+v is not", p)
		}
	}
}

// TestFindInGroupOpensOverMembers: with a group active the form opens over its
// members only, titled with the group.
func TestFindInGroupOpensOverMembers(t *testing.T) {
	roots := peekFixture(t, "api", "ui")
	storeGroup(t, "web", roots)
	m := switchModel(t)
	m.activeGroup = "web"

	tm, _ := m.Update(OpenFindInProjectGroupMsg{})
	m = tm.(Model)
	if !m.allFind.IsOpen() {
		t.Fatal("project.findInGroup must open the form")
	}
	if m.allFind.Group() != "web" {
		t.Fatalf("form group = %q, want web", m.allFind.Group())
	}
	m.allFind.SetSize(120, 40) // the switch model has no terminal size
	view := ansi.Strip(m.allFind.View())
	if !strings.Contains(view, "Find in Project Group") || !strings.Contains(view, "Group web (2 of 2 searched") {
		t.Errorf("the form must show the group title and members, got:\n%s", view)
	}
}

// TestGroupSearchLeavesExcludedRootsAlone: a confirmed group run persists the
// shared query/toggle/glob state but never writes the all-projects root
// selection — project.find_all.excluded_roots stays as the user left it.
func TestGroupSearchLeavesExcludedRootsAlone(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	opts := config.Discover(".")
	if err := config.WriteKey(opts, config.UserScope, "project.find_all.excluded_roots", []string{"/kept"}); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(allfind.ConfirmMsg{
		State: allfind.State{Query: "needle", ExcludedRoots: []string{"/other"}},
		Roots: []allfind.Project{{Root: root, Name: "r"}},
		Group: "web",
	})
	m = tm.(Model)
	if m.allResults.Group() != "web" {
		t.Errorf("the results must remember the group, got %q", m.allResults.Group())
	}
	m = drainCmd(m, cmd)

	data, err := os.ReadFile(filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "settings.toml"))
	if err != nil {
		t.Fatalf("user settings not written: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `query = "needle"`) {
		t.Errorf("the shared find_all state must still persist:\n%s", s)
	}
	if strings.Contains(s, `"/other"`) {
		t.Errorf("a group run must not write excluded_roots:\n%s", s)
	}
	if !strings.Contains(s, `"/kept"`) {
		t.Errorf("the remembered exclusions must survive a group run:\n%s", s)
	}
}

// TestGroupSearchCrossProjectOpen: a group run started in api and a hit opened
// in ui rides the very same pending-open path as the all-projects search — the
// switch runs, the parked open lands on the line, the result set survives.
func TestGroupSearchCrossProjectOpen(t *testing.T) {
	base := t.TempDir()
	api, ui := filepath.Join(base, "api"), filepath.Join(base, "ui")
	for _, d := range []string{api, ui} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	hit := filepath.Join(ui, "there.txt")
	if err := os.WriteFile(hit, []byte("x\nneedle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(api)
	m := switchModel(t)
	m.allResults.SetSize(m.width, m.height)
	m.allResults.BeginGroup("needle", []allfind.Project{
		{Root: api, Name: "api"}, {Root: ui, Name: "ui"},
	}, "web")
	m.allFindGen = m.allSearch.Gen()
	tm, _ := m.Update(search.MultiBatchMsg{Gen: m.allFindGen, Root: ui, Matches: []search.Match{
		{Path: hit, Line: 2, Text: "needle", StartCol: 0, EndCol: 6},
	}})
	m = tm.(Model)
	tm, _ = m.Update(search.MultiDoneMsg{Gen: m.allFindGen, Total: 1})
	m = tm.(Model)
	if !m.allResults.IsOpen() {
		t.Fatal("the group results must open on completion, like the all-projects ones")
	}
	if view := ansi.Strip(m.allResults.View()); !strings.Contains(view, "group web · 2 projects") {
		t.Errorf("the overlay header must name the searched set, got:\n%s", view)
	}

	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	m = stepCmd(m, cmd) // the OpenMatchMsg
	if m.allPendingOpen == nil || m.allPendingOpen.Root != ui {
		t.Fatal("the foreign member's hit must park a pending open")
	}
	m = stepCmd(m, project.SwitchTo(ui))
	tm, _ = m.Update(project.SwitchedMsg{Root: ui})
	m = tm.(Model)
	ed := m.activeEditor()
	if ed == nil || ed.Path() != hit {
		t.Fatal("the match's file must be open after the switch")
	}
	if line, _ := ed.Cursor(); line != 2 {
		t.Errorf("cursor line = %d, want the hit's line (1-based 2)", line)
	}
	if m.allResults.Total() != 1 || m.allResults.Group() != "web" {
		t.Errorf("the group result set must survive the switch: total=%d group=%q",
			m.allResults.Total(), m.allResults.Group())
	}
}
