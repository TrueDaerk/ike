package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/project"
	"ike/internal/registry"
)

// project_switchmru_test.go covers project.switchMRU1…9 (#2489): the digit
// chords onto the recent projects — switchLast generalized past the last one.

// seedHistory installs a recent-projects history in the process-wide config,
// newest first, and restores the previous one afterwards. Production writes it
// through config.WriteKey on every completed switch; the tests care about the
// resolution, not the write-back.
func seedHistory(t *testing.T, paths ...string) {
	t.Helper()
	prev := config.Get()
	cfg := *prev
	cfg.Project.History = nil
	for _, p := range paths {
		cfg.Project.History = append(cfg.Project.History, config.ProjectHistoryEntry{
			Path: p, Name: filepath.Base(p),
		})
	}
	config.Set(&cfg)
	t.Cleanup(func() { config.Set(prev) })
}

// runSwitch executes the off-loop validation the handler returns and feeds the
// resulting message back into the model, the way the Bubble Tea runtime does.
func runSwitch(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("the chord must return the validating switch command")
	}
	msg := cmd()
	if fail, bad := msg.(project.SwitchFailedMsg); bad {
		t.Fatalf("switch failed: %v", fail.Err)
	}
	out, _ := m.Update(msg)
	return out.(Model)
}

// TestSwitchMRUFirstIsTheSwitchLastTarget is the acceptance criterion for
// digit one: ctrl+alt+1 lands where project.switchLast would — the project one
// came from — and resumes its parked workspace instead of rebuilding it.
func TestSwitchMRUFirstIsTheSwitchLastTarget(t *testing.T) {
	a, b := twoProjects(t)
	m := switchModel(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if !sameDir(t, cwd(t), b) {
		t.Fatalf("setup: cwd = %s, want b", cwd(t))
	}
	bg := m.ws.Background()
	last := bg[len(bg)-1] // what project.switchLast would resume
	wsA := m.ws.Peek(last)

	// The history production would have written by now: b (where we stand),
	// then a.
	seedHistory(t, cwd(t), a)

	out, cmd := m.Update(SwitchProjectMRUMsg{Index: 1})
	m = runSwitch(t, out.(Model), cmd)
	if !sameDir(t, cwd(t), a) {
		t.Fatalf("switchMRU1 landed in %s, want a", cwd(t))
	}
	if !sameDir(t, cwd(t), last) {
		t.Fatalf("switchMRU1 landed in %s, want switchLast's target %s", cwd(t), last)
	}
	if m.activeWS() != wsA {
		t.Fatal("switchMRU1 must resume the parked workspace, not rebuild it")
	}
}

// TestSwitchMRUPicksNthEntry covers the numbering itself: digit three is the
// third-most-recent project, counted after the current one is dropped.
func TestSwitchMRUPicksNthEntry(t *testing.T) {
	base := t.TempDir()
	var dirs []string
	for _, name := range []string{"one", "two", "three", "four"} {
		d := filepath.Join(base, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
	}
	t.Chdir(dirs[0])
	m := switchModel(t)
	seedHistory(t, cwd(t), dirs[1], dirs[2], dirs[3])

	out, cmd := m.Update(SwitchProjectMRUMsg{Index: 3})
	m = runSwitch(t, out.(Model), cmd)
	if !sameDir(t, cwd(t), dirs[3]) {
		t.Fatalf("switchMRU3 landed in %s, want %s", cwd(t), dirs[3])
	}
}

// TestSwitchMRUBeyondListNotifies is the no-op case: a digit past the end of
// the list changes nothing and says why.
func TestSwitchMRUBeyondListNotifies(t *testing.T) {
	a, _ := twoProjects(t)
	m := switchModel(t)
	seedHistory(t, cwd(t))

	out, _ := m.Update(SwitchProjectMRUMsg{Index: 4})
	m = out.(Model)
	if !sameDir(t, cwd(t), a) {
		t.Fatalf("cwd changed to %s", cwd(t))
	}
	if len(m.history) == 0 || !strings.Contains(m.history[len(m.history)-1].text, "no recent project 4") {
		t.Fatalf("expected a 'no recent project 4' notification, history = %+v", m.history)
	}
}

// TestRecentProjectsColumnShowsMRUDigits is the palette half of #2489: the
// Recent Projects column of the recent-files dialog renders each project's
// chord digit in front of its name, so the numbers are learned from the list
// the user already opens.
func TestRecentProjectsColumnShowsMRUDigits(t *testing.T) {
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
	seedHistory(t, cwd(t), alpha, beta)

	out, _ := m.Update(ShowRecentFilesMsg{})
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"1 alpha", "2 beta"} {
		if !strings.Contains(view, want) {
			t.Errorf("recent-projects column is missing %q:\n%s", want, view)
		}
	}
}

// TestSwitchMRUCommandsAndChords guards the family: nine registered commands,
// each bound to its own ctrl+alt+digit on both platforms, and each reachable
// from a focused terminal like the other project entry points (#805).
func TestSwitchMRUCommandsAndChords(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		bound := map[string]string{}
		for _, b := range keymap.DefaultsFor(keymap.PresetJetBrains, goos) {
			if strings.HasPrefix(b.Command, "project.switchMRU") {
				bound[b.Command] = b.Chord.String()
			}
		}
		for i := 1; i <= project.MaxMRU; i++ {
			id := "project.switchMRU" + strconv.Itoa(i)
			if _, ok := registry.Global().Command(id); !ok {
				t.Errorf("%s is not registered", id)
			}
			if want := "ctrl+alt+" + strconv.Itoa(i); bound[id] != want {
				t.Errorf("%s (%s) is bound to %q, want %q", id, goos, bound[id], want)
			}
			if !terminalGlobalCommands[id] {
				t.Errorf("%s must stay reachable from a focused terminal", id)
			}
		}
	}
}
