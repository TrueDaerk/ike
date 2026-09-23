package app

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/netlink"
	"ike/internal/project"
)

// netlink_close_test.go covers the app side of the network close command
// (#2703): the guard's reasons on the wire, the discard path, the notices,
// and the last-project restart that never quits.

// netClose drives one close request through the Update loop and returns
// the settled model with the IDE's verdict.
func netClose(t *testing.T, m Model, req netlink.CloseRequest) (Model, netlink.CloseResult) {
	t.Helper()
	if req.Client.Name == "" {
		req.Client = netlink.Client{Name: "phone", Addr: "10.0.0.2:5000"}
	}
	var got *netlink.CloseResult
	out, _ := m.Update(netCloseMsg{req: req, reply: func(r netlink.CloseResult) { got = &r }})
	if got == nil {
		t.Fatal("the close must answer exactly once")
	}
	return out.(Model), *got
}

// twoCleanProjects builds a (parked) and b (active), both clean.
func twoCleanProjects(t *testing.T) (m Model, a, b string) {
	t.Helper()
	base := t.TempDir()
	a, b = filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(a)
	m = switchModel(t)
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if len(m.ws.Background()) != 1 {
		t.Fatalf("background = %v, want the parked a", m.ws.Background())
	}
	return m, a, b
}

// netDirtyFixture is closeCurrentFixture with the first-start onboarding
// dialog dismissed at every step (it swallows scripted keys when the package
// runs filtered on a machine missing a language server) and the auto-save
// gate off in the persisted layer, so the dirty buffer survives the round
// trip through b: dirty a active, clean b parked.
func netDirtyFixture(t *testing.T) (m Model, aRoot, bRoot, dirtyPath string) {
	t.Helper()
	switchAutoSaveOff(t)
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dirtyPath = filepath.Join(a, "f.txt")
	if err := os.WriteFile(dirtyPath, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(a)
	m = dismissOnboarding(switchModel(t))
	m = openDirty(t, m, dirtyPath)
	if ed := m.activeEditor(); ed == nil || !ed.Dirty() {
		t.Fatal("fixture: the edit must dirty the buffer")
	}
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = dismissOnboarding(out.(Model))
	out, _ = m.Update(project.SwitchProjectMsg{Root: m.ws.Background()[0]})
	m = dismissOnboarding(out.(Model)) // back in a (dirty, active); b parked
	if len(m.ws.Background()) != 1 {
		t.Fatalf("background = %v, want the parked b", m.ws.Background())
	}
	if dirty := collectActivity(m.activeWS()).dirty; len(dirty) != 1 || dirty[0] != "f.txt" {
		t.Fatalf("fixture: a must come back dirty, got %v", dirty)
	}
	return m, a, m.ws.Background()[0], dirtyPath
}

// TestNetCloseIdleResumesBackground: an idle active project closes exactly
// like project.close — the MRU parked workspace resumes — and the notice
// names the client.
func TestNetCloseIdleResumesBackground(t *testing.T) {
	m, a, b := twoCleanProjects(t)
	m, r := netClose(t, m, netlink.CloseRequest{})
	if r.Outcome != netlink.CloseClosed || r.Project != "b" || !sameDir(t, r.Root, b) {
		t.Fatalf("close idle %+v", r)
	}
	if !sameDir(t, cwd(t), a) || !sameDir(t, m.activeWS().Root, a) {
		t.Fatalf("close must resume the parked project, active = %s", m.activeWS().Root)
	}
	if len(m.ws.Background()) != 0 {
		t.Fatalf("closed workspace must not stay parked, background = %v", m.ws.Background())
	}
	if !notified(m, "closed b via network (phone)") {
		t.Fatal("a remote close must raise its notice")
	}
}

// TestNetCloseByNameAndUnknown: the project name resolves case-insensitively
// among the open workspaces; a name that is not open is unknown, and so is
// a project from the history that is not open.
func TestNetCloseByNameAndUnknown(t *testing.T) {
	m, a, _ := twoCleanProjects(t)
	_, r := netClose(t, m, netlink.CloseRequest{Project: "nope"})
	if r.Outcome != netlink.CloseUnknown {
		t.Fatalf("unknown project %+v", r)
	}
	m, r = netClose(t, m, netlink.CloseRequest{Project: "A"})
	if r.Outcome != netlink.CloseClosed || r.Project != "a" {
		t.Fatalf("close the parked project by name %+v", r)
	}
	if len(m.ws.Background()) != 0 || m.ws.Peek(a) != nil {
		t.Fatalf("the parked workspace must be gone, background = %v", m.ws.Background())
	}
	if !sameDir(t, m.activeWS().Root, filepath.Join(filepath.Dir(a), "b")) {
		t.Fatalf("closing a parked project must leave the active one alone, active = %s", m.activeWS().Root)
	}
	if !notified(m, "closed a via network (phone)") {
		t.Fatal("a remote close of a parked project must raise its notice")
	}
}

// TestNetCloseBusyBlockedNoPopup: a dirty active project is blocked with
// the guard's own summary lines; nothing changes and no prompt opens.
func TestNetCloseBusyBlockedNoPopup(t *testing.T) {
	m, a, _, dirtyPath := netDirtyFixture(t)
	m, r := netClose(t, m, netlink.CloseRequest{})
	if r.Outcome != netlink.CloseBlocked || r.Project != "a" || !sameDir(t, r.Root, a) {
		t.Fatalf("busy close %+v", r)
	}
	want := m.activeCloseActivity().summary()
	if len(r.Reasons) != 1 || r.Reasons[0] != "unsaved: f.txt" || r.Reasons[0] != want[0] {
		t.Fatalf("reasons %v, want the guard's lines %v", r.Reasons, want)
	}
	if m.shell.IsOpen() || m.projectClosePromptOpen() {
		t.Fatal("a blocked network close must not open the UI guard")
	}
	if len(m.ws.Background()) != 1 || !sameDir(t, m.activeWS().Root, a) {
		t.Fatal("a blocked close must touch no workspace")
	}
	if data, _ := os.ReadFile(dirtyPath); string(data) != "one\n" {
		t.Fatalf("blocked never writes, file = %q", data)
	}
}

// TestNetCloseForceDiscards: a force whose grant matches root and reasons
// runs the guard's discard branch — nothing is written, the parked project
// resumes — and the notice says what was discarded.
func TestNetCloseForceDiscards(t *testing.T) {
	m, a, bRoot, dirtyPath := netDirtyFixture(t)
	grant := netlink.ForceGrant{Root: m.activeWS().Root, Project: "a", Reasons: []string{"unsaved: f.txt"}}
	m, r := netClose(t, m, netlink.CloseRequest{Project: "a", Force: true, Grant: grant})
	if r.Outcome != netlink.CloseClosed {
		t.Fatalf("force %+v", r)
	}
	if !sameDir(t, m.activeWS().Root, bRoot) || len(m.ws.Background()) != 0 {
		t.Fatalf("force must close a and resume b, active = %s bg = %v", m.activeWS().Root, m.ws.Background())
	}
	if data, _ := os.ReadFile(dirtyPath); string(data) != "one\n" {
		t.Fatalf("force discards, never writes, file = %q", data)
	}
	if !notified(m, "closed a via network (phone) — discarded 1 unsaved buffer") {
		t.Fatal("a forced close must say what it discarded")
	}
	_ = a
}

// TestNetCloseForceStale: a grant for other reasons — or another root — is
// stale: nothing closes, and the next plain close is blocked afresh.
func TestNetCloseForceStale(t *testing.T) {
	m, a, _, _ := netDirtyFixture(t)
	root := m.activeWS().Root
	for _, grant := range []netlink.ForceGrant{
		{Root: root, Project: "a", Reasons: []string{"unsaved: g.txt"}},
		{Root: root, Project: "a", Reasons: []string{"unsaved: f.txt", "tool sql"}},
		{Root: root, Project: "a"},
		{Root: filepath.Join(filepath.Dir(root), "b"), Project: "b", Reasons: []string{"unsaved: f.txt"}},
	} {
		var r netlink.CloseResult
		m, r = netClose(t, m, netlink.CloseRequest{Force: true, Grant: grant})
		if r.Outcome != netlink.CloseStale {
			t.Fatalf("grant %+v: outcome %+v, want stale", grant, r)
		}
		if !sameDir(t, m.activeWS().Root, a) || len(m.ws.Background()) != 1 {
			t.Fatal("a stale force must touch nothing")
		}
	}
	_, r := netClose(t, m, netlink.CloseRequest{})
	if r.Outcome != netlink.CloseBlocked {
		t.Fatalf("after a stale force the plain close is blocked again: %+v", r)
	}
}

// TestNetCloseWhileGuardAsks: a UI close guard already asking the user owns
// the decision — the network verdict is unavailable and the prompt stays.
func TestNetCloseWhileGuardAsks(t *testing.T) {
	m, _, _, _ := netDirtyFixture(t)
	out, _ := m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if !m.projectClosePromptOpen() {
		t.Fatal("fixture: the busy close must prompt")
	}
	m, r := netClose(t, m, netlink.CloseRequest{})
	if r.Outcome != netlink.CloseUnavailable {
		t.Fatalf("close under an open guard %+v", r)
	}
	if !m.projectClosePromptOpen() || len(m.ws.Background()) != 1 {
		t.Fatal("the open guard must stay, untouched")
	}
}

// TestNetCloseLastProjectNeverQuits: with no parked workspace a forced close
// tears the project down and reopens it fresh — the dirty edit is gone, the
// file untouched, the IDE still standing in the same root.
func TestNetCloseLastProjectNeverQuits(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	m := dismissOnboarding(switchModel(t))
	m = openDirty(t, m, file)
	if ed := m.activeEditor(); ed == nil || !ed.Dirty() {
		t.Fatal("fixture: the edit must dirty the buffer")
	}
	root := m.activeWS().Root
	if len(m.ws.Background()) != 0 {
		t.Fatal("fixture: a single project")
	}

	_, r := netClose(t, m, netlink.CloseRequest{})
	if r.Outcome != netlink.CloseBlocked || len(r.Reasons) != 1 || r.Reasons[0] != "unsaved: f.txt" {
		t.Fatalf("the last project's guard blocks like any other: %+v", r)
	}
	before := m.ws.Active()
	m, r = netClose(t, m, netlink.CloseRequest{Force: true,
		Grant: netlink.ForceGrant{Root: root, Project: filepath.Base(root), Reasons: r.Reasons}})
	if r.Outcome != netlink.CloseClosed {
		t.Fatalf("force on the last project %+v", r)
	}
	if m.ws.Active() == nil || m.ws.Active() == before || !sameDir(t, m.ws.Active().Root, root) {
		t.Fatalf("the IDE must stand in the same root on a fresh workspace, active = %+v", m.ws.Active())
	}
	if !sameDir(t, cwd(t), dir) {
		t.Fatalf("cwd = %s, want the project root", cwd(t))
	}
	if dirty := collectActivity(m.ws.Active()).dirty; len(dirty) != 0 {
		t.Fatalf("the discarded edit must be gone, dirty = %v", dirty)
	}
	if data, _ := os.ReadFile(file); string(data) != "one\n" {
		t.Fatalf("force discards, never writes, file = %q", data)
	}
	if !notified(m, "closed "+filepath.Base(root)+" via network (phone) — discarded 1 unsaved buffer") {
		t.Fatal("the forced close must raise its notice")
	}
}

// TestNetCloseIdleLastProjectNeverQuits: the plain (unforced) close of the
// last, idle project reopens it fresh rather than quitting the IDE.
func TestNetCloseIdleLastProjectNeverQuits(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := dismissOnboarding(switchModel(t))
	before := m.ws.Active()
	m, r := netClose(t, m, netlink.CloseRequest{})
	if r.Outcome != netlink.CloseClosed {
		t.Fatalf("close %+v", r)
	}
	if m.ws.Active() == nil || m.ws.Active() == before || !sameDir(t, m.ws.Active().Root, dir) {
		t.Fatalf("the IDE must stand in the same root on a fresh workspace, active = %+v", m.ws.Active())
	}
	if !notified(m, "reopened at its start state") {
		t.Fatal("the restart must be announced")
	}
}

// TestNetCloseNotice: the wording — who asked, what a force discarded and
// stopped, and a client without a name falls back to its address.
func TestNetCloseNotice(t *testing.T) {
	phone := netlink.Client{Name: "phone", Addr: "10.0.0.2:5000"}
	act := wsActivity{dirty: []string{"a.go", "b.go"}, running: []string{"tool sql"}}
	if got := netCloseNotice("ike", phone, act, false); got != "closed ike via network (phone)" {
		t.Errorf("plain: %q", got)
	}
	want := "closed ike via network (phone) — discarded 2 unsaved buffers, stopped 1 running process"
	if got := netCloseNotice("ike", phone, act, true); got != want {
		t.Errorf("forced: %q, want %q", got, want)
	}
	if got := netCloseNotice("ike", netlink.Client{Addr: "10.0.0.2:5000"}, wsActivity{}, true); got != "closed ike via network (10.0.0.2:5000)" {
		t.Errorf("nameless client: %q", got)
	}
}

// TestNetCloseOverTheWire: the whole bridge — a paired client's close line
// reaches the Update loop through host.Send as a netCloseMsg, and the
// verdict the handler posts is what the client reads back.
func TestNetCloseOverTheWire(t *testing.T) {
	m, a, _ := twoCleanProjects(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	msgs := make(chan tea.Msg, 8)
	m.host.SetSender(func(msg tea.Msg) { msgs <- msg })
	cfg := config.Get()
	cfg.Network.Enabled, cfg.Network.Bind, cfg.Network.Port = true, "127.0.0.1", 0
	if err := m.startNetLink(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.nlServer.Close)
	token, _, err := m.nlServer.Store().Issue("phone", "127.0.0.1:1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	replies := make(chan netlink.Response, 1)
	go func() {
		conn, err := net.Dial("tcp", m.nlServer.Addr().String())
		if err != nil {
			t.Error(err)
			replies <- netlink.Response{}
			return
		}
		defer conn.Close()
		req, _ := json.Marshal(netlink.Request{Cmd: "close", Token: token, Project: "b"})
		_, _ = conn.Write(append(req, '\n'))
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, err := bufio.NewReader(conn).ReadString('\n')
		var resp netlink.Response
		if err == nil {
			err = json.Unmarshal([]byte(line), &resp)
		}
		if err != nil {
			t.Errorf("reply %q: %v", line, err)
		}
		replies <- resp
	}()
	// Background work of the switch (todo scan, watcher) sends too; only the
	// close is of interest.
	var msg tea.Msg
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg = <-msgs:
		case <-deadline:
			t.Fatal("the close never reached the Update loop")
		}
		if _, ok := msg.(netCloseMsg); ok {
			break
		}
	}
	out, _ := m.Update(msg)
	m = out.(Model)
	resp := <-replies
	if resp.Type != "ok" || resp.Project != "b" {
		t.Fatalf("wire reply %+v", resp)
	}
	if !sameDir(t, m.activeWS().Root, a) {
		t.Fatalf("the close must have resumed a, active = %s", m.activeWS().Root)
	}
}
