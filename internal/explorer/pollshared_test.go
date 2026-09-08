package explorer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// pollshared_test.go guards the shared-stamp poll chain (#2540): the
// goroutine watches the stamp set the model publishes on every rebuild, so
// it never has to wake the program just to refresh its snapshot; a retired
// chain returns nil (no pass at all) instead of a stale pollMsg.

// runPoll runs the chain's Cmd with a deadline, so a chain that never
// returns fails the test instead of hanging it.
func runPoll(t *testing.T, cmd tea.Cmd, want string) tea.Msg {
	t.Helper()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatalf("the poll chain never returned (%s)", want)
		return nil
	}
}

// stampPaths lists the paths of the published stamp set.
func stampPaths(m *Model) []string {
	ps := m.pollState()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	out := make([]string, 0, len(ps.stamps))
	for _, s := range ps.stamps {
		out = append(out, s.path)
	}
	return out
}

func hasPath(paths []string, p string) bool {
	for _, x := range paths {
		if x == p {
			return true
		}
	}
	return false
}

// TestRetiredPollChainReturnsNil: RetirePoll (a parked workspace, a project
// switch, auto-refresh off) makes the running goroutine return nil on its
// next round — bubbletea drops nil before Update, so retiring costs no pass.
func TestRetiredPollChainReturnsNil(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.autoRefresh = true
	m.pollEvery = time.Millisecond
	cmd := m.startPoll()
	if cmd == nil {
		t.Fatal("startPoll should schedule a poll")
	}
	m.RetirePoll()
	if msg := runPoll(t, cmd, "retired chain"); msg != nil {
		t.Fatalf("a retired chain must return nil, got %#v", msg)
	}
	if m.polling {
		t.Fatal("RetirePoll clears the armed flag")
	}
}

// TestRotatedPollChainRetiresTheOldGoroutine: Init on a loaded root rotates
// the chain id (#2163); the goroutine armed under the old id must retire
// silently rather than deliver a stale pollMsg for applyPoll to drop.
func TestRotatedPollChainRetiresTheOldGoroutine(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.autoRefresh = true
	m.pollEvery = time.Millisecond
	old := m.startPoll()
	if old == nil {
		t.Fatal("startPoll should schedule a poll")
	}
	if m.Init() == nil {
		t.Fatal("Init on a loaded root schedules the fresh chain")
	}
	if msg := runPoll(t, old, "superseded chain"); msg != nil {
		t.Fatalf("the superseded chain must return nil, got %#v", msg)
	}
}

// TestPollChainSeesExpandedDirsWithoutAWake: a directory expanded while the
// chain runs joins the published stamp set on the rebuild, so the same
// goroutine reports a change under it — no snapshot-refresh wake needed.
func TestPollChainSeesExpandedDirsWithoutAWake(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.autoRefresh = true
	m.pollEvery = time.Millisecond
	cmd := m.startPoll()
	if cmd == nil {
		t.Fatal("startPoll should schedule a poll")
	}
	sub := filepath.Join(root, "sub")
	if hasPath(stampPaths(&m), sub) {
		t.Fatal("test setup: sub is collapsed and must not be watched yet")
	}
	// Expand sub: the scan lands, rebuild publishes the wider stamp set.
	m.cursor = rowIndex(t, m, "sub")
	m, _ = send(m, key("l"))
	if !hasPath(stampPaths(&m), sub) {
		t.Fatalf("expanding sub must publish its stamp, got %v", stampPaths(&m))
	}
	// A change under sub is reported by the chain that was started before
	// the expand — it reads the published set each round.
	mustWrite(t, filepath.Join(sub, "later.txt"), "x")
	msg, ok := runPoll(t, cmd, "change under sub").(pollMsg)
	if !ok {
		t.Fatalf("poll cmd returned %#v", msg)
	}
	if !hasPath(msg.changed, sub) {
		t.Fatalf("the chain should report sub, got %v", msg.changed)
	}
	if msg.id != m.pollID {
		t.Fatal("the report carries the chain the model owns")
	}
}

// TestPollChainDropsDirsUnderACollapsedOne: collapsing a directory drops
// the loaded directories beneath it from the published set (the collapsed
// one itself stays watched, as a loaded node always is), so a change deep
// inside a folded subtree no longer wakes the app.
func TestPollChainDropsDirsUnderACollapsedOne(t *testing.T) {
	root := tree(t)
	sub := filepath.Join(root, "sub")
	deep := filepath.Join(sub, "deep")
	if err := os.Mkdir(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	m := mounted(t, root, 40, 20)
	// Expand and collapse before polling starts: with auto-refresh on, the
	// expand's scan would arm the chain and pumpScans would drain it.
	m.cursor = rowIndex(t, m, "sub")
	m, _ = send(m, key("l")) // expand sub
	m.cursor = rowIndex(t, m, "deep")
	m, _ = send(m, key("l")) // expand deep
	m.autoRefresh = true
	m.pollEvery = time.Millisecond
	if cmd := m.startPoll(); cmd == nil {
		t.Fatal("startPoll should schedule a poll")
	}
	if got := stampPaths(&m); !hasPath(got, sub) || !hasPath(got, deep) {
		t.Fatalf("both expanded dirs are watched, got %v", got)
	}
	m.cursor = rowIndex(t, m, "sub")
	m, _ = send(m, key("h")) // collapse sub
	got := stampPaths(&m)
	if hasPath(got, deep) {
		t.Fatalf("deep must leave the stamp set once sub is collapsed, got %v", got)
	}
	if !hasPath(got, sub) || !hasPath(got, root) {
		t.Fatalf("sub (loaded) and the root stay watched, got %v", got)
	}
}

// rowIndex finds the row of the entry named name.
func rowIndex(t *testing.T, m Model, name string) int {
	t.Helper()
	for i, n := range m.rows {
		if n.name == name {
			return i
		}
	}
	t.Fatalf("no row named %q in %v", name, names(m))
	return -1
}

// TestPollChainReportsVanishedDir: a watched directory that disappears is
// reported as its parent (the parent's listing changed).
func TestPollChainReportsVanishedDir(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	sub := filepath.Join(root, "sub")
	m.cursor = rowIndex(t, m, "sub")
	m, _ = send(m, key("l")) // expand while auto-refresh is still off
	m.autoRefresh = true
	m.pollEvery = time.Millisecond
	cmd := m.startPoll()
	if cmd == nil {
		t.Fatal("startPoll should schedule a poll")
	}
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	msg, ok := runPoll(t, cmd, "vanished sub").(pollMsg)
	if !ok {
		t.Fatalf("poll cmd returned %#v", msg)
	}
	if !hasPath(msg.changed, root) {
		t.Fatalf("a vanished sub reports its parent, got %v", msg.changed)
	}
}
