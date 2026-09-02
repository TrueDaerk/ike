package app

import "testing"

// TestIdleModelArmsNoTimers pins the #2402 idle rule structurally: a freshly
// settled model with no user activity holds zero armed demand tickers — the
// idle session's pass rate is then bounded by the documented exceptions alone
// (explorer keep-alive poll, forge poll where configured).
func TestIdleModelArmsNoTimers(t *testing.T) {
	m := sized(t, 100, 30)
	if n := m.armedTimers(); n != 0 {
		t.Fatalf("idle model holds %d armed timers, want 0 — a debounce tick "+
			"armed without user activity re-wakes an idle session", n)
	}
}

// TestTermCheckDeferralArmsNoTimer (#2402): a capability verdict parked behind
// a busy modal must wait event-driven — pending flag only, no retry ticker
// (the pre-#2402 retry tick was ~10 idle passes per 10s for the first minute
// of every session that started inside a modal).
func TestTermCheckDeferralArmsNoTimer(t *testing.T) {
	m := sized(t, 100, 30)
	m.caps.kitty = false
	m.shell.Open()
	base := m.armedTimers()
	m.runTermCheck()
	if !m.caps.pending {
		t.Fatal("busy shell must park the verdict as pending")
	}
	if n := m.armedTimers(); n != base {
		t.Fatalf("deferred verdict armed a timer: %d -> %d", base, n)
	}
}
