package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/registry"
	"ike/internal/telemetry"
)

// recentRowsFixture builds the #3083 scenario on the real registry with
// telemetry on: "alpha" is parked behind a switch to "here" (open, ●), and
// "gamma" is a history-only entry (closed). The recent-files dialog is open
// with no recent files, so its Recent Projects column holds the focus on
// alpha, the first row.
func recentRowsFixture(t *testing.T) (m Model, alpha, gamma string) {
	t.Helper()
	base := t.TempDir()
	a, here, g := filepath.Join(base, "alpha"), filepath.Join(base, "here"), filepath.Join(base, "gamma")
	for _, d := range []string{a, here, g} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	chdir(t, a)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m = NewWith(registry.Global(), host.MapConfig{"project.auto_save_on_switch": "false"})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	out, _ = m.Update(project.SwitchProjectMsg{Root: here})
	m = out.(Model)
	bg := m.ws.Background()
	if len(bg) != 1 {
		t.Fatalf("background = %v, want the parked alpha", bg)
	}
	alpha, gamma = bg[0], g
	seedHistory(t, cwd(t), alpha, gamma)
	m.recent.Set(nil)

	out, _ = m.Update(ShowRecentFilesMsg{})
	m = out.(Model)
	it, ok := m.recentProjectRow()
	if !ok || it.Title != "alpha" {
		t.Fatalf("focused row = %+v (ok=%v), want the alpha project row", it, ok)
	}
	return m, alpha, gamma
}

// pressRecentRow presses key and applies every message the dispatch produced.
func pressRecentRow(t *testing.T, m Model, key tea.KeyPressMsg) Model {
	t.Helper()
	out, cmd := m.Update(key)
	m = out.(Model)
	for _, msg := range cmdMsgs(cmd) {
		out, _ = m.Update(msg)
		m = out.(Model)
	}
	return m
}

var (
	closeRowKey  = tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModCtrl}
	removeRowKey = tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModCtrl}
)

// recentRowLine returns the rendered dialog line holding name.
func recentRowLine(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	t.Fatalf("no rendered row for %q", name)
	return ""
}

// TestRecentProjectsColumnMarksOpenProjects: the open (parked) project's row
// renders the ● marker, the closed one's does not.
func TestRecentProjectsColumnMarksOpenProjects(t *testing.T) {
	m, _, _ := recentRowsFixture(t)
	if line := recentRowLine(t, m, "alpha"); !strings.Contains(line, "●") {
		t.Errorf("open project row lacks the ● marker: %q", line)
	}
	if line := recentRowLine(t, m, "gamma"); strings.Contains(line, "●") {
		t.Errorf("closed project row carries the ● marker: %q", line)
	}
}

// TestRecentRowCloseClosesWithoutSwitching: the close chord tears the parked
// project down as a timed project.close op, keeps the active project and the
// dialog, and leaves the row listed without its marker.
func TestRecentRowCloseClosesWithoutSwitching(t *testing.T) {
	m, alpha, _ := recentRowsFixture(t)
	active := m.activeWS().Root

	m = pressRecentRow(t, m, closeRowKey)

	if m.ws.Peek(alpha) != nil {
		t.Fatal("the close chord must drop the parked workspace")
	}
	if m.activeWS().Root != active {
		t.Fatal("closing from the list must not switch the active project")
	}
	if !m.palette.IsOpen() {
		t.Fatal("an idle close keeps the dialog open")
	}
	if line := recentRowLine(t, m, "alpha"); strings.Contains(line, "●") {
		t.Errorf("closed row must stay listed, unmarked: %q", line)
	}
	if it, ok := m.recentProjectRow(); !ok || it.Title != "alpha" {
		t.Errorf("the cursor must stay on the closed row, got %+v", it)
	}
	ops := opsOf(usageEvents(t, m), telemetry.OpProjectClose)
	if len(ops) != 2 || ops[0].Data["phase"] != "start" || ops[1].Data["phase"] != "ok" {
		t.Fatalf("want a start/ok project.close pair, got %v", ops)
	}
}

// TestRecentRowCloseGlobalChord: the Global project.close chord closes the
// focused row instead of the active project while the dialog is up.
func TestRecentRowCloseGlobalChord(t *testing.T) {
	m, alpha, _ := recentRowsFixture(t)
	active := m.activeWS().Root
	m = pressRecentRow(t, m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl | tea.ModShift})
	if m.ws.Peek(alpha) != nil || m.activeWS().Root != active {
		t.Fatal("ctrl+shift+w in the dialog must close the focused row, not the active project")
	}
}

// TestRecentRowCloseOnClosedProjectIsNoop: a closed row has nothing to close.
func TestRecentRowCloseOnClosedProjectIsNoop(t *testing.T) {
	m, _, _ := recentRowsFixture(t)
	m = pressRecentRow(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if it, _ := m.recentProjectRow(); it.Title != "gamma" {
		t.Fatalf("focused row = %q, want gamma", it.Title)
	}
	n := len(m.ws.Background())
	m = pressRecentRow(t, m, closeRowKey)
	if len(m.ws.Background()) != n {
		t.Fatal("closing a closed project must not touch the open workspaces")
	}
	if !strings.Contains(recentRowNotice(m), "not open") {
		t.Errorf("want a 'not open' notice, got %q", recentRowNotice(m))
	}
}

// TestRecentRowRemoveRefusedForOpenProject: forgetting an open project is
// refused with a notice; the workspace and its history entry stay.
func TestRecentRowRemoveRefusedForOpenProject(t *testing.T) {
	m, alpha, _ := recentRowsFixture(t)
	out, cmd := m.Update(removeRowKey)
	m = out.(Model)
	for _, msg := range cmdMsgs(cmd) {
		if _, bad := msg.(project.RemoveFromHistoryMsg); bad {
			t.Fatal("an open project must not be removed from the history")
		}
		out, _ = m.Update(msg)
		m = out.(Model)
	}
	if m.ws.Peek(alpha) == nil {
		t.Fatal("the refused remove must leave the workspace open")
	}
	if !strings.Contains(recentRowNotice(m), "is open") {
		t.Errorf("want an 'is open' refusal notice, got %q", recentRowNotice(m))
	}
}

// TestRecentRowRemoveForgetsClosedProject: on a closed row the remove chord
// deletes the history entry.
func TestRecentRowRemoveForgetsClosedProject(t *testing.T) {
	m, _, gamma := recentRowsFixture(t)
	m = pressRecentRow(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	out, cmd := m.Update(removeRowKey)
	m = out.(Model)
	var removed bool
	for _, msg := range cmdMsgs(cmd) {
		out, next := m.Update(msg)
		m = out.(Model)
		for _, inner := range cmdMsgs(next) {
			if r, ok := inner.(project.RemovedFromHistoryMsg); ok && r.Err == nil {
				removed = true
			}
		}
	}
	if !removed {
		t.Fatalf("the remove chord must delete gamma (%s) from the history", gamma)
	}
}

// TestRecentRowChordsFallThroughElsewhere: off a recent project row the
// chords are not the row actions — the palette keeps them (query editing).
func TestRecentRowChordsFallThroughElsewhere(t *testing.T) {
	m, _, _ := recentRowsFixture(t)
	m.palette.Open(m.paletteContext())
	if _, ok := m.recentProjectRow(); ok {
		t.Fatal("the command palette has no recent project rows")
	}
	for _, k := range []tea.KeyPressMsg{closeRowKey, removeRowKey} {
		if _, handled := m.paletteBindingCmd(k); handled {
			t.Errorf("%v must fall through to the palette outside the recent projects column", k)
		}
	}
	// Run from the command palette, the commands only explain themselves.
	m.palette.Close()
	out, _ := m.Update(RecentCloseRowMsg{})
	if got := recentRowNotice(out.(Model)); !strings.Contains(got, "Recent Files") {
		t.Errorf("want the focus-a-row notice, got %q", got)
	}
}

// recentRowNotice is the newest notification text, whether still queued on
// the host or already drained into the model's history.
func recentRowNotice(m Model) string {
	if pending := m.host.DrainNotifications(); len(pending) > 0 {
		return pending[len(pending)-1].Text
	}
	if len(m.history) == 0 {
		return ""
	}
	return m.history[len(m.history)-1].text
}
