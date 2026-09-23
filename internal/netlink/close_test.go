package netlink

import (
	"sync"
	"testing"
	"time"
)

// fakeIDE stands in for the update loop behind Options.Close: it records
// every request and answers from a scripted state — busy with reasons, or
// idle — closing on a plain close when idle and on a force whose grant still
// matches.
type fakeIDE struct {
	mu      sync.Mutex
	root    string
	project string
	reasons []string // non-empty = busy
	unknown bool     // no project matches
	reqs    []CloseRequest
	hang    bool // never reply
}

func (f *fakeIDE) close(req CloseRequest, reply func(CloseResult)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if f.hang {
		return
	}
	switch {
	case f.unknown:
		reply(CloseResult{Outcome: CloseUnknown})
	case req.Force && !req.Grant.Matches(f.root, f.reasons):
		reply(CloseResult{Outcome: CloseStale, Root: f.root, Project: f.project})
	case req.Force:
		f.reasons = nil
		reply(CloseResult{Outcome: CloseClosed, Root: f.root, Project: f.project})
	case len(f.reasons) > 0:
		reply(CloseResult{Outcome: CloseBlocked, Root: f.root, Project: f.project,
			Reasons: append([]string(nil), f.reasons...)})
	default:
		reply(CloseResult{Outcome: CloseClosed, Root: f.root, Project: f.project})
	}
}

func (f *fakeIDE) requests() []CloseRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CloseRequest(nil), f.reqs...)
}

func (f *fakeIDE) setReasons(r ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reasons = r
}

// closeServer starts a server wired to a fresh fakeIDE and pairs one client.
func closeServer(t *testing.T, ide *fakeIDE, mut ...func(*Options)) (*Server, *syncRecorder, *client) {
	t.Helper()
	srv, rec, _ := testServer(t, append([]func(*Options){func(o *Options) { o.Close = ide.close }}, mut...)...)
	c := dial(t, srv)
	pairClient(t, srv, rec, c)
	return srv, rec, c
}

// TestServerCloseUnauthorized: close is guarded like status — an unpaired
// asker is refused and no pairing popup appears.
func TestServerCloseUnauthorized(t *testing.T) {
	ide := &fakeIDE{root: "/home/dev/ike", project: "ike"}
	srv, rec, _ := testServer(t, func(o *Options) { o.Close = ide.close })
	c := dial(t, srv)
	if r := c.send(Request{Cmd: "close"}); r.Type != "error" || r.Error != CodeUnauthorized {
		t.Fatalf("close unpaired %+v", r)
	}
	if r := c.send(Request{Cmd: "close", Token: "bogus", Project: "ike"}); r.Error != CodeUnauthorized {
		t.Fatalf("close with a bogus token %+v", r)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.issued) != 0 {
		t.Fatalf("close must not open a pairing challenge: %+v", rec.issued)
	}
	if len(ide.requests()) != 0 {
		t.Fatal("an unpaired close must never reach the IDE")
	}
}

// TestServerCloseIdle: an idle project closes and the IDE learns who asked
// and which project; no project at all means the active one.
func TestServerCloseIdle(t *testing.T) {
	ide := &fakeIDE{root: "/home/dev/ike", project: "ike"}
	_, _, c := closeServer(t, ide)
	r := c.send(Request{Cmd: "close", Project: "ike"})
	if r.Type != "ok" || r.Project != "ike" {
		t.Fatalf("close idle %+v", r)
	}
	if r := c.send(Request{Cmd: "close"}); r.Type != "ok" {
		t.Fatalf("close active %+v", r)
	}
	reqs := ide.requests()
	if len(reqs) != 2 || reqs[0].Project != "ike" || reqs[0].Force || reqs[0].Client.Name != "phone" {
		t.Fatalf("requests %+v", reqs)
	}
	if reqs[1].Project != "" || reqs[1].Remote != "" {
		t.Fatalf("a close without a project names the active one: %+v", reqs[1])
	}
}

// TestServerCloseRemote: a remote is normalised before it reaches the IDE;
// an unparseable one, or a remote beside a project, is a bad request.
func TestServerCloseRemote(t *testing.T) {
	ide := &fakeIDE{root: "/home/dev/ike", project: "ike"}
	_, _, c := closeServer(t, ide)
	if r := c.send(Request{Cmd: "close", Remote: "git@github.com:TrueDaerk/ike.git"}); r.Type != "ok" {
		t.Fatalf("close by remote %+v", r)
	}
	if reqs := ide.requests(); len(reqs) != 1 || reqs[0].Remote != "github.com/truedaerk/ike" {
		t.Fatalf("the remote must arrive normalised: %+v", reqs)
	}
	if r := c.send(Request{Cmd: "close", Remote: "not a remote"}); r.Error != CodeBadRequest {
		t.Fatalf("bad remote %+v", r)
	}
	if r := c.send(Request{Cmd: "close", Remote: "git@github.com:a/b.git", Project: "b"}); r.Error != CodeBadRequest {
		t.Fatalf("project and remote together %+v", r)
	}
}

// TestServerCloseUnavailable: no Close hook, an unknown project, and an IDE
// that never answers all come back as unavailable — the connection is not
// held hostage by a wedged update loop.
func TestServerCloseUnavailable(t *testing.T) {
	t.Run("no hook", func(t *testing.T) {
		srv, rec, _ := testServer(t)
		c := dial(t, srv)
		pairClient(t, srv, rec, c)
		if r := c.send(Request{Cmd: "close"}); r.Type != "error" || r.Error != CodeUnavailable {
			t.Fatalf("close %+v", r)
		}
	})
	t.Run("unknown project", func(t *testing.T) {
		_, _, c := closeServer(t, &fakeIDE{unknown: true})
		if r := c.send(Request{Cmd: "close", Project: "nope"}); r.Error != CodeUnavailable {
			t.Fatalf("close %+v", r)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		_, _, c := closeServer(t, &fakeIDE{hang: true}, func(o *Options) { o.CloseTimeout = 50 * time.Millisecond })
		start := time.Now()
		r := c.send(Request{Cmd: "close"})
		if r.Error != CodeUnavailable {
			t.Fatalf("close %+v", r)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatal("the timeout must bound the wait")
		}
	})
}

// TestServerCloseBlockedThenForce walks the guarded path: a busy project is
// blocked with the guard's lines and a token; echoing the token forces the
// close with the grant bound to root and reasons; the same token again is
// refused; a plain close after that mints a different token.
func TestServerCloseBlockedThenForce(t *testing.T) {
	ide := &fakeIDE{root: "/home/dev/ike", project: "ike",
		reasons: []string{"running shell process: vim", "unsaved: app.go"}}
	_, _, c := closeServer(t, ide)

	r := c.send(Request{Cmd: "close", Project: "ike"})
	if r.Type != "blocked" || r.ForceToken == "" || r.ExpiresIn != 120 || r.Project != "ike" {
		t.Fatalf("blocked %+v", r)
	}
	if len(r.Reasons) != 2 || r.Reasons[0] != "running shell process: vim" || r.Reasons[1] != "unsaved: app.go" {
		t.Fatalf("reasons must be the guard's lines verbatim: %v", r.Reasons)
	}
	first := r.ForceToken

	f := c.send(Request{Cmd: "close", Project: "ike", Force: first})
	if f.Type != "ok" {
		t.Fatalf("force %+v", f)
	}
	reqs := ide.requests()
	last := reqs[len(reqs)-1]
	if !last.Force || last.Grant.Root != "/home/dev/ike" || !sameReasons(last.Grant.Reasons, r.Reasons) {
		t.Fatalf("the force must carry the grant the token was bound to: %+v", last)
	}

	if again := c.send(Request{Cmd: "close", Project: "ike", Force: first}); again.Type != "forbidden" || again.Reason != ReasonStaleForceToken {
		t.Fatalf("a used token must be forbidden: %+v", again)
	}
	if n := len(ide.requests()); n != len(reqs) {
		t.Fatal("a spent token must be refused before the IDE is asked")
	}

	ide.setReasons("tool sql")
	r2 := c.send(Request{Cmd: "close", Project: "ike"})
	if r2.Type != "blocked" || r2.ForceToken == "" || r2.ForceToken == first {
		t.Fatalf("a fresh blocked reply mints a fresh token: %+v", r2)
	}
}

// TestServerCloseForceRefused: a wrong token, an expired one, one minted for
// another client, and a grant the activity has outgrown are all forbidden
// with stale_force_token, and the next plain close is blocked afresh.
func TestServerCloseForceRefused(t *testing.T) {
	ide := &fakeIDE{root: "/home/dev/ike", project: "ike", reasons: []string{"unsaved: app.go"}}
	srv, _, c := closeServer(t, ide)

	blocked := func() Response {
		t.Helper()
		r := c.send(Request{Cmd: "close", Project: "ike"})
		if r.Type != "blocked" || r.ForceToken == "" {
			t.Fatalf("blocked %+v", r)
		}
		return r
	}
	expectForbidden := func(r Response, what string) {
		t.Helper()
		if r.Type != "forbidden" || r.Reason != ReasonStaleForceToken {
			t.Fatalf("%s: %+v", what, r)
		}
	}

	// Wrong token — and the wrong guess burns the live one.
	live := blocked().ForceToken
	expectForbidden(c.send(Request{Cmd: "close", Project: "ike", Force: "wrong"}), "wrong token")
	expectForbidden(c.send(Request{Cmd: "close", Project: "ike", Force: live}), "token after a wrong guess")

	// Expired.
	live = blocked().ForceToken
	base := time.Now()
	srv.now = func() time.Time { return base.Add(ForceTokenTTL + time.Second) }
	expectForbidden(c.send(Request{Cmd: "close", Project: "ike", Force: live}), "expired token")
	srv.now = time.Now

	// Another client's token.
	live = blocked().ForceToken
	otherToken, _, err := srv.Store().Issue("laptop", "127.0.0.1:9", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	other := dial(t, srv)
	expectForbidden(other.send(Request{Cmd: "close", Token: otherToken, Project: "ike", Force: live}), "another client's token")
	if r := c.send(Request{Cmd: "close", Project: "ike", Force: live}); r.Type != "ok" {
		t.Fatalf("the owner's token must survive another client's miss: %+v", r)
	}

	// Activity changed since the blocked reply: the IDE calls it stale.
	ide.setReasons("unsaved: app.go")
	live = blocked().ForceToken
	ide.setReasons("unsaved: app.go, other.go")
	expectForbidden(c.send(Request{Cmd: "close", Project: "ike", Force: live}), "outgrown grant")
	if r := c.send(Request{Cmd: "close", Project: "ike"}); r.Type != "blocked" || len(r.Reasons) != 1 || r.Reasons[0] != "unsaved: app.go, other.go" {
		t.Fatalf("the next plain close reports the current reasons: %+v", r)
	}
}

// TestServerCloseUnpairDropsForceToken: revoking the pairing drops the
// client's force token with it.
func TestServerCloseUnpairDropsForceToken(t *testing.T) {
	ide := &fakeIDE{root: "/home/dev/ike", project: "ike", reasons: []string{"tool sql"}}
	srv, _, c := closeServer(t, ide)
	live := c.send(Request{Cmd: "close"}).ForceToken
	if live == "" {
		t.Fatal("expected a blocked reply with a token")
	}
	clientID := srv.Store().Clients()[0].ID
	if r := c.send(Request{Cmd: "unpair"}); r.Type != "ok" {
		t.Fatalf("unpair %+v", r)
	}
	if _, ok := srv.force.Consume(clientID, live, time.Now()); ok {
		t.Fatal("unpair must forget the client's force token")
	}
}

// TestServerHelloProto: hello reports the protocol generation so a client
// can tell close is available.
func TestServerHelloProto(t *testing.T) {
	srv, _, _ := testServer(t)
	c := dial(t, srv)
	if r := c.send(Request{Cmd: "hello"}); r.Type != "hello" || r.Proto != ProtocolVersion || r.Proto < 3 {
		t.Fatalf("hello %+v", r)
	}
}
