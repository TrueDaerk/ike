package netlink

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"sync"
	"time"
)

// force.go holds the one-time force tokens of the close command (#2703). A
// close refused by the IDE's busy guard answers "blocked" with the guard's
// reasons and a token; echoing that token on a second close discards the
// listed activity. The token is what proves the client saw the reasons: it
// is bound to the pairing (keyed by client id), to the project the guard
// spoke about and to the exact set of reasons, lives ForceTokenTTL, and is
// consumed on first use — right or wrong.
//
// The tokens are in-memory only and hashed like pairing tokens: a restart
// forgets them (the client simply asks again), and nothing on the heap holds
// a usable credential in the clear.

// ForceTokenTTL is how long a force token stays valid: long enough to read
// the reasons and decide, short enough that a forgotten token cannot be
// replayed much later.
const ForceTokenTTL = 120 * time.Second

// ForceGrant is what a force token was issued for — the binding a force
// request has to match.
type ForceGrant struct {
	// Root is the resolved project root the guard evaluated.
	Root string
	// Project is the project name as the blocked reply named it.
	Project string
	// Reasons are the guard's summary lines the client was shown.
	Reasons []string
}

// forceEntry is one outstanding token: its hash, its grant and its expiry.
type forceEntry struct {
	hash    string
	grant   ForceGrant
	expires time.Time
}

// ForceTokens is the in-memory force-token table, one outstanding token per
// paired client: a new blocked reply replaces the client's earlier token, so
// only the reasons the client saw last can ever be forced.
type ForceTokens struct {
	mu      sync.Mutex
	entries map[string]forceEntry // by client id
}

// Issue mints a token for clientID bound to grant, valid ForceTokenTTL from
// now, replacing any token the client still had. It returns the plaintext
// token — the only time it exists outside the client — and the seconds it
// stays valid.
func (f *ForceTokens) Issue(clientID string, grant ForceGrant, now time.Time) (token string, expiresIn int, err error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", 0, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw[:])
	grant.Reasons = append([]string(nil), grant.Reasons...)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.entries == nil {
		f.entries = map[string]forceEntry{}
	}
	f.entries[clientID] = forceEntry{hash: hashToken(token), grant: grant, expires: now.Add(ForceTokenTTL)}
	return token, int(ForceTokenTTL / time.Second), nil
}

// Consume redeems token for clientID: the client's outstanding token is
// dropped whatever the outcome (single use — a wrong or expired guess burns
// it too, so the next plain close issues a fresh one), and ok reports
// whether it was the live token of this client. The hash comparison is
// constant-time.
func (f *ForceTokens) Consume(clientID, token string, now time.Time) (ForceGrant, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[clientID]
	if !ok {
		return ForceGrant{}, false
	}
	delete(f.entries, clientID)
	if token == "" || !now.Before(e.expires) {
		return ForceGrant{}, false
	}
	if subtle.ConstantTimeCompare([]byte(e.hash), []byte(hashToken(token))) != 1 {
		return ForceGrant{}, false
	}
	return e.grant, true
}

// Forget drops the client's outstanding token, if any — an unpaired client
// must not be able to force anything with a token minted for its old
// pairing.
func (f *ForceTokens) Forget(clientID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, clientID)
}

// sameReasons reports whether two reason lists are identical, line for line
// and in order — the guard renders its summary deterministically, so any
// difference means the activity changed since the client saw it.
func sameReasons(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Matches reports whether the grant still describes the target the force
// request names: the same root and exactly the reasons the client was
// shown. A buffer dirtied or a process started since makes the grant stale.
func (g ForceGrant) Matches(root string, reasons []string) bool {
	return g.Root == root && sameReasons(g.Reasons, reasons)
}
