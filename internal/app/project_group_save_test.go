package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/project"
)

// project_group_save_test.go covers project.group.saveOpen (0510, #2577): the
// composition of the root set (active first, parked in MRU order, a peek left
// out), the name prompt's validation, the replace confirmation and the marker
// the save leaves active.

// driveGroupSave executes the returned command tree and feeds the save's own
// messages — GroupSavedMsg, ActiveGroupMsg — back into Update until nothing
// is pending.
func driveGroupSave(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	pending := runPeekCmds(t, cmd)
	for len(pending) > 0 {
		msg := pending[0]
		pending = pending[1:]
		all = append(all, msg)
		switch msg.(type) {
		case project.GroupSavedMsg, project.ActiveGroupMsg:
			out, c := m.Update(msg)
			m = out.(Model)
			pending = append(pending, runPeekCmds(t, c)...)
		}
	}
	return m, all
}

// typeGroupName feeds text into the open save prompt one key at a time.
func typeGroupName(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	return m
}

// openSaveGroupPrompt dispatches the command and asserts the dialog opened.
func openSaveGroupPrompt(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(project.SaveOpenGroupMsg{})
	m = out.(Model)
	if !m.groupSavePromptOpen() {
		t.Fatal("project.group.saveOpen must open the name prompt")
	}
	return m
}

// storedGroup reads a persisted group back off disk.
func storedGroup(t *testing.T, name string) (project.Group, bool) {
	t.Helper()
	cfg, _ := config.Load(config.Discover("."))
	return project.FindGroup(cfg, name)
}

// forgetGroup drops a group written by a test (storeGroup does this for the
// ones it seeds; a saved one has no such hook).
func forgetGroup(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		opts := config.Discover(".")
		_ = project.ClearActiveGroup(opts)
		_ = project.RemoveGroup(opts, name)
	})
}

// openSetFixture parks two projects and lands on a third, so the open set is
// api (active), ui, www in MRU order.
func openSetFixture(t *testing.T) (Model, []string) {
	t.Helper()
	roots := peekFixture(t, "origin", "api", "ui", "www")
	switchAutoSaveOff(t)
	m := dismissOnboarding(switchModel(t))
	for _, r := range []string{roots[3], roots[2], roots[1]} {
		m, _ = driveGroupMsg(t, m, project.SwitchProjectMsg{Root: r})
	}
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("setup: api must be active, cwd = %s", cwd(t))
	}
	return m, roots
}

// TestSaveOpenGroupComposesRootSetAndActivates is the acceptance shape: the
// prompt lists the active workspace first and the parked ones most-recent
// first, enter writes the group, and the group is active right away.
func TestSaveOpenGroupComposesRootSetAndActivates(t *testing.T) {
	m, roots := openSetFixture(t)
	forgetGroup(t, "web")

	m = openSaveGroupPrompt(t, m)
	got := m.groupSave.roots
	if len(got) != 4 {
		t.Fatalf("the open set is the active workspace plus the three parked ones, got %v", got)
	}
	// api active, then ui, www, origin — the parked ones most recent first.
	for i, want := range []string{roots[1], roots[2], roots[3], roots[0]} {
		if !sameDir(t, got[i], want) {
			t.Errorf("root %d = %s, want %s", i, got[i], want)
		}
	}
	body := shellBody(m)
	for _, r := range got {
		if !strings.Contains(body, project.CompactPath(r)) {
			t.Errorf("the prompt must list %s:\n%s", r, body)
		}
	}

	m = typeGroupName(t, m, "web")
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = driveGroupSave(t, out.(Model), cmd)

	if m.groupSavePromptOpen() {
		t.Error("a successful save closes the prompt")
	}
	g, ok := storedGroup(t, "web")
	if !ok || len(g.Roots) != 4 {
		t.Fatalf("group web on disk = %+v (%v), want four roots", g, ok)
	}
	if !sameDir(t, g.Roots[0], roots[1]) {
		t.Errorf("member 1 = %s, want the active workspace %s", g.Roots[0], roots[1])
	}
	if m.activeGroup != "web" {
		t.Errorf("marker = %q, want web", m.activeGroup)
	}
	if got := activeGroupOnDisk(t); got != "web" {
		t.Errorf("project.active_group on disk = %q, want web", got)
	}
	if !groupNotified(m, "group web saved · 4 projects") {
		t.Errorf("landing toast missing, history = %+v", m.history)
	}
}

// TestSaveOpenGroupExcludesPeek: a peeked workspace (#2136) is not part of the
// set — the peek's origin, which is parked, still is.
func TestSaveOpenGroupExcludesPeek(t *testing.T) {
	roots := peekFixture(t, "origin", "api")
	switchAutoSaveOff(t)
	m := dismissOnboarding(switchModel(t))
	m, _ = driveGroupMsg(t, m, project.PeekProjectMsg{Root: roots[1]})
	if m.peek == nil {
		t.Fatalf("setup: api must be a peek, cwd = %s", cwd(t))
	}

	m = openSaveGroupPrompt(t, m)
	got := m.groupSave.roots
	if len(got) != 1 || !sameDir(t, got[0], roots[0]) {
		t.Fatalf("the peek must be left out and the parked origin kept, got %v", got)
	}
}

// TestSaveOpenGroupWithoutBackgroundSavesOneMember: with nothing parked the
// command still writes a one-member seed.
func TestSaveOpenGroupWithoutBackgroundSavesOneMember(t *testing.T) {
	roots := peekFixture(t, "solo")
	switchAutoSaveOff(t)
	m := dismissOnboarding(switchModel(t))
	forgetGroup(t, "seed")

	m = openSaveGroupPrompt(t, m)
	if len(m.groupSave.roots) != 1 || !sameDir(t, m.groupSave.roots[0], roots[0]) {
		t.Fatalf("the open set is the single active workspace, got %v", m.groupSave.roots)
	}
	m = typeGroupName(t, m, "seed")
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = driveGroupSave(t, out.(Model), cmd)

	g, ok := storedGroup(t, "seed")
	if !ok || len(g.Roots) != 1 {
		t.Fatalf("group seed on disk = %+v (%v), want one root", g, ok)
	}
	if !groupNotified(m, "group seed saved · 1 project") {
		t.Errorf("the toast counts one project, history = %+v", m.history)
	}
}

// TestSaveOpenGroupRejectsInvalidName: an empty name (and a name carrying a
// path separator) keeps the prompt open with the reason attached.
func TestSaveOpenGroupRejectsInvalidName(t *testing.T) {
	m, _ := openSetFixture(t)

	m = openSaveGroupPrompt(t, m)
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if cmd != nil {
		t.Error("an empty name must not start a write")
	}
	if !m.groupSavePromptOpen() {
		t.Fatal("the prompt stays open on a validation failure")
	}
	if !strings.Contains(shellBody(m), "group name is empty") {
		t.Errorf("the reason is shown:\n%s", shellBody(m))
	}

	m = typeGroupName(t, m, "web/api")
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if !strings.Contains(shellBody(m), "path separator") {
		t.Errorf("a separator in the name is rejected:\n%s", shellBody(m))
	}
}

// TestSaveOpenGroupReplaceConfirmation: an existing name asks first — n goes
// back to editing, y replaces the stored roots in place.
func TestSaveOpenGroupReplaceConfirmation(t *testing.T) {
	m, roots := openSetFixture(t)
	storeGroup(t, "web", []string{roots[3]})

	m = openSaveGroupPrompt(t, m)
	m = typeGroupName(t, m, "web")
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if cmd != nil {
		t.Error("the confirmation must not write yet")
	}
	if m.groupSave == nil || m.groupSave.replace != "web" {
		t.Fatalf("enter on an existing name asks first, state = %+v", m.groupSave)
	}
	if !strings.Contains(shellBody(m), `replace group "web"? [y/n]`) {
		t.Errorf("the confirmation names the group:\n%s", shellBody(m))
	}

	// n returns to editing without touching the stored group.
	out, _ = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = out.(Model)
	if m.groupSave == nil || m.groupSave.replace != "" {
		t.Fatalf("n keeps the prompt editable, state = %+v", m.groupSave)
	}
	if g, _ := storedGroup(t, "web"); len(g.Roots) != 1 {
		t.Errorf("n must not replace, stored roots = %v", g.Roots)
	}

	out, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	out, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m, _ = driveGroupSave(t, out.(Model), cmd)

	g, ok := storedGroup(t, "web")
	if !ok || len(g.Roots) != 4 {
		t.Fatalf("y replaces the roots, stored = %+v (%v)", g, ok)
	}
	if m.activeGroup != "web" || activeGroupOnDisk(t) != "web" {
		t.Errorf("the replaced group becomes active: memory = %q disk = %q", m.activeGroup, activeGroupOnDisk(t))
	}
}
