package explorer

import (
	"testing"
	"time"
)

// #2163 regression tests: the auto-refresh poll runs as chained Cmd
// goroutines with no cancellation seam, so chain identity (pollID) is the
// only thing keeping a project switch or a workspace resume from growing an
// extra permanent stat-walker per switch.

// TestStalePollChainRetires: a pollMsg minted by a chain this model does not
// own (a departed model's loop) is dropped without a re-arm.
func TestStalePollChainRetires(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.autoRefresh = true
	m.pollEvery = time.Millisecond
	if cmd := m.startPoll(); cmd == nil {
		t.Fatal("startPoll should schedule a poll")
	}
	own := m.pollID
	// A stale chain (different id) delivers: it must retire silently.
	if cmd := m.applyPoll(pollMsg{id: own - 1, changed: []string{root}}); cmd != nil {
		t.Fatal("stale pollMsg must not re-arm the chain")
	}
	// The owned chain keeps running.
	if cmd := m.applyPoll(pollMsg{id: own}); cmd == nil {
		t.Fatal("owned pollMsg must re-arm the chain")
	}
}

// TestInitRotatesPollChain: Init on a loaded root (session restore, parked
// workspace resume) always starts exactly one fresh chain — the previous id
// is retired, so a still-sleeping older loop cannot be re-armed into a
// duplicate.
func TestInitRotatesPollChain(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.autoRefresh = true
	m.pollEvery = time.Millisecond
	if cmd := m.startPoll(); cmd == nil {
		t.Fatal("startPoll should schedule a poll")
	}
	old := m.pollID
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init on a loaded root should schedule the poll")
	}
	if m.pollID == old {
		t.Fatal("Init must retire the previous chain id")
	}
	if cmd := m.applyPoll(pollMsg{id: old}); cmd != nil {
		t.Fatal("the pre-Init chain must be dropped after the rotation")
	}
	if !m.polling {
		t.Fatal("polling should be armed after Init")
	}
}

// TestPollDisableReenable: flipping auto_refresh off retires the chain and
// clears the armed flag; RearmPoll then restarts it — before #2163 the stale
// polling flag blocked the restart forever.
func TestPollDisableReenable(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.autoRefresh = true
	m.pollEvery = time.Millisecond
	if cmd := m.startPoll(); cmd == nil {
		t.Fatal("startPoll should schedule a poll")
	}
	id := m.pollID
	// Disable while the chain sleeps: its next delivery ends the chain and
	// clears the armed flag.
	m.autoRefresh = false
	m.polling = false // what Configure does on disable
	if cmd := m.applyPoll(pollMsg{id: id}); cmd != nil {
		t.Fatal("chain must end once auto-refresh is off")
	}
	if m.polling {
		t.Fatal("polling flag must clear when the chain ends")
	}
	// Re-enable: RearmPoll starts a fresh chain.
	m.autoRefresh = true
	if cmd := m.RearmPoll(); cmd == nil {
		t.Fatal("RearmPoll must restart the chain after a re-enable")
	}
	if m.pollID == id {
		t.Fatal("the restarted chain needs a fresh id")
	}
	// And is idempotent while one runs.
	if cmd := m.RearmPoll(); cmd != nil {
		t.Fatal("RearmPoll must be a no-op while a chain runs")
	}
}
