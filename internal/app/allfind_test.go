package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/allfind"
	"ike/internal/project"
	"ike/internal/search"
)

// allFindApp opens the all-projects search form.
func allFindApp(t *testing.T) Model {
	t.Helper()
	m := newSized()
	tm, _ := m.Update(OpenFindInAllProjectsMsg{})
	m = tm.(Model)
	if !m.allFind.IsOpen() {
		t.Fatal("project.findInAllProjects must open the form")
	}
	return m
}

func TestFindInAllProjectsCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"project.findInAllProjects", "project.findInAllProjectsResults"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("%s must be a registry command", id)
		}
	}
}

// TestAllFindProjectsMapsHistory covers the history-to-project-list mapping:
// order kept, exclusion seeded, missing roots marked.
func TestAllFindProjectsMapsHistory(t *testing.T) {
	entries := []project.Entry{
		{Path: "/live", Name: "live", LastOpened: time.Now()},
		{Path: "/gone", Name: "gone"},
	}
	stat := func(path string) (os.FileInfo, error) {
		if path == "/live" {
			return os.Stat(".") // any real directory
		}
		return nil, errors.New("missing")
	}
	got := allFindProjects(entries, []string{"/live"}, stat)
	if len(got) != 2 {
		t.Fatalf("got %d projects", len(got))
	}
	if got[0].Root != "/live" || !got[0].Excluded || got[0].Missing {
		t.Fatalf("live entry wrong: %+v", got[0])
	}
	if got[1].Root != "/gone" || !got[1].Missing || got[1].Excluded {
		t.Fatalf("gone entry wrong: %+v", got[1])
	}
}

// TestAllFindConfirmStartsBackgroundScanAndPersists drives the form to
// confirm: the form closes (editor regains the keyboard), the scan starts
// with a fresh generation, and the state persists to the user layer.
func TestAllFindConfirmStartsBackgroundScanAndPersists(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := allFindApp(t)
	before := m.allSearch.Gen()
	tm, cmd := m.Update(allfind.ConfirmMsg{
		State: allfind.State{Query: "needle", CaseSensitive: true, Include: "*.go"},
		Roots: []allfind.Project{{Root: root, Name: "r"}},
	})
	m = tm.(Model)
	if m.allFindGen != before+1 {
		t.Fatalf("gen = %d, want %d (scan must have started)", m.allFindGen, before+1)
	}
	if m.allResults.Visible() {
		t.Fatal("the popup must stay hidden until the scan finishes")
	}
	// The persistence cmd writes the user layer and reloads.
	m = drainCmd(m, cmd)
	fa := readUserFindAll(t)
	if fa.Query != "needle" || !fa.CaseSensitive || len(fa.Include) != 1 || fa.Include[0] != "*.go" {
		t.Fatalf("persisted state wrong: %+v", fa)
	}
}

// readUserFindAll loads the persisted user-layer find_all table.
func readUserFindAll(t *testing.T) (fa struct {
	Query         string
	CaseSensitive bool
	Include       []string
}) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "settings.toml"))
	if err != nil {
		t.Fatalf("user settings not written: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "[project.find_all]") {
		t.Fatalf("no [project.find_all] table in:\n%s", s)
	}
	fa.Query = "needle" // presence asserted textually below; keep it simple
	if !strings.Contains(s, `query = "needle"`) {
		fa.Query = ""
	}
	fa.CaseSensitive = strings.Contains(s, "case_sensitive = true")
	if strings.Contains(s, `"*.go"`) {
		fa.Include = []string{"*.go"}
	}
	return fa
}

// TestAllFindResultsPopupDoesNotStealFocus routes a finished scan through the
// app: the popup shows, the editor keeps the keyboard, the results command
// focuses it, esc hides it and the command brings it back.
func TestAllFindResultsPopupFocusBehaviour(t *testing.T) {
	m := newSized()
	m.allResults.Begin("needle", []allfind.Project{{Root: "/a", Name: "alpha"}})
	m.allFindGen = m.allSearch.Gen()
	tm, _ := m.Update(search.MultiBatchMsg{Gen: m.allFindGen, Root: "/a", Matches: []search.Match{
		{Path: "/a/x.go", Line: 1, Text: "needle", StartCol: 0, EndCol: 6},
	}})
	m = tm.(Model)
	tm, _ = m.Update(search.MultiDoneMsg{Gen: m.allFindGen, Total: 1})
	m = tm.(Model)
	if !m.allResults.Visible() {
		t.Fatal("the popup must appear when the scan finishes")
	}
	if m.allResults.Focused() {
		t.Fatal("the popup must NOT take focus on completion")
	}
	// A key while unfocused is never routed to the popup (the funnel gates on
	// Focused()): the cursor stays put even for keys the popup handles.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = tm.(Model)
	if m.allResults.Focused() {
		t.Fatal("a key press must not focus the popup")
	}
	// The show-results command focuses it.
	tm, _ = m.Update(ShowAllFindResultsMsg{})
	m = tm.(Model)
	if !m.allResults.Focused() {
		t.Fatal("the results command must focus the popup")
	}
	// Esc hides; the results stay for the next show.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.allResults.Visible() {
		t.Fatal("esc must hide the popup")
	}
	tm, _ = m.Update(ShowAllFindResultsMsg{})
	m = tm.(Model)
	if !m.allResults.Visible() || !m.allResults.Focused() {
		t.Fatal("the results command must bring the popup back")
	}
}

// TestAllFindStaleGenerationsDrop guards the rerun-cancels-previous contract
// at the routing layer.
func TestAllFindStaleGenerationsDrop(t *testing.T) {
	m := newSized()
	m.allResults.Begin("q", []allfind.Project{{Root: "/a", Name: "alpha"}})
	m.allFindGen = 7
	tm, _ := m.Update(search.MultiBatchMsg{Gen: 6, Root: "/a", Matches: []search.Match{
		{Path: "/a/x.go", Line: 1, Text: "q", StartCol: 0, EndCol: 1},
	}})
	m = tm.(Model)
	if m.allResults.Total() != 0 {
		t.Fatal("a stale batch must be dropped")
	}
	tm, _ = m.Update(search.MultiDoneMsg{Gen: 6, Total: 1})
	m = tm.(Model)
	if m.allResults.Visible() {
		t.Fatal("a stale done must not open the popup")
	}
}

// TestAllFindEnterInCurrentProjectOpens routes a match in the current project
// straight to the editor, no switch.
func TestAllFindEnterInCurrentProjectOpens(t *testing.T) {
	m := newSized()
	cwd, _ := os.Getwd() // tests run in internal/app — allfind.go is right here
	path := filepath.Join(cwd, "allfind.go")
	tm, _ := m.Update(allfind.OpenMatchMsg{Root: cwd, Path: path, Line: 2, Col: 0})
	m = tm.(Model)
	if m.allPendingOpen != nil {
		t.Fatal("a current-project match must not arm a pending switch open")
	}
	if ed := m.activeEditor(); ed == nil || !strings.HasSuffix(ed.Path(), "allfind.go") {
		t.Fatal("the match's file must be open in the editor")
	}
}

// TestAllFindEnterInOtherProjectArmsSwitch parks the open and starts the
// validated switch; the SwitchedMsg handler finishes it.
func TestAllFindEnterInOtherProjectArmsSwitch(t *testing.T) {
	m := newSized()
	other := t.TempDir()
	tm, cmd := m.Update(allfind.OpenMatchMsg{Root: other, Path: filepath.Join(other, "x.go"), Line: 1, Col: 0})
	m = tm.(Model)
	if m.allPendingOpen == nil || m.allPendingOpen.Root != other {
		t.Fatal("a foreign match must park a pending open")
	}
	found := false
	for _, msg := range cmdMsgs(cmd) {
		if sw, ok := msg.(project.SwitchProjectMsg); ok && sw.Root == other {
			found = true
		}
	}
	if !found {
		t.Fatal("a foreign match must dispatch the project switch")
	}
}

// TestAllFindFormConfirmClosesOverlay: enter on the form dispatches the
// confirm and closes the overlay in the same step — the key funnel only
// routes to the form while IsOpen, so the editor has the keyboard back.
func TestAllFindFormConfirmClosesOverlay(t *testing.T) {
	m := allFindApp(t)
	m.allFind.Open(allfind.State{Query: "q"}, []allfind.Project{{Root: t.TempDir(), Name: "r"}}, "")
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.allFind.IsOpen() {
		t.Fatal("confirming must close the form")
	}
	if cmd == nil {
		t.Fatal("confirming must dispatch the confirm cmd")
	}
}
