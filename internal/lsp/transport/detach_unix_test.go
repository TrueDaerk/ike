//go:build !windows

package transport

import (
	"syscall"
	"testing"
)

// TestDetachedNewSession verifies Spec.Detached starts the process in its own
// session, detached from the caller's controlling terminal (#620): a session
// leader's session id equals its pid and differs from the test process's.
func TestDetachedNewSession(t *testing.T) {
	p, err := Start(Spec{Command: "sleep", Args: []string{"5"}, Detached: true})
	if err != nil {
		t.Skipf("sleep unavailable: %v", err)
	}
	defer p.Stop()

	pid := p.cmd.Process.Pid
	sid, err := syscall.Getsid(pid)
	if err != nil {
		t.Fatalf("getsid: %v", err)
	}
	if sid != pid {
		t.Fatalf("detached process sid = %d, want %d (own session leader)", sid, pid)
	}
	if own, _ := syscall.Getsid(0); sid == own {
		t.Fatalf("detached process shares the caller's session %d", own)
	}
}

// TestAttachedSharesSession is the control: without Detached the process stays
// in the caller's session, so it could receive the TUI's job-control signals.
func TestAttachedSharesSession(t *testing.T) {
	p, err := Start(Spec{Command: "sleep", Args: []string{"5"}})
	if err != nil {
		t.Skipf("sleep unavailable: %v", err)
	}
	defer p.Stop()

	sid, err := syscall.Getsid(p.cmd.Process.Pid)
	if err != nil {
		t.Fatalf("getsid: %v", err)
	}
	if own, _ := syscall.Getsid(0); sid != own {
		t.Fatalf("attached process sid = %d, want caller session %d", sid, own)
	}
}
