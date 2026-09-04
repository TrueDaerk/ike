package netlink

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncRecorder is recorder behind a mutex — server events arrive on
// connection goroutines.
type syncRecorder struct {
	mu sync.Mutex
	recorder
}

func (r *syncRecorder) ChallengeIssued(c Challenge) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorder.ChallengeIssued(c)
}
func (r *syncRecorder) ChallengeCleared() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorder.ChallengeCleared()
}
func (r *syncRecorder) Paired(c Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorder.Paired(c)
}
func (r *syncRecorder) last() Challenge {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.issued[len(r.issued)-1]
}

// testServer starts a loopback server on a free port.
func testServer(t *testing.T) (*Server, *syncRecorder, *[]string) {
	t.Helper()
	rec := &syncRecorder{}
	var delivered []string
	var mu sync.Mutex
	store, _ := OpenStore(filepath.Join(t.TempDir(), "clients.json"))
	srv, err := Serve(Options{
		Addr:    "127.0.0.1:0",
		Store:   store,
		Version: "test",
		Events:  rec,
		Deliver: func(url string) { mu.Lock(); delivered = append(delivered, url); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv, rec, &delivered
}

// client is a line-protocol test client.
type client struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func dial(t *testing.T, srv *Server) *client {
	t.Helper()
	conn, err := net.DialTimeout("tcp", srv.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &client{t: t, conn: conn, r: bufio.NewReader(conn)}
}

// send writes one request and reads its response.
func (c *client) send(req Request) Response {
	c.t.Helper()
	data, _ := json.Marshal(req)
	return c.raw(string(data))
}

// raw writes one line verbatim and reads the response.
func (c *client) raw(line string) Response {
	c.t.Helper()
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.conn.Write([]byte(line + "\n")); err != nil {
		c.t.Fatal(err)
	}
	reply, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var resp Response
	if err := json.Unmarshal([]byte(reply), &resp); err != nil {
		c.t.Fatalf("bad response %q: %v", reply, err)
	}
	return resp
}

// TestServerPairThenOpen walks the whole happy path: hello (unpaired), an
// open attempt that turns into a challenge, the right code, the token, an
// open that reaches Deliver, and a second connection authenticating by
// token alone.
func TestServerPairThenOpen(t *testing.T) {
	srv, rec, delivered := testServer(t)
	c := dial(t, srv)

	hello := c.send(Request{Cmd: "hello"})
	if hello.Type != "hello" || hello.Version != "test" || hello.Authenticated == nil || *hello.Authenticated {
		t.Fatalf("hello %+v", hello)
	}
	// An unpaired open does not refuse — it starts pairing.
	ch := c.send(Request{Cmd: "open", Project: "ike", Client: "phone"})
	if ch.Type != "challenge" || ch.Reason != "new" || ch.Alphabet == nil || ch.ExpiresIn <= 0 {
		t.Fatalf("challenge %+v", ch)
	}
	if len(ch.Alphabet.Suits) != 4 || len(ch.Alphabet.Colours) != 4 || ch.Alphabet.Length != CodeLength {
		t.Fatalf("alphabet %+v", ch.Alphabet)
	}
	if len(*delivered) != 0 {
		t.Fatal("nothing may be delivered before pairing")
	}
	live := rec.last()
	if live.Client != "phone" {
		t.Fatalf("challenge client %q", live.Client)
	}
	paired := c.send(Request{Cmd: "pair", Code: live.Code[:]})
	if paired.Type != "paired" || paired.Token == "" || paired.ClientID == "" {
		t.Fatalf("paired %+v", paired)
	}
	if list := srv.Store().Clients(); len(list) != 1 || list[0].Name != "phone" {
		t.Fatalf("store %+v", list)
	}
	// The connection is now authenticated: open works without the token.
	ok := c.send(Request{Cmd: "open", Project: "ike", File: "main.go", Line: 12, Tool: "terminal"})
	if ok.Type != "ok" || !strings.HasPrefix(ok.Link, "ike://open?") {
		t.Fatalf("open %+v", ok)
	}
	if len(*delivered) != 1 || !strings.Contains((*delivered)[0], "file=main.go%3A12") {
		t.Fatalf("delivered %v", *delivered)
	}
	// A fresh connection authenticates with the token on the request.
	c2 := dial(t, srv)
	if r := c2.send(Request{Cmd: "auth", Token: paired.Token}); r.Type != "ok" {
		t.Fatalf("auth by token %+v", r)
	}
	if r := c2.send(Request{Cmd: "open", URL: "ike://open?project=ike"}); r.Type != "ok" {
		t.Fatalf("open on authed conn %+v", r)
	}
	// Bad links are refused with invalid_link, never delivered.
	if r := c2.send(Request{Cmd: "open", URL: "ike://open?project=../x"}); r.Type != "error" || r.Error != CodeInvalidLink {
		t.Fatalf("bad link %+v", r)
	}
	if r := c2.send(Request{Cmd: "open", Project: "ike", Remote: "git@github.com:a/b"}); r.Error != CodeInvalidLink {
		t.Fatalf("project+remote %+v", r)
	}
	if len(*delivered) != 2 {
		t.Fatalf("delivered %v", *delivered)
	}
	// unpair forgets the token.
	if r := c2.send(Request{Cmd: "unpair"}); r.Type != "ok" {
		t.Fatalf("unpair %+v", r)
	}
	c3 := dial(t, srv)
	if r := c3.send(Request{Cmd: "auth", Token: paired.Token}); r.Type != "error" || r.Error != CodeUnauthorized {
		t.Fatalf("revoked token %+v", r)
	}
}

// TestServerWrongCodeRegenerates: a miss answers with a new challenge whose
// reason is "wrong", after the penalty delay.
func TestServerWrongCodeRegenerates(t *testing.T) {
	srv, rec, _ := testServer(t)
	c := dial(t, srv)
	ch := c.send(Request{Cmd: "pair", Client: "laptop"})
	if ch.Type != "challenge" {
		t.Fatalf("challenge %+v", ch)
	}
	first := rec.last()
	started := time.Now()
	miss := wrongGuess(first.Code)
	again := c.send(Request{Cmd: "pair", Code: miss[:]})
	if again.Type != "challenge" || again.Reason != "wrong" {
		t.Fatalf("after a miss %+v", again)
	}
	if time.Since(started) < wrongDelayStep {
		t.Fatal("a miss must be answered after the penalty delay")
	}
	second := rec.last()
	if second.Code.Equal(first.Code) || second.Client != "laptop" {
		t.Fatalf("the code must change and keep the client name: %+v", second)
	}
	if r := c.send(Request{Cmd: "pair", CodeText: second.Code.String()}); r.Type != "paired" {
		t.Fatalf("text-form guess %+v", r)
	}
}

// TestServerRejectsGarbage: non-JSON, unknown commands, missing cmd, guesses
// without a live code, and guarded commands without a token.
func TestServerRejectsGarbage(t *testing.T) {
	srv, _, delivered := testServer(t)
	c := dial(t, srv)
	if r := c.raw("open ike://open?project=x"); r.Type != "error" || r.Error != CodeBadRequest {
		t.Fatalf("plain text %+v", r)
	}
	if r := c.send(Request{Cmd: "frobnicate"}); r.Error != CodeBadRequest {
		t.Fatalf("unknown cmd %+v", r)
	}
	if r := c.raw(`{}`); r.Error != CodeBadRequest {
		t.Fatalf("missing cmd %+v", r)
	}
	if r := c.send(Request{Cmd: "pair", CodeText: "spade:red spade:red spade:red spade:red spade:red spade:red"}); r.Error != CodeNoChallenge {
		t.Fatalf("guess without challenge %+v", r)
	}
	if r := c.send(Request{Cmd: "unpair"}); r.Error != CodeUnauthorized {
		t.Fatalf("unpair unauthenticated %+v", r)
	}
	if r := c.send(Request{Cmd: "ping"}); r.Type != "ok" {
		t.Fatalf("ping %+v", r)
	}
	if len(*delivered) != 0 {
		t.Fatal("garbage must never deliver")
	}
	// An oversized line ends the connection with too_large.
	huge := dial(t, srv)
	if r := huge.raw(`{"cmd":"ping","client":"` + strings.Repeat("x", maxLineLen) + `"}`); r.Error != CodeTooLarge {
		t.Fatalf("oversized %+v", r)
	}
}

// TestServerUnterminatedLastLine: a client that closes its write side
// without a trailing newline still gets its answer.
func TestServerUnterminatedLastLine(t *testing.T) {
	srv, _, _ := testServer(t)
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"cmd":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	_ = conn.(*net.TCPConn).CloseWrite()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(reply, "pong") {
		t.Fatalf("reply %q err %v", reply, err)
	}
}

// TestLinkFromRequest: the parts assemble into a strict, parseable link.
func TestLinkFromRequest(t *testing.T) {
	link, err := LinkFromRequest(Request{Project: "ike", File: "a/b.go:7", Line: 9, Tool: "vcs"})
	if err != nil || !strings.Contains(link, "file=a%2Fb.go%3A7") || !strings.Contains(link, "tool=vcs") {
		t.Fatalf("%q %v", link, err)
	}
	if _, err := LinkFromRequest(Request{}); err == nil {
		t.Fatal("an empty request must fail")
	}
	if _, err := LinkFromRequest(Request{URL: "https://example.com"}); err == nil {
		t.Fatal("a non-ike URL must fail")
	}
	if _, err := LinkFromRequest(Request{Remote: "not a remote"}); err == nil {
		t.Fatal("an unparseable remote must fail")
	}
}
