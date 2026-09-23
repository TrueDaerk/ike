package netlink

import (
	"testing"
	"time"
)

// TestForceTokensIssueConsume: a minted token redeems once for its client
// and hands back the grant it was bound to; a second redemption finds
// nothing.
func TestForceTokensIssueConsume(t *testing.T) {
	var f ForceTokens
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	grant := ForceGrant{Root: "/home/dev/ike", Project: "ike", Reasons: []string{"unsaved: app.go"}}
	token, left, err := f.Issue("c1", grant, now)
	if err != nil || token == "" {
		t.Fatalf("issue: %q %v", token, err)
	}
	if left != 120 {
		t.Errorf("expires_in = %d, want 120", left)
	}
	got, ok := f.Consume("c1", token, now.Add(30*time.Second))
	if !ok {
		t.Fatal("the live token must redeem")
	}
	if got.Root != grant.Root || got.Project != grant.Project || !sameReasons(got.Reasons, grant.Reasons) {
		t.Errorf("grant %+v, want %+v", got, grant)
	}
	if _, ok := f.Consume("c1", token, now.Add(31*time.Second)); ok {
		t.Fatal("a token is single-use")
	}
}

// TestForceTokensExpiry: past ForceTokenTTL the token is dead, and the
// attempt burns it like any other.
func TestForceTokensExpiry(t *testing.T) {
	var f ForceTokens
	now := time.Now()
	token, _, _ := f.Issue("c1", ForceGrant{Root: "/r"}, now)
	if _, ok := f.Consume("c1", token, now.Add(ForceTokenTTL)); ok {
		t.Fatal("a token must not redeem at its expiry")
	}
	if _, ok := f.Consume("c1", token, now); ok {
		t.Fatal("an expired attempt burns the token")
	}
}

// TestForceTokensWrongGuessBurns: a wrong token drops the client's live one
// — the client has to ask for fresh reasons — and a token never redeems for
// another client.
func TestForceTokensWrongGuessBurns(t *testing.T) {
	var f ForceTokens
	now := time.Now()
	token, _, _ := f.Issue("c1", ForceGrant{Root: "/r"}, now)
	if _, ok := f.Consume("c1", "nope", now); ok {
		t.Fatal("a wrong token must not redeem")
	}
	if _, ok := f.Consume("c1", token, now); ok {
		t.Fatal("the wrong guess must have burned the live token")
	}
	token, _, _ = f.Issue("c1", ForceGrant{Root: "/r"}, now)
	if _, ok := f.Consume("c2", token, now); ok {
		t.Fatal("a token is bound to the client it was issued to")
	}
	if _, ok := f.Consume("c1", token, now); !ok {
		t.Fatal("another client's miss must not touch c1's token")
	}
}

// TestForceTokensReplaceAndForget: a new blocked reply replaces the client's
// token — only the reasons seen last can be forced — and Forget (unpair)
// drops it.
func TestForceTokensReplaceAndForget(t *testing.T) {
	var f ForceTokens
	now := time.Now()
	first, _, _ := f.Issue("c1", ForceGrant{Root: "/r", Reasons: []string{"a"}}, now)
	second, _, _ := f.Issue("c1", ForceGrant{Root: "/r", Reasons: []string{"a", "b"}}, now)
	if _, ok := f.Consume("c1", first, now); ok {
		t.Fatal("a replaced token must be dead")
	}
	second, _, _ = f.Issue("c1", ForceGrant{Root: "/r", Reasons: []string{"a", "b"}}, now)
	if g, ok := f.Consume("c1", second, now); !ok || len(g.Reasons) != 2 {
		t.Fatalf("the latest token must redeem with its own grant: %+v %v", g, ok)
	}
	third, _, _ := f.Issue("c1", ForceGrant{Root: "/r"}, now)
	f.Forget("c1")
	if _, ok := f.Consume("c1", third, now); ok {
		t.Fatal("Forget must drop the token")
	}
}

// TestForceGrantMatches: the grant matches the same root with the same
// reasons in the same order, and nothing else.
func TestForceGrantMatches(t *testing.T) {
	g := ForceGrant{Root: "/r", Reasons: []string{"tool sql", "unsaved: a.go"}}
	for _, tc := range []struct {
		name    string
		root    string
		reasons []string
		want    bool
	}{
		{"same", "/r", []string{"tool sql", "unsaved: a.go"}, true},
		{"other root", "/q", []string{"tool sql", "unsaved: a.go"}, false},
		{"reason added", "/r", []string{"tool sql", "unsaved: a.go, b.go"}, false},
		{"reason gone", "/r", []string{"tool sql"}, false},
		{"reordered", "/r", []string{"unsaved: a.go", "tool sql"}, false},
		{"idle now", "/r", nil, false},
	} {
		if got := g.Matches(tc.root, tc.reasons); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", tc.name, got, tc.want)
		}
	}
	if !(ForceGrant{Root: "/r"}).Matches("/r", nil) {
		t.Error("an idle grant matches an idle target")
	}
}
