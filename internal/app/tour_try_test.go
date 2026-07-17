package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/registry"
)

// tour_try_test.go covers the interactive try-it steps (#680): pass-through
// routing while a page has unfinished tasks, ticking on the command-executed
// signal (#679), suspension/resume around shell takeovers, and that paging /
// skipping keeps working regardless of task state.

// tourModel builds a sized model with the kbtest plugin (ctrl+y →
// kbtest.fire) and the welcome tour open on its first page.
func tourModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	reg := registry.New()
	reg.Add(kbPlugin{})
	m := NewWith(reg, host.MapConfig{"keymap.bindings.ctrl+y": "kbtest.fire"})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	m.openTour()
	if !m.tourOpen() {
		t.Fatal("setup: tour must be open")
	}
	return m
}

func TestTourPassThroughWhileTaskPending(t *testing.T) {
	m := tourModel(t)
	if !m.tour.HasPendingTasks() {
		t.Fatal("the first page must carry a pending try-it task")
	}
	// A non-paging key falls through to normal key handling and drives the
	// bound command — it is not swallowed by the tour or the shell scroller.
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	var fired bool
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(kbFiredMsg); ok {
			fired = true
		}
	}
	if !fired {
		t.Fatal("a try-it page must pass non-paging keys through to the keymap")
	}
	// Paging keys still belong to the tour, task state notwithstanding.
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = out.(Model)
	if m.tour.Page() != 1 {
		t.Fatalf("right must page the tour, page = %d", m.tour.Page())
	}
	// The editor page is passive: the same key is swallowed again.
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatal("a passive page must swallow non-paging keys")
	}
	// Esc skips regardless of unfinished tasks.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.tourOpen() {
		t.Fatal("esc must close the tour with tasks unfinished")
	}
}

func TestTourTicksOnCommandExecuted(t *testing.T) {
	m := tourModel(t)
	out, _ := m.Update(CommandExecutedMsg{ID: "palette.searchEverywhere"})
	m = out.(Model)
	if !m.tour.TaskDone("palette.searchEverywhere") {
		t.Fatal("the executed signal must tick the matching try-it task")
	}
	if m.tour.HasPendingTasks() {
		t.Fatal("the first page's only task is done")
	}
	// Once ticked, the page is modal again: non-paging keys are swallowed.
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatal("a completed page must swallow non-paging keys again")
	}
}

func TestTourEndToEndKeyTick(t *testing.T) {
	// The real chain: a passed-through chord resolves through the keymap,
	// dispatch emits CommandExecutedMsg (#679), and the tour ticks. The
	// default model is used so explorer.toggle's cmd+1 binding is live.
	m := newSized()
	m.openTour()
	m.tour.Next()
	m.tour.Next() // the layout page: explorer.toggle task
	if !m.tour.HasPendingTasks() {
		t.Fatal("the layout page must carry the explorer.toggle task")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: '1', Mod: tea.ModMeta})
	if !m.tour.TaskDone("explorer.toggle") {
		t.Fatal("cmd+1 through the tour must execute explorer.toggle and tick the task")
	}
	if !m.tourOpen() {
		t.Fatal("the tour must still be showing after a pane-level try-it")
	}
}

func TestTourSuspendsAndResumesAroundShellTakeover(t *testing.T) {
	// A try-it key may hand the floating shell to other content (f1 help).
	// The tour is then suspended — keys route to the shell, not the tour —
	// and it resumes on the page it left once the shell is free.
	m := tourModel(t)
	out, _ := m.Update(CommandExecutedMsg{ID: "palette.searchEverywhere"})
	m = out.(Model)
	m.openHelp()
	if m.tourOpen() {
		t.Fatal("the shell shows help — the tour must count as suspended")
	}
	if !m.tourSuspended() {
		t.Fatal("tour must be suspended while help holds the shell")
	}
	// Esc goes to the shell (help) and closes it, not the tour.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.shell.IsOpen() && m.tourOpen() {
		t.Fatal("esc must close the help shell first")
	}
	// The next key resumes the tour and acts on it: right pages forward.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = out.(Model)
	if !m.tourOpen() {
		t.Fatal("the tour must resume once the shell is free")
	}
	if m.tour.Page() != 1 {
		t.Fatalf("the resuming key must act on the tour, page = %d", m.tour.Page())
	}
	if !m.tour.TaskDone("palette.searchEverywhere") {
		t.Fatal("the resumed tour must keep the ticked task")
	}
}
