package terminal

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// TestSendKeyCtrlCDiscardsSpooledBacklog (#989): output buffered in the spool
// when ctrl+c arrives is pre-abort backlog and must not replay onto the grid —
// otherwise a fast producer keeps "running" on screen long after the interrupt
// landed. The test stalls the feed loop by holding gridMu (as a busy render
// would), spools marker chunks, sends ctrl+c and asserts the stalled markers
// never render. At most the single chunk already taken by the feed loop may
// land; the session stays fully usable afterwards.
func TestSendKeyCtrlCDiscardsSpooledBacklog(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	// Let the shell settle so the feed loop is idle-blocked in take().
	for _, r := range "echo ready-marker\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "shell ready", func() bool {
		return strings.Count(plainView(s), "ready-marker") >= 2
	})

	// Stall the feed loop like a busy render/resize would, then spool backlog
	// the way the PTY read loop does.
	s.gridMu.Lock()
	s.out.put([]byte("STALE-A\r\n"))
	s.out.put([]byte("STALE-B\r\n"))
	s.out.put([]byte("STALE-C\r\n"))

	s.SendKey(vt.KeyPressEvent{Code: 'c', Mod: vt.ModCtrl})
	s.gridMu.Unlock()

	// Post-abort output must still flow (the prompt, new commands).
	for _, r := range "echo after-abort\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "post-abort echo", func() bool {
		return strings.Count(plainView(s), "after-abort") >= 2
	})

	// The feed loop may have taken one chunk before the discard; the rest of
	// the backlog must be gone.
	view := plainView(s)
	if strings.Contains(view, "STALE-B") || strings.Contains(view, "STALE-C") {
		t.Fatalf("spooled backlog rendered after ctrl+c:\n%s", view)
	}
}

// TestSendKeyCtrlCWithEmptySpoolStillDelivers (#989): the discard path must
// not swallow the key itself — ctrl+c with nothing buffered reaches the child
// as a normal interrupt.
func TestSendKeyCtrlCWithEmptySpoolStillDelivers(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	for _, r := range "sleep 30\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "sleep echoed", func() bool {
		return strings.Contains(plainView(s), "sleep 30")
	})

	s.SendKey(vt.KeyPressEvent{Code: 'c', Mod: vt.ModCtrl})

	// The interrupt must kill the sleep and hand the prompt back.
	for _, r := range "echo interrupted-ok\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "prompt back after interrupt", func() bool {
		return strings.Count(plainView(s), "interrupted-ok") >= 2
	})
}
