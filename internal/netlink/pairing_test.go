package netlink

import (
	"errors"
	"testing"
	"time"
)

// recorder captures pairing events.
type recorder struct {
	issued  []Challenge
	cleared int
	paired  []Client
}

func (r *recorder) ChallengeIssued(c Challenge) { r.issued = append(r.issued, c) }
func (r *recorder) ChallengeCleared()           { r.cleared++ }
func (r *recorder) Paired(c Client)             { r.paired = append(r.paired, c) }

// newTestPairing builds a machine on a controllable clock.
func newTestPairing(ttl time.Duration) (*Pairing, *recorder, *time.Time) {
	rec := &recorder{}
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	p := NewPairing(ttl, rec)
	p.now = func() time.Time { return now }
	return p, rec, &now
}

// wrongGuess returns a code that differs from c.
func wrongGuess(c Code) Code {
	g := c
	g[0] = nextDigit(g[0])
	return g
}

// TestPairingRightGuessPairs: a correct guess clears the challenge.
func TestPairingRightGuessPairs(t *testing.T) {
	p, rec, _ := newTestPairing(time.Minute)
	c, err := p.Begin("phone", "10.0.0.2:5000")
	if err != nil {
		t.Fatal(err)
	}
	if c.Reason != "new" || c.Client != "phone" || len(rec.issued) != 1 {
		t.Fatalf("challenge %+v, issued %d", c, len(rec.issued))
	}
	v, _, delay := p.Attempt("10.0.0.2:5001", c.Code)
	if v != VerdictOK || delay != 0 {
		t.Fatalf("verdict %v delay %v", v, delay)
	}
	if _, ok := p.Current(); ok || rec.cleared != 1 {
		t.Fatal("the challenge must be gone after pairing")
	}
}

// TestPairingWrongGuessRegenerates: a miss breeds a fresh code with reason
// "wrong" and a growing delay.
func TestPairingWrongGuessRegenerates(t *testing.T) {
	p, rec, _ := newTestPairing(time.Minute)
	c, _ := p.Begin("", "10.0.0.2:5000")
	v, next, delay := p.Attempt("10.0.0.2:5000", wrongGuess(c.Code))
	if v != VerdictWrong || next.Reason != "wrong" || next.Code.Equal(c.Code) {
		t.Fatalf("verdict %v next %+v", v, next)
	}
	if delay != wrongDelayStep {
		t.Fatalf("first miss delay %v", delay)
	}
	v, _, delay = p.Attempt("10.0.0.2:5000", wrongGuess(next.Code))
	if v != VerdictWrong || delay != 2*wrongDelayStep {
		t.Fatalf("second miss: %v %v", v, delay)
	}
	if len(rec.issued) != 3 {
		t.Fatalf("issued %d codes, want 3", len(rec.issued))
	}
	// The old code is dead: guessing it now is just another miss.
	if v, _, _ := p.Attempt("10.0.0.2:5000", c.Code); v != VerdictWrong {
		t.Fatalf("a superseded code must not pair, got %v", v)
	}
}

// TestPairingBlocksAfterRepeatedMisses: maxFailures misses from one host
// block it; Begin refuses, Attempt reports blocked, and the block lifts
// after blockFor.
func TestPairingBlocksAfterRepeatedMisses(t *testing.T) {
	p, rec, now := newTestPairing(time.Minute)
	c, _ := p.Begin("", "10.0.0.2:5000")
	var v Verdict
	for i := 0; i < maxFailures; i++ {
		v, c, _ = p.Attempt("10.0.0.2:5000", wrongGuess(c.Code))
	}
	if v != VerdictBlocked {
		t.Fatalf("after %d misses want blocked, got %v", maxFailures, v)
	}
	if _, ok := p.Current(); ok || rec.cleared != 1 {
		t.Fatal("blocking clears the live challenge")
	}
	if _, err := p.Begin("", "10.0.0.2:6000"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("Begin from the blocked host must refuse, got %v", err)
	}
	if _, err := p.Begin("", "10.0.0.3:6000"); err != nil {
		t.Fatalf("another host is unaffected: %v", err)
	}
	*now = now.Add(blockFor + time.Second)
	if _, err := p.Begin("", "10.0.0.2:6000"); err != nil {
		t.Fatalf("the block must lift: %v", err)
	}
}

// TestPairingExpiry: a guess after the TTL yields a fresh "expired" code;
// Expire drops a timed-out challenge for the UI.
func TestPairingExpiry(t *testing.T) {
	p, _, now := newTestPairing(30 * time.Second)
	c, _ := p.Begin("", "10.0.0.2:5000")
	if p.Expire() {
		t.Fatal("Expire must not drop a live code")
	}
	*now = now.Add(31 * time.Second)
	v, next, _ := p.Attempt("10.0.0.2:5000", c.Code)
	if v != VerdictExpired || next.Reason != "expired" || next.Code.Equal(c.Code) {
		t.Fatalf("verdict %v next %+v", v, next)
	}
	*now = now.Add(31 * time.Second)
	if !p.Expire() {
		t.Fatal("Expire must drop the timed-out code")
	}
	if v, _, _ := p.Attempt("10.0.0.2:5000", next.Code); v != VerdictNone {
		t.Fatalf("after Expire a guess finds no challenge, got %v", v)
	}
}

// TestPairingCancelCoolsDown: the user refusing the popup kills the code and
// keeps that host from asking again for a while.
func TestPairingCancelCoolsDown(t *testing.T) {
	p, _, now := newTestPairing(time.Minute)
	if _, ok := p.Cancel(); ok {
		t.Fatal("nothing to cancel yet")
	}
	c, _ := p.Begin("phone", "10.0.0.2:5000")
	got, ok := p.Cancel()
	if !ok || !got.Code.Equal(c.Code) {
		t.Fatal("Cancel returns the live challenge")
	}
	if _, err := p.Begin("phone", "10.0.0.2:5001"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("the refused host must wait, got %v", err)
	}
	*now = now.Add(cancelCooldown + time.Second)
	if _, err := p.Begin("phone", "10.0.0.2:5001"); err != nil {
		t.Fatalf("cooldown over: %v", err)
	}
}

// TestHostOf: the port is stripped for IPv4 and bracketed IPv6.
func TestHostOf(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.2:5000": "10.0.0.2", "[::1]:5000": "[::1]", "nohost": "nohost",
	} {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}
