package netlink

import (
	"errors"
	"sync"
	"time"
)

// pairing.go is the state machine behind the pairing popup. There is at most
// one live challenge per instance — the one the popup shows. A challenge is
// born from a client's pair request (or from an unauthenticated command),
// dies on a correct answer, a wrong answer (which immediately breeds a fresh
// one), expiry, or the user cancelling the popup.
//
// Brute force is hopeless by construction (16^6 ≈ 16.8 million codes, one
// guess per code, a few dozen seconds per code) but the machine still slows
// repeat offenders down: every wrong guess costs a growing delay, and a
// handful of consecutive misses from one address blocks that address for a
// while — the popup stops flickering with new codes on a scanner's behalf.

const (
	// DefaultCodeTTL is how long a code stays valid — the popup's bar counts
	// it down. Long enough to read six glyphs off one screen and tap them
	// into another, short enough that a code seen over a shoulder is useless
	// minutes later.
	DefaultCodeTTL = 90 * time.Second
	// maxFailures consecutive wrong guesses from one address block it.
	maxFailures = 5
	// blockFor is the block duration after maxFailures misses.
	blockFor = 5 * time.Minute
	// cancelCooldown keeps an address the user just refused from reopening
	// the popup right away.
	cancelCooldown = 30 * time.Second
	// wrongDelayStep is the response delay per accumulated failure.
	wrongDelayStep = 2 * time.Second
)

// Challenge is one live pairing code together with what the popup shows
// about it.
type Challenge struct {
	Code    Code
	Issued  time.Time
	Expires time.Time
	// Client is the name the client gave itself; "" when it gave none.
	Client string
	// Addr is the remote address (host:port) the request came from.
	Addr string
	// Reason says why this code exists: "new" (fresh request), "wrong" (the
	// previous guess missed), "expired" (the previous code timed out).
	Reason string
}

// Verdict is the outcome of one guess.
type Verdict int

const (
	// VerdictOK — the guess matched; the client is paired.
	VerdictOK Verdict = iota
	// VerdictWrong — the guess missed; a fresh challenge replaces the code.
	VerdictWrong
	// VerdictExpired — the code had timed out; a fresh challenge is issued.
	VerdictExpired
	// VerdictNone — no challenge was live (never requested, or cancelled).
	VerdictNone
	// VerdictBlocked — the address is blocked after repeated misses or a
	// user refusal; no new challenge is issued.
	VerdictBlocked
)

// ErrBlocked is returned by Begin for an address that is currently blocked.
var ErrBlocked = errors.New("address is blocked for now")

// Events receives the state changes the UI cares about. Calls happen on the
// connection goroutine; implementations must hand off (host.Send) and never
// block.
type Events interface {
	// ChallengeIssued fires for every new code, including replacements.
	ChallengeIssued(Challenge)
	// ChallengeCleared fires when the live challenge ends without a
	// replacement: paired, or the address got blocked.
	ChallengeCleared()
	// Paired fires once a client holds a token.
	Paired(Client)
}

// Pairing is the state machine. Safe for concurrent use.
type Pairing struct {
	mu       sync.Mutex
	ttl      time.Duration
	now      func() time.Time
	events   Events
	cur      *Challenge
	failures map[string]int       // consecutive misses per address host
	blocked  map[string]time.Time // block expiry per address host
}

// NewPairing builds a machine with ttl per code (0 selects DefaultCodeTTL).
func NewPairing(ttl time.Duration, events Events) *Pairing {
	if ttl <= 0 {
		ttl = DefaultCodeTTL
	}
	return &Pairing{
		ttl:      ttl,
		now:      time.Now,
		events:   events,
		failures: map[string]int{},
		blocked:  map[string]time.Time{},
	}
}

// Begin issues a fresh code for a client at addr, replacing any live one.
func (p *Pairing) Begin(client, addr string) (Challenge, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isBlockedLocked(addr) {
		return Challenge{}, ErrBlocked
	}
	return p.issueLocked(client, addr, "new"), nil
}

// Attempt checks a guess from addr. The returned challenge is the
// replacement code for VerdictWrong / VerdictExpired (zero otherwise), and
// delay is how long the caller should wait before answering — a growing
// penalty for consecutive misses from that address.
func (p *Pairing) Attempt(addr string, guess Code) (v Verdict, next Challenge, delay time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isBlockedLocked(addr) {
		return VerdictBlocked, Challenge{}, 0
	}
	cur := p.cur
	if cur == nil {
		return VerdictNone, Challenge{}, 0
	}
	if !p.now().Before(cur.Expires) {
		return VerdictExpired, p.issueLocked(cur.Client, addr, "expired"), 0
	}
	if cur.Code.Equal(guess) {
		p.cur = nil
		delete(p.failures, hostOf(addr))
		if p.events != nil {
			p.events.ChallengeCleared()
		}
		return VerdictOK, Challenge{}, 0
	}
	host := hostOf(addr)
	p.failures[host]++
	n := p.failures[host]
	if n >= maxFailures {
		p.blocked[host] = p.now().Add(blockFor)
		delete(p.failures, host)
		p.cur = nil
		if p.events != nil {
			p.events.ChallengeCleared()
		}
		return VerdictBlocked, Challenge{}, 0
	}
	return VerdictWrong, p.issueLocked(cur.Client, addr, "wrong"), time.Duration(n) * wrongDelayStep
}

// Cancel is the user refusing the popup: the live code dies, and the address
// it came from cannot request another one for cancelCooldown. Returns the
// cancelled challenge and whether there was one.
func (p *Pairing) Cancel() (Challenge, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil {
		return Challenge{}, false
	}
	c := *p.cur
	p.cur = nil
	p.blocked[hostOf(c.Addr)] = p.now().Add(cancelCooldown)
	return c, true
}

// Expire drops the live challenge when its time is up (the UI's tick calls
// it once the bar hits zero). A later guess then gets VerdictNone rather than
// a free replacement code; the client asks again explicitly.
func (p *Pairing) Expire() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil || p.now().Before(p.cur.Expires) {
		return false
	}
	p.cur = nil
	return true
}

// Current returns the live challenge, ok false when there is none.
func (p *Pairing) Current() (Challenge, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil {
		return Challenge{}, false
	}
	return *p.cur, true
}

// issueLocked mints a code and announces it. Called with mu held.
func (p *Pairing) issueLocked(client, addr, reason string) Challenge {
	now := p.now()
	c := Challenge{Code: Generate(), Issued: now, Expires: now.Add(p.ttl),
		Client: client, Addr: addr, Reason: reason}
	p.cur = &c
	if p.events != nil {
		p.events.ChallengeIssued(c)
	}
	return c
}

// isBlockedLocked reports whether addr's host is blocked, forgetting stale
// blocks as it goes. Called with mu held.
func (p *Pairing) isBlockedLocked(addr string) bool {
	host := hostOf(addr)
	until, ok := p.blocked[host]
	if !ok {
		return false
	}
	if p.now().Before(until) {
		return true
	}
	delete(p.blocked, host)
	return false
}

// hostOf strips the port off host:port so every connection from one device
// shares its failure count.
func hostOf(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
