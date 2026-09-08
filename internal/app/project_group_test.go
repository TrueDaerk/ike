package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/project"
	"ike/internal/registry"
)

// project_group_test.go covers project.group.open (0510, #2571): the warm-up
// switch chain, the active-group marker and its carry-over, the status
// segment, and the cap protection while the chain runs.

// storeGroup persists a group at user scope for the test's lifetime and
// clears the marker afterwards, so the shared temp HOME carries nothing over.
func storeGroup(t *testing.T, name string, roots []string) {
	t.Helper()
	// The switch models run without the IKE_CONFIG_DIR redirect (their user
	// layer is the temp HOME's ~/.ike); the group must land in that file.
	t.Setenv("IKE_CONFIG_DIR", "")
	opts := config.Discover(".")
	if err := project.UpsertGroup(opts, project.Group{Name: name, Roots: roots}); err != nil {
		t.Fatalf("store group: %v", err)
	}
	// The live config is what the open reads (production keeps it current
	// through the reload chain); the test model does not load it itself.
	prev := config.Get()
	cfg, _ := config.Load(opts)
	config.Set(cfg)
	t.Cleanup(func() {
		_ = project.ClearActiveGroup(opts)
		_ = project.RemoveGroup(opts, name)
		config.Set(prev)
	})
}

// driveGroupOpen executes the returned command tree and feeds the chain's
// own messages — SwitchedMsg, SwitchFailedMsg, ActiveGroupMsg — back into
// Update until nothing is pending, returning the settled model and every
// message seen (RecordedMsg included, for the history assertions).
func driveGroupOpen(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	pending := runPeekCmds(t, cmd)
	for len(pending) > 0 {
		msg := pending[0]
		pending = pending[1:]
		all = append(all, msg)
		switch msg.(type) {
		case project.SwitchedMsg, project.SwitchFailedMsg, project.ActiveGroupMsg:
			out, c := m.Update(msg)
			m = out.(Model)
			pending = append(pending, runPeekCmds(t, c)...)
		}
	}
	return m, all
}

// openGroup dispatches OpenGroupMsg and drives the chain to its landing.
func openGroup(t *testing.T, m Model, name string) (Model, []tea.Msg) {
	t.Helper()
	out, cmd := m.Update(project.OpenGroupMsg{Name: name})
	return driveGroupOpen(t, out.(Model), cmd)
}

// parkedRoot reports whether root is in the background set, symlinks
// tolerated (the manager keys by os.Getwd's spelling).
func parkedRoot(t *testing.T, m Model, root string) bool {
	t.Helper()
	for _, bg := range m.ws.Background() {
		if sameDir(t, bg, root) {
			return true
		}
	}
	return false
}

// groupNotified reports whether a notification containing text was raised
// at any point of the chain: live toasts, the host queue, or the history
// ring the departing models drained into (it rides every rebuild).
func groupNotified(m Model, text string) bool {
	if notified(m, text) {
		return true
	}
	for _, h := range m.history {
		if strings.Contains(h.text, text) {
			return true
		}
	}
	return false
}

// groupStatus renders the status line with the editor focused, where the
// segment model applies.
func groupStatus(m Model) string {
	m.setFocus(m.activeEditorKey())
	return m.statusLine()
}

func activeGroupMsgs(msgs []tea.Msg) []project.ActiveGroupMsg {
	var out []project.ActiveGroupMsg
	for _, msg := range msgs {
		if a, ok := msg.(project.ActiveGroupMsg); ok {
			out = append(out, a)
		}
	}
	return out
}

// TestGroupOpenWarmsAllMembersAndLandsOnFirst is the acceptance shape: a
// three-member group ends with member 1 active and 2, 3 parked, three
// history records (one per hop), no per-hop toast, the landing toast, and
// the marker written and read back.
func TestGroupOpenWarmsAllMembersAndLandsOnFirst(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui", "www")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)

	m, msgs := openGroup(t, m, "web")
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("landing must be member 1, cwd = %s", cwd(t))
	}
	if m.groupOpening != nil {
		t.Fatal("the chain must be finished")
	}
	for _, r := range roots[2:] {
		if !parkedRoot(t, m, r) {
			t.Errorf("%s must be parked after the open, background = %v", filepath.Base(r), m.ws.Background())
		}
	}
	if m.activeGroup != "web" {
		t.Errorf("marker = %q, want web", m.activeGroup)
	}
	if got := recordedRoots(msgs); len(got) != 3 ||
		!sameDir(t, got[0], roots[3]) || !sameDir(t, got[1], roots[2]) || !sameDir(t, got[2], roots[1]) {
		t.Errorf("hops record www, ui, api in that order, got %v", got)
	}
	if !groupNotified(m, "group web open · 3 projects") {
		t.Errorf("landing toast missing, history = %+v", m.history)
	}
	if groupNotified(m, "switched to") {
		t.Error("the hops must not toast individually")
	}
	if a := activeGroupMsgs(msgs); len(a) != 1 || a[0].Err != nil || a[0].Name != "web" {
		t.Fatalf("the marker write must land once without error, got %+v", a)
	}
	cfg, _ := config.Load(config.Discover("."))
	if cfg.Project.ActiveGroup != "web" {
		t.Errorf("project.active_group on disk = %q, want web", cfg.Project.ActiveGroup)
	}
	if !strings.Contains(groupStatus(m), "⦿ web/api") {
		t.Errorf("status segment should read the group and root, got %q", groupStatus(m))
	}
}

// TestGroupOpenSkipsMissingMember: a root that is gone is reported once and
// the group opens with what exists.
func TestGroupOpenSkipsMissingMember(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	gone := filepath.Join(filepath.Dir(roots[1]), "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	storeGroup(t, "web", []string{roots[1], gone, roots[2]})
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	m := switchModel(t)

	m, msgs := openGroup(t, m, "web")
	if !groupNotified(m, `group "web": 1 of 3 roots missing`) {
		t.Errorf("the missing root is reported once, history = %+v", m.history)
	}
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("landing must be member 1, cwd = %s", cwd(t))
	}
	if !parkedRoot(t, m, roots[2]) {
		t.Error("the present member ui must be parked")
	}
	if got := recordedRoots(msgs); len(got) != 2 {
		t.Errorf("two hops record two opens, got %v", got)
	}
	if !groupNotified(m, "group web open · 2 projects") {
		t.Errorf("the landing counts the members that opened, history = %+v", m.history)
	}
	if m.activeGroup != "web" {
		t.Errorf("marker = %q, want web", m.activeGroup)
	}
}

// TestGroupOpenContinuesAfterChdirFailure: a member that validates (readable
// directory) but cannot be entered fails its hop; the chain skips it with
// one notification and still lands on member 1.
func TestGroupOpenContinuesAfterChdirFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can enter any directory")
	}
	roots := peekFixture(t, "origin", "api", "locked", "www")
	if err := os.Chmod(roots[2], 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roots[2], 0o755) })
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)

	m, msgs := openGroup(t, m, "web")
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("landing must be member 1, cwd = %s", cwd(t))
	}
	if !parkedRoot(t, m, roots[3]) {
		t.Error("www must be parked")
	}
	if parkedRoot(t, m, roots[2]) {
		t.Error("the failed member must not be parked")
	}
	if !groupNotified(m, `group "web": skipped locked`) {
		t.Errorf("the failed hop is reported once, history = %+v", m.history)
	}
	if groupNotified(m, "cannot switch project") {
		t.Error("the plain switch-failed toast must not fire for a chain hop")
	}
	if got := recordedRoots(msgs); len(got) != 2 {
		t.Errorf("only the successful hops record, got %v", got)
	}
	if !groupNotified(m, "group web open · 2 projects") || m.activeGroup != "web" {
		t.Errorf("the chain still lands: marker = %q, history = %+v", m.activeGroup, m.history)
	}
}

// TestGroupOpenReopenRewarmsAndLandsOnFirst: opening the group one is already
// in re-warms the members that are no longer parked and lands on member 1.
func TestGroupOpenReopenRewarmsAndLandsOnFirst(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")

	// Drop ui from the background (close-from-list), then re-open from api.
	out, _ := m.Update(project.CloseWorkspaceMsg{Path: m.ws.Background()[len(m.ws.Background())-1]})
	m = out.(Model)
	if parkedRoot(t, m, roots[2]) {
		t.Fatal("setup: ui must be closed")
	}
	m, msgs := openGroup(t, m, "web")
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("landing must be member 1, cwd = %s", cwd(t))
	}
	if !parkedRoot(t, m, roots[2]) {
		t.Error("ui must be re-warmed")
	}
	if got := recordedRoots(msgs); len(got) != 2 {
		t.Errorf("both hops run again (ui cold, api resumed), got %v", got)
	}
	if m.activeGroup != "web" {
		t.Errorf("marker = %q, want web", m.activeGroup)
	}
}

// TestGroupOpenSingleMemberAlreadyActive: a group whose only member is the
// current root is a friendly no-op for the landing — no switch, marker set.
func TestGroupOpenSingleMemberAlreadyActive(t *testing.T) {
	roots := peekFixture(t, "api")
	storeGroup(t, "solo", roots)
	m := switchModel(t)
	active := m.activeWS()

	m, msgs := openGroup(t, m, "solo")
	if m.activeWS() != active {
		t.Fatal("no switch must run for the root already active")
	}
	if got := recordedRoots(msgs); len(got) != 0 {
		t.Errorf("nothing to record, got %v", got)
	}
	if m.activeGroup != "solo" || !groupNotified(m, "group solo open · 1 project") {
		t.Errorf("marker = %q, history = %+v", m.activeGroup, m.history)
	}
}

// TestGroupOpenFromPeekEscalates: the first hop is a normal switch away from
// the peek, so the peeked root enters the history and the marker clears.
func TestGroupOpenFromPeekEscalates(t *testing.T) {
	roots := peekFixture(t, "origin", "peeked", "api", "ui")
	storeGroup(t, "web", roots[2:])
	m := switchModel(t)
	m, _ = enterPeek(t, m, roots[1])
	if m.peek == nil {
		t.Fatal("setup: must be peeking")
	}

	m, _ = openGroup(t, m, "web")
	if m.peek != nil {
		t.Error("the open must escalate the peek")
	}
	found := false
	for _, p := range historyPaths() {
		if sameDir(t, p, roots[1]) {
			found = true
		}
	}
	if !found {
		t.Errorf("the escalated peek root must be recorded, history = %v", historyPaths())
	}
	if !sameDir(t, cwd(t), roots[2]) || m.activeGroup != "web" {
		t.Errorf("landing = %s, marker = %q", cwd(t), m.activeGroup)
	}
}

// TestGroupOpenReplacesActiveGroup: opening another group replaces the
// marker; the previous group's workspaces stay parked.
func TestGroupOpenReplacesActiveGroup(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui", "cli")
	storeGroup(t, "web", roots[1:3])
	storeGroup(t, "tools", roots[3:])
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")
	m, _ = openGroup(t, m, "tools")
	if m.activeGroup != "tools" {
		t.Errorf("marker = %q, want tools", m.activeGroup)
	}
	if !sameDir(t, cwd(t), roots[3]) {
		t.Errorf("landing = %s, want cli", cwd(t))
	}
	if !parkedRoot(t, m, roots[1]) || !parkedRoot(t, m, roots[2]) {
		t.Errorf("the previous group's members stay parked, background = %v", m.ws.Background())
	}
}

// TestGroupMarkerCarriesAcrossSwitch: a plain switch after the open keeps the
// marker (only group.close clears it) and the segment names the new root.
func TestGroupMarkerCarriesAcrossSwitch(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")

	out, _ := m.Update(project.SwitchProjectMsg{Root: roots[0]})
	m = out.(Model)
	if m.activeGroup != "web" {
		t.Errorf("marker must ride the rebuild, got %q", m.activeGroup)
	}
	if !strings.Contains(groupStatus(m), "⦿ web/origin") {
		t.Errorf("segment names the current root, got %q", groupStatus(m))
	}
}

// TestGroupMarkerSeedsFromConfigAtStart: a fresh model reads the persisted
// marker (main.go reconciles it against the root first).
func TestGroupMarkerSeedsFromConfigAtStart(t *testing.T) {
	roots := peekFixture(t, "api", "ui")
	storeGroup(t, "web", roots)
	if err := project.SetActiveGroup(config.Discover("."), "web"); err != nil {
		t.Fatal(err)
	}
	prev := config.Get()
	t.Cleanup(func() { config.Set(prev) })
	cfg, _ := config.Load(config.Discover("."))
	config.Set(cfg)

	m := switchModel(t)
	if m.activeGroup != "web" {
		t.Errorf("marker seeded = %q, want web", m.activeGroup)
	}
	if !strings.Contains(groupStatus(m), "⦿ web/api") {
		t.Errorf("segment after restart, got %q", groupStatus(m))
	}
	if m.groupOpening != nil || !strings.Contains(groupStatus(m), "⦿") {
		t.Error("no chain runs at startup")
	}
}

// TestGroupOpenDoesNotEvictMembers: with the cap below the member count the
// hops that park the members must not evict them — the chain protects the
// group before the marker is persisted; a parked non-member goes first.
func TestGroupOpenDoesNotEvictMembers(t *testing.T) {
	roots := peekFixture(t, "origin", "other", "api", "ui", "www")
	storeGroup(t, "web", roots[2:])
	opts := config.Discover(".")
	if err := config.WriteKey(opts, config.UserScope, "project.max_workspaces", 1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.WriteKey(opts, config.UserScope, "project.max_workspaces", defaultMaxWorkspaces) })
	m := switchModel(t)
	// Two non-members in play: origin parks when we switch to other.
	out, _ := m.Update(project.SwitchProjectMsg{Root: roots[1]})
	m = out.(Model)

	m, _ = openGroup(t, m, "web")
	if !sameDir(t, cwd(t), roots[2]) {
		t.Fatalf("landing = %s, want api", cwd(t))
	}
	for _, r := range roots[3:] {
		if !parkedRoot(t, m, r) {
			t.Errorf("member %s must survive the cap, background = %v", filepath.Base(r), m.ws.Background())
		}
	}
	if parkedRoot(t, m, roots[0]) {
		t.Errorf("the LRU non-member goes first, background = %v", m.ws.Background())
	}
	if len(m.ws.Background()) != 3 {
		t.Errorf("cap = len(members) = 3, background = %v", m.ws.Background())
	}
}

// TestGroupOpenUnknownAndEmpty: an unknown name and a group with no member on
// disk only notify.
func TestGroupOpenUnknownAndEmpty(t *testing.T) {
	roots := peekFixture(t, "origin", "api")
	storeGroup(t, "web", roots[1:])
	if err := os.RemoveAll(roots[1]); err != nil {
		t.Fatal(err)
	}
	m := switchModel(t)
	active := m.activeWS()

	m, _ = openGroup(t, m, "nope")
	if !groupNotified(m, `group "nope" not found`) {
		t.Errorf("unknown group notifies, history = %+v", m.history)
	}
	m, _ = openGroup(t, m, "web")
	if !groupNotified(m, `group "web": no member exists on disk`) {
		t.Errorf("empty group notifies, history = %+v", m.history)
	}
	if m.activeWS() != active || m.activeGroup != "" || m.groupOpening != nil {
		t.Error("nothing must change")
	}
}

// TestGroupPickerOpensLocked verifies the project.group.open wiring: the
// dispatched OpenGroupPickerMsg opens the palette locked to the group mode.
func TestGroupPickerOpensLocked(t *testing.T) {
	m := sized(t, 100, 40)
	out, _ := m.Update(project.OpenGroupPickerMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("OpenGroupPickerMsg should open the palette")
	}
}

// TestGroupOpenCommandRegistered: the command exists with its title, the
// File menu lists it and the terminal allowlist keeps its chord reachable.
func TestGroupOpenCommandRegistered(t *testing.T) {
	reg := registry.New()
	reg.Add(appCommands{})
	c, ok := reg.Command("project.group.open")
	if !ok || c.Title != "Open Project Group…" {
		t.Fatalf("project.group.open must be registered with its title, got %+v (%v)", c, ok)
	}
	if !terminalGlobalCommands["project.group.open"] {
		t.Error("project.group.open must be on the terminal allowlist (#805)")
	}
	if statusSegmentCommands["group"] != "project.group.next" {
		t.Error("the group segment click maps to project.group.next")
	}
}

// TestGroupStatusSegment covers the slot's three states and its drop rank.
func TestGroupStatusSegment(t *testing.T) {
	m := newSized()
	if got := m.groupSegment(); got != "" {
		t.Errorf("hidden without a group, got %q", got)
	}
	m.groupOpening = &groupOpen{name: "web", total: 3, done: 1}
	if got := m.groupSegment(); got != "opening web 2/3" {
		t.Errorf("chain progress = %q", got)
	}
	m.groupOpening.done = 3
	if got := m.groupSegment(); got != "opening web 3/3" {
		t.Errorf("progress never exceeds the total, got %q", got)
	}
	m.groupOpening = nil
	m.activeGroup = "web"
	if got := m.groupSegment(); !strings.HasPrefix(got, "⦿ web/") {
		t.Errorf("active marker = %q", got)
	}
	// Drop rank: the slot goes with the hint, before eol.
	left := segList(
		"mode", "NORMAL",
		"file", "a/deep/path/to/the/current/buffer/file_with_long_name.go",
		"group", "⦿ web/api", "eol", "LF", "diagnostics", "3E 1W",
	)
	right := segList("cursor", "Ln 9, Col 3")
	line := composeStatus(left, right, 48)
	if strings.Contains(line, "⦿ web/api") {
		t.Errorf("the group slot drops under width pressure: %q", line)
	}
	if !strings.Contains(line, "3E 1W") {
		t.Errorf("diagnostics outrank the group slot: %q", line)
	}
	// composeStatus drops in place, so the wide check builds its own lists.
	left = segList("mode", "NORMAL", "file", "main.go", "group", "⦿ web/api", "eol", "LF")
	right = segList("cursor", "Ln 9, Col 3")
	if !strings.Contains(composeStatus(left, right, 200), "⦿ web/api") {
		t.Error("with room the slot renders")
	}
}
