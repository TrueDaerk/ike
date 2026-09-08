package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/menu"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/registry"
)

// project_group_close_test.go covers leaving and moving within a group (0510,
// #2572): project.group.close with the aggregated busy guard and the teardown
// order, project.group.next / .prev cycling, and project.group.warm.

// driveGroupMsg dispatches msg and drives the resulting command tree the way
// driveGroupOpen does, so switches, chains and marker writes settle. The
// settled model has the first-start onboarding dismissed (a rebuilt model
// opens it where a language server is missing; it would eat scripted keys).
func driveGroupMsg(t *testing.T, m Model, msg tea.Msg) (Model, []tea.Msg) {
	t.Helper()
	out, cmd := m.Update(msg)
	m, msgs := driveGroupOpen(t, out.(Model), cmd)
	return dismissOnboarding(m), msgs
}

// switchAutoSaveOff turns the #2186 auto-save gate off in the *persisted*
// user layer: a model rebuilt by a switch reads its config from disk, so the
// switchModel MapConfig alone only covers the first model. The scenarios here
// park dirty buffers across hops, which the gate would write away.
func switchAutoSaveOff(t *testing.T) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", "")
	opts := config.Discover(".")
	if err := config.WriteKey(opts, config.UserScope, "project.auto_save_on_switch", false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.WriteKey(opts, config.UserScope, "project.auto_save_on_switch", true) })
}

// bgRootFor returns the manager's spelling of root in the background set.
func bgRootFor(t *testing.T, m Model, root string) string {
	t.Helper()
	for _, bg := range m.ws.Background() {
		if sameDir(t, bg, root) {
			return bg
		}
	}
	t.Fatalf("%s is not parked, background = %v", filepath.Base(root), m.ws.Background())
	return ""
}

// activeGroupOnDisk reads the persisted project.active_group marker.
func activeGroupOnDisk(t *testing.T) string {
	t.Helper()
	cfg, _ := config.Load(config.Discover("."))
	if cfg == nil {
		return ""
	}
	return cfg.Project.ActiveGroup
}

// TestGroupCloseCleanTearsDownAndLandsOnNonMember is the acceptance shape:
// every member is torn down (sessions persisted first), the IDE lands on the
// MRU non-member, the marker clears in memory and on disk.
func TestGroupCloseCleanTearsDownAndLandsOnNonMember(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")
	if !sameDir(t, cwd(t), roots[1]) || len(m.ws.Background()) != 2 {
		t.Fatalf("setup: active api with origin + ui parked, cwd = %s bg = %v", cwd(t), m.ws.Background())
	}

	m, msgs := driveGroupMsg(t, m, project.CloseGroupMsg{})
	if !sameDir(t, cwd(t), roots[0]) {
		t.Fatalf("the close lands on the MRU non-member, cwd = %s", cwd(t))
	}
	if len(m.ws.Background()) != 0 {
		t.Errorf("every member is torn down, background = %v", m.ws.Background())
	}
	if m.activeGroup != "" {
		t.Errorf("marker in memory = %q, want cleared", m.activeGroup)
	}
	if a := activeGroupMsgs(msgs); len(a) != 1 || a[0].Err != nil || a[0].Name != "" {
		t.Fatalf("the marker clear must land once without error, got %+v", a)
	}
	if got := activeGroupOnDisk(t); got != "" {
		t.Errorf("project.active_group on disk = %q, want cleared", got)
	}
	for _, r := range roots[1:] {
		if _, err := os.Stat(filepath.Join(r, ".ike", "session.json")); err != nil {
			t.Errorf("%s: session persisted before the teardown: %v", filepath.Base(r), err)
		}
	}
	if !groupNotified(m, "closed group web · 2 projects") {
		t.Errorf("landing toast missing, history = %+v", m.history)
	}
	if strings.Contains(groupStatus(m), "⦿") {
		t.Errorf("the segment is gone with the marker, status = %q", groupStatus(m))
	}
}

// TestGroupCloseActiveNonMemberDropsParkedMembers: standing in a non-member
// while the group is active, the close needs no switch — the parked members
// go, the current project stays.
func TestGroupCloseActiveNonMemberDropsParkedMembers(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")
	m, _ = driveGroupMsg(t, m, project.SwitchProjectMsg{Root: roots[0]})
	active := m.activeWS()

	m, _ = driveGroupMsg(t, m, project.CloseGroupMsg{})
	if m.activeWS() != active || !sameDir(t, cwd(t), roots[0]) {
		t.Fatalf("no switch runs from a non-member, cwd = %s", cwd(t))
	}
	if len(m.ws.Background()) != 0 || m.activeGroup != "" {
		t.Errorf("members torn down and marker cleared: bg = %v marker = %q", m.ws.Background(), m.activeGroup)
	}
}

// TestGroupCloseNoNonMemberQuits: with every parked workspace a member the
// close degrades to the quit — clean state, no guard — and the marker clears
// on disk before the process ends.
func TestGroupCloseNoNonMemberQuits(t *testing.T) {
	roots := peekFixture(t, "api", "ui")
	storeGroup(t, "web", roots)
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")
	if len(m.ws.Background()) != 1 {
		t.Fatalf("setup: ui parked, bg = %v", m.ws.Background())
	}

	out, cmd := m.Update(project.CloseGroupMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("closing the group with no non-member must quit")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected the quit command, got nil msg")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %#v", msg)
	}
	if m.activeGroup != "" || activeGroupOnDisk(t) != "" {
		t.Errorf("the marker clears with the quit: memory = %q disk = %q", m.activeGroup, activeGroupOnDisk(t))
	}
}

// TestGroupCloseNoNonMemberDirtyRunsQuitGuard: the degraded close runs the
// #287/#821 quit guard; esc keeps the group (marker included), d quits and
// clears the marker.
func TestGroupCloseNoNonMemberDirtyRunsQuitGuard(t *testing.T) {
	roots := peekFixture(t, "api", "ui")
	storeGroup(t, "web", roots)
	file := filepath.Join(roots[0], "f.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := dismissOnboarding(switchModel(t))
	m, _ = openGroup(t, m, "web")
	m = openDirty(t, dismissOnboarding(m), file)

	out, _ := m.Update(project.CloseGroupMsg{})
	m = out.(Model)
	if !m.closePromptOpen() || m.closePending.groupClose != "web" {
		t.Fatal("the degraded close must open the quit guard for the group")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.closePromptOpen() || m.activeGroup != "web" || activeGroupOnDisk(t) != "web" {
		t.Fatalf("esc keeps the group: prompt = %v marker = %q disk = %q", m.closePromptOpen(), m.activeGroup, activeGroupOnDisk(t))
	}

	out, _ = m.Update(project.CloseGroupMsg{})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("d must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("d must yield tea.QuitMsg")
	}
	if m.activeGroup != "" || activeGroupOnDisk(t) != "" {
		t.Errorf("d clears the marker: memory = %q disk = %q", m.activeGroup, activeGroupOnDisk(t))
	}
	if data, _ := os.ReadFile(file); string(data) != "one\n" {
		t.Errorf("d discards, never writes, file = %q", data)
	}
}

// busyGroupFixture: group web = api, ui; ui parked with a dirty buffer, api
// active with a running shell terminal, origin parked as the non-member.
func busyGroupFixture(t *testing.T) (m Model, roots []string, dirtyPath string, term *pane.Instance) {
	t.Helper()
	roots = peekFixture(t, "origin", "api", "ui")
	switchAutoSaveOff(t)
	storeGroup(t, "web", roots[1:])
	dirtyPath = filepath.Join(roots[2], "f.txt")
	if err := os.WriteFile(dirtyPath, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = dismissOnboarding(switchModel(t))
	m, _ = driveGroupMsg(t, m, project.SwitchProjectMsg{Root: roots[2]})
	m = openDirty(t, m, dirtyPath)
	// The open from ui counts ui as done and switches to api: ui parks
	// dirty (the #2186 gate is off in switchModel).
	m, _ = openGroup(t, m, "web")
	m = dismissOnboarding(m)
	if !sameDir(t, cwd(t), roots[1]) || !parkedRoot(t, m, roots[2]) || !parkedRoot(t, m, roots[0]) {
		t.Fatalf("setup: active api, ui + origin parked; cwd = %s bg = %v", cwd(t), m.ws.Background())
	}
	if got := collectActivity(m.ws.Peek(bgRootFor(t, m, roots[2]))).dirty; len(got) != 1 {
		t.Fatalf("setup: ui must park dirty, got %v", got)
	}
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	term = m.activeWS().Panes.FocusedInstance()
	if term == nil || term.Kind() != pane.KindTerminal || !term.Terminal().Running() {
		t.Fatal("setup: terminal.new must open a running shell in api")
	}
	return m, roots, dirtyPath, term
}

// TestGroupCloseBusyPromptNamesMembersAndCancels: one prompt lists both busy
// members — the dirty parked one and the active one with the shell — and
// esc leaves everything open.
func TestGroupCloseBusyPromptNamesMembersAndCancels(t *testing.T) {
	m, roots, _, term := busyGroupFixture(t)
	t.Cleanup(func() { term.Terminal().Close() })

	out, _ := m.Update(project.CloseGroupMsg{})
	m = out.(Model)
	if !m.groupClosePromptOpen() {
		t.Fatal("a busy group close must prompt")
	}
	p := m.groupClosePending
	if len(p.busy) != 2 {
		t.Fatalf("both busy members listed, got %+v", p.busy)
	}
	if !sameDir(t, p.busy[0].root, roots[1]) || p.busy[0].act.shells != 1 {
		t.Errorf("the active member lists its shell first, got %+v", p.busy[0])
	}
	if !sameDir(t, p.busy[1].root, roots[2]) || strings.Join(p.busy[1].act.dirty, ",") != "f.txt" {
		t.Errorf("the parked member lists its dirty buffer, got %+v", p.busy[1])
	}
	if !p.anyDirty() {
		t.Error("the save option is offered when a member is dirty")
	}
	if !sameDir(t, p.target, roots[0]) {
		t.Errorf("the landing is the non-member origin, got %s", p.target)
	}
	if len(m.ws.Background()) != 2 {
		t.Error("prompting must not touch any workspace")
	}

	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.groupClosePromptOpen() || !sameDir(t, cwd(t), roots[1]) || len(m.ws.Background()) != 2 {
		t.Fatalf("esc leaves everything open: cwd = %s bg = %v", cwd(t), m.ws.Background())
	}
	if m.activeGroup != "web" || !term.Terminal().Running() {
		t.Error("esc keeps the marker and the shell")
	}
}

// TestGroupCloseBusyDiscardCloses: d closes every member — the shell stops,
// the dirty buffer is dropped unwritten — and lands on the non-member.
func TestGroupCloseBusyDiscardCloses(t *testing.T) {
	m, roots, dirtyPath, _ := busyGroupFixture(t)

	out, _ := m.Update(project.CloseGroupMsg{})
	m = out.(Model)
	m, _ = driveGroupMsg(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if !sameDir(t, cwd(t), roots[0]) || len(m.ws.Background()) != 0 {
		t.Fatalf("d closes every member and lands on origin: cwd = %s bg = %v", cwd(t), m.ws.Background())
	}
	if data, _ := os.ReadFile(dirtyPath); string(data) != "one\n" {
		t.Errorf("d discards, never writes, file = %q", data)
	}
	if m.activeGroup != "" {
		t.Errorf("marker = %q, want cleared", m.activeGroup)
	}
}

// TestGroupCloseBusySaveThenCloses: s writes the parked member's dirty buffer
// (enter stands in for it, the primary) and then closes the group.
func TestGroupCloseBusySaveThenCloses(t *testing.T) {
	m, roots, dirtyPath, _ := busyGroupFixture(t)

	out, _ := m.Update(project.CloseGroupMsg{})
	m = out.(Model)
	m, _ = driveGroupMsg(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !sameDir(t, cwd(t), roots[0]) || len(m.ws.Background()) != 0 {
		t.Fatalf("s closes every member after saving: cwd = %s bg = %v", cwd(t), m.ws.Background())
	}
	data, err := os.ReadFile(dirtyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Errorf("s writes the parked member's buffer, got %q", data)
	}
	if m.activeGroup != "" || activeGroupOnDisk(t) != "" {
		t.Errorf("marker cleared: memory = %q disk = %q", m.activeGroup, activeGroupOnDisk(t))
	}
}

// TestGroupCloseSaveFailureKeepsGroupOpen: a member whose dirty buffer cannot
// be written stays open and nothing else closes.
func TestGroupCloseSaveFailureKeepsGroupOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	roots := peekFixture(t, "origin", "api", "ui")
	switchAutoSaveOff(t)
	storeGroup(t, "web", roots[1:])
	dirtyPath := filepath.Join(roots[2], "f.txt")
	if err := os.WriteFile(dirtyPath, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := dismissOnboarding(switchModel(t))
	m, _ = driveGroupMsg(t, m, project.SwitchProjectMsg{Root: roots[2]})
	m = openDirty(t, m, dirtyPath)
	m, _ = openGroup(t, m, "web")
	m = dismissOnboarding(m)
	// Make the write fail: the directory refuses new files (the raw write
	// goes through a temp file), and the file itself is read-only.
	if err := os.Chmod(dirtyPath, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(roots[2], 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roots[2], 0o755); _ = os.Chmod(dirtyPath, 0o644) })

	out, _ := m.Update(project.CloseGroupMsg{})
	m = out.(Model)
	if !m.groupClosePromptOpen() {
		t.Fatal("the dirty parked member must prompt")
	}
	m, _ = driveGroupMsg(t, m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if !sameDir(t, cwd(t), roots[1]) || len(m.ws.Background()) != 2 {
		t.Fatalf("a failed save closes nothing: cwd = %s bg = %v", cwd(t), m.ws.Background())
	}
	if m.activeGroup != "web" {
		t.Errorf("marker stays, got %q", m.activeGroup)
	}
	if !groupNotified(m, "group not closed: save failed in ui") {
		t.Errorf("the failure names the member, history = %+v", m.history)
	}
}

// TestGroupCycleNextPrevWrapSkipMissingColdVisit: next/prev walk the list
// order with wrap, a member missing on disk is skipped, and an unparked
// member is a cold first visit.
func TestGroupCycleNextPrevWrapSkipMissingColdVisit(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui", "gone", "www")
	storeGroup(t, "web", roots[1:])
	if err := os.RemoveAll(roots[3]); err != nil {
		t.Fatal(err)
	}
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("setup: landing api, cwd = %s", cwd(t))
	}
	step := func(delta int, want string) {
		t.Helper()
		m, _ = driveGroupMsg(t, m, project.CycleGroupMsg{Delta: delta})
		if !sameDir(t, cwd(t), want) {
			t.Fatalf("delta %+d: cwd = %s, want %s", delta, cwd(t), filepath.Base(want))
		}
		if m.activeGroup != "web" {
			t.Fatalf("the marker rides every step, got %q", m.activeGroup)
		}
	}
	step(1, roots[2])  // api -> ui
	step(1, roots[4])  // ui -> www (gone skipped)
	step(1, roots[1])  // www -> api (wrap)
	step(-1, roots[4]) // api -> www (wrap back, gone skipped)
	step(-1, roots[2]) // www -> ui
	step(-1, roots[1]) // ui -> api

	// Drop www from the background; the next visit is a cold one.
	out, _ := m.Update(project.CloseWorkspaceMsg{Path: bgRootFor(t, m, roots[4])})
	m = out.(Model)
	if parkedRoot(t, m, roots[4]) {
		t.Fatal("setup: www must be closed")
	}
	step(1, roots[2])
	var msgs []tea.Msg
	m, msgs = driveGroupMsg(t, m, project.CycleGroupMsg{Delta: 1})
	if !sameDir(t, cwd(t), roots[4]) {
		t.Fatalf("the cold visit lands on www, cwd = %s", cwd(t))
	}
	if got := recordedRoots(msgs); len(got) != 1 || !sameDir(t, got[0], roots[4]) {
		t.Errorf("a cycle step is a normal recorded switch, got %v", got)
	}
	if !parkedRoot(t, m, roots[2]) {
		t.Error("the member left behind parks like any switch")
	}
}

// TestGroupCycleFromNonMemberAndSingle: from a non-member next goes to member
// 1 and prev to the last member; a single-member group only notifies.
func TestGroupCycleFromNonMemberAndSingle(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")
	m, _ = driveGroupMsg(t, m, project.SwitchProjectMsg{Root: roots[0]})

	m, _ = driveGroupMsg(t, m, project.CycleGroupMsg{Delta: -1})
	if !sameDir(t, cwd(t), roots[2]) {
		t.Fatalf("prev from a non-member lands on the last member, cwd = %s", cwd(t))
	}
	m, _ = driveGroupMsg(t, m, project.SwitchProjectMsg{Root: roots[0]})
	m, _ = driveGroupMsg(t, m, project.CycleGroupMsg{Delta: 1})
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("next from a non-member lands on member 1, cwd = %s", cwd(t))
	}

	storeGroup(t, "solo", roots[1:2])
	m, _ = openGroup(t, m, "solo")
	active := m.activeWS()
	m, _ = driveGroupMsg(t, m, project.CycleGroupMsg{Delta: 1})
	if m.activeWS() != active || !groupNotified(m, "group solo has no other project") {
		t.Errorf("a single-member group only notifies, history = %+v", m.history)
	}
}

// TestGroupCycleAndWarmWithoutGroupNotify: without an active group the
// commands are no-ops with a notification.
func TestGroupCycleAndWarmWithoutGroupNotify(t *testing.T) {
	roots := peekFixture(t, "origin")
	m := switchModel(t)
	active := m.activeWS()
	for _, msg := range []tea.Msg{project.CycleGroupMsg{Delta: 1}, project.CycleGroupMsg{Delta: -1}, project.WarmGroupMsg{}, project.CloseGroupMsg{}} {
		var msgs []tea.Msg
		m, msgs = driveGroupMsg(t, m, msg)
		if m.activeWS() != active || !sameDir(t, cwd(t), roots[0]) || len(recordedRoots(msgs)) != 0 || m.groupOpening != nil {
			t.Errorf("%T without a group must not switch anything", msg)
		}
	}
	if !notified(m, "no project group open") {
		t.Errorf("no-op with a notification, history = %+v", m.history)
	}
}

// TestGroupWarmReparksMissingAndReturns: warm re-parks only the members that
// dropped out of the background set and returns to the current member; a
// warm group is a no-op with a notification.
func TestGroupWarmReparksMissingAndReturns(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui", "www")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")
	for _, r := range roots[2:] {
		out, _ := m.Update(project.CloseWorkspaceMsg{Path: bgRootFor(t, m, r)})
		m = out.(Model)
	}
	if parkedRoot(t, m, roots[2]) || parkedRoot(t, m, roots[3]) {
		t.Fatal("setup: ui and www must be closed")
	}

	m, msgs := driveGroupMsg(t, m, project.WarmGroupMsg{})
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("warm returns to the current member, cwd = %s", cwd(t))
	}
	if m.groupOpening != nil {
		t.Fatal("the chain must be finished")
	}
	for _, r := range roots[2:] {
		if !parkedRoot(t, m, r) {
			t.Errorf("%s must be re-parked, background = %v", filepath.Base(r), m.ws.Background())
		}
	}
	if got := recordedRoots(msgs); len(got) != 3 ||
		!sameDir(t, got[0], roots[3]) || !sameDir(t, got[1], roots[2]) || !sameDir(t, got[2], roots[1]) {
		t.Errorf("hops record www, ui, then the return to api, got %v", got)
	}
	if !groupNotified(m, "group web warm · re-parked 2 projects") {
		t.Errorf("warm toast missing, history = %+v", m.history)
	}
	if len(activeGroupMsgs(msgs)) != 0 || m.activeGroup != "web" {
		t.Errorf("warm never rewrites the marker, msgs = %+v marker = %q", activeGroupMsgs(msgs), m.activeGroup)
	}

	m, msgs = driveGroupMsg(t, m, project.WarmGroupMsg{})
	if len(recordedRoots(msgs)) != 0 || !groupNotified(m, "group web is warm") {
		t.Errorf("a warm group runs nothing, records = %v history = %+v", recordedRoots(msgs), m.history)
	}
}

// TestGroupWarmSegment: the status slot reads "warming" while a warm chain
// runs.
func TestGroupWarmSegment(t *testing.T) {
	m := newSized()
	m.groupOpening = &groupOpen{name: "web", total: 2, warm: true}
	if got := m.groupSegment(); got != "warming web 1/2" {
		t.Errorf("warm chain progress = %q", got)
	}
}

// TestGroupCloseCommandsRegistered: the three commands and warm exist with
// their titles, chords, the terminal allowlist keeps close/next/prev
// reachable and the File menu lists the close.
func TestGroupCloseCommandsRegistered(t *testing.T) {
	reg := registry.New()
	reg.Add(appCommands{})
	for id, title := range map[string]string{
		"project.group.close": "Close Project Group",
		"project.group.next":  "Next Project in Group",
		"project.group.prev":  "Previous Project in Group",
		"project.group.warm":  "Warm Project Group",
	} {
		c, ok := reg.Command(id)
		if !ok || c.Title != title {
			t.Errorf("%s must be registered as %q, got %+v (%v)", id, title, c, ok)
		}
	}
	for _, id := range []string{"project.group.close", "project.group.next", "project.group.prev"} {
		if !terminalGlobalCommands[id] {
			t.Errorf("%s must be on the terminal allowlist (#805)", id)
		}
	}
	chords := map[string]map[string]bool{}
	for _, b := range keymap.Defaults(keymap.PresetJetBrains) {
		if chords[b.Command] == nil {
			chords[b.Command] = map[string]bool{}
		}
		chords[b.Command][b.Chord.String()] = true
	}
	for id, want := range map[string][]string{
		"project.group.close": {"cmd+alt+shift+w", "ctrl+alt+shift+w"},
		"project.group.next":  {"cmd+alt+right-bracket", "ctrl+alt+right-bracket"},
		"project.group.prev":  {"cmd+alt+left-bracket", "ctrl+alt+left-bracket"},
	} {
		for _, c := range want {
			if !chords[id][c] {
				t.Errorf("%s lacks the default chord %s, has %v", id, c, chords[id])
			}
		}
	}
	if len(chords["project.group.warm"]) != 0 {
		t.Errorf("project.group.warm is palette-only, has %v", chords["project.group.warm"])
	}
	found := false
	for _, mn := range menu.Defaults() {
		if mn.Title != "File" {
			continue
		}
		for _, it := range mn.Items {
			if it.Command == "project.group.close" && it.Title == "Close Project Group" {
				found = true
			}
		}
	}
	if !found {
		t.Error("the File menu lists Close Project Group")
	}
}
