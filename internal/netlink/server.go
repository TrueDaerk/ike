package netlink

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"ike/internal/deeplink"
)

// server.go is the TCP endpoint: accept loop, per-connection request loop,
// command dispatch. A connection may stay open and send any number of
// requests; each is answered with exactly one response line.
//
// Trust: everything on the wire is attacker input. Lines are capped, idle
// connections are cut, guarded commands need a paired token, and an accepted
// link is only ever a string the IDE re-parses through the same strict
// grammar an OS click goes through.

const (
	// maxLineLen caps one request line; a legitimate request is well under a
	// kilobyte.
	maxLineLen = 16 * 1024
	// idleTimeout cuts a connection that stays silent this long.
	idleTimeout = 5 * time.Minute
	// maxConns bounds simultaneous connections — a small tool, not a web
	// server.
	maxConns = 32
)

// Options configures Serve.
type Options struct {
	// Addr is the listen address, "host:port".
	Addr string
	// Store holds the paired clients; nil selects an in-memory store.
	Store *Store
	// Version is reported by hello.
	Version string
	// Deliver hands one validated ike:// URL to the IDE. It runs on the
	// connection goroutine and must not block.
	Deliver func(url string)
	// State reports what the IDE currently shows, for the status command
	// (#2529). It runs on the connection goroutine — concurrently with the
	// update loop — so it must be safe to call from any goroutine and must
	// not block; the IDE side answers from a snapshot. nil means "no state
	// available": status is then answered with unavailable.
	State func() Status
	// Close asks the IDE to close a project (#2703). It runs on the
	// connection goroutine and must not block: the IDE side posts the
	// request into its update loop — where the busy guard lives — and calls
	// reply exactly once from there. The server waits CloseTimeout for it
	// and answers unavailable otherwise. nil means "this IKE cannot close
	// projects over the wire": close is then answered with unavailable.
	Close func(req CloseRequest, reply func(CloseResult))
	// Events receives pairing state changes (may be nil).
	Events Events
	// CodeTTL is the pairing code lifetime; 0 selects DefaultCodeTTL.
	CodeTTL time.Duration
	// CloseTimeout bounds the wait for the IDE's close verdict; 0 selects
	// DefaultCloseTimeout.
	CloseTimeout time.Duration
}

// DefaultCloseTimeout is how long a close request waits for the update
// loop before it is answered unavailable — a guard evaluation is a few
// string compares, so an IDE that takes longer is wedged, not busy.
const DefaultCloseTimeout = 5 * time.Second

// Server is one listening endpoint.
type Server struct {
	ln      net.Listener
	opts    Options
	pairing *Pairing
	store   *Store
	force   ForceTokens
	now     func() time.Time

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	done  chan struct{}
}

// Serve starts listening on opts.Addr.
func Serve(opts Options) (*Server, error) {
	if opts.Deliver == nil {
		return nil, errors.New("netlink: Deliver is required")
	}
	store := opts.Store
	if store == nil {
		store = &Store{}
	}
	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return nil, err
	}
	s := &Server{
		ln:      ln,
		opts:    opts,
		pairing: NewPairing(opts.CodeTTL, opts.Events),
		store:   store,
		now:     time.Now,
		conns:   map[net.Conn]struct{}{},
		done:    make(chan struct{}),
	}
	go s.accept()
	return s, nil
}

// Addr is the bound address (useful when the port was 0).
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Pairing exposes the state machine for the popup (cancel, current, expire).
func (s *Server) Pairing() *Pairing { return s.pairing }

// Store exposes the client store (listing and revoking from the UI).
func (s *Server) Store() *Store { return s.store }

// Close stops accepting and drops every open connection.
func (s *Server) Close() {
	if s == nil {
		return
	}
	_ = s.ln.Close()
	s.mu.Lock()
	select {
	case <-s.done:
	default:
		close(s.done) // wakes connections sleeping out a wrong-guess penalty
	}
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
}

// accept runs until the listener closes.
func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if len(s.conns) >= maxConns {
			s.mu.Unlock()
			writeResponse(conn, errorResponse(CodeInternal, "too many connections"))
			_ = conn.Close()
			continue
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go s.handle(conn)
	}
}

// session is one connection's state.
type session struct {
	conn   net.Conn
	addr   string
	client Client
	authed bool
}

// handle runs the request loop of one connection.
func (s *Server) handle(conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()
	sess := &session{conn: conn, addr: conn.RemoteAddr().String()}
	r := bufio.NewReaderSize(conn, 4096)
	for {
		_ = conn.SetDeadline(s.now().Add(idleTimeout))
		line, err := readLine(r, maxLineLen)
		if errors.Is(err, errLineTooLong) {
			writeResponse(conn, errorResponse(CodeTooLarge, "request line too long"))
			return
		}
		if err != nil {
			return
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			writeResponse(conn, errorResponse(CodeBadRequest, "not a JSON object: "+err.Error()))
			continue
		}
		resp, delay := s.dispatch(sess, req)
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-s.done:
			}
		}
		if !writeResponse(conn, resp) {
			return
		}
	}
}

// dispatch answers one request; delay is a penalty wait before the answer
// goes out (wrong pairing guesses).
func (s *Server) dispatch(sess *session, req Request) (Response, time.Duration) {
	// A token may ride on any request; a valid one authenticates the
	// connection for good.
	if req.Token != "" && !sess.authed {
		if c, ok := s.store.Verify(req.Token, s.now()); ok {
			sess.client, sess.authed = c, true
		}
	}
	switch strings.ToLower(strings.TrimSpace(req.Cmd)) {
	case "hello":
		authed := sess.authed
		return Response{Type: "hello", Name: "ike", Version: s.opts.Version, Proto: ProtocolVersion,
			Authenticated: &authed, Message: helloMessage(authed)}, 0
	case "ping":
		return Response{Type: "ok", Message: "pong"}, 0
	case "auth":
		if sess.authed {
			return Response{Type: "ok", Message: "authenticated as " + sess.client.Name}, 0
		}
		return errorResponse(CodeUnauthorized, "unknown token — pair first"), 0
	case "pair":
		return s.pair(sess, req)
	case "unpair":
		if !sess.authed {
			return errorResponse(CodeUnauthorized, "not paired"), 0
		}
		if _, err := s.store.Revoke(sess.client.ID); err != nil {
			return errorResponse(CodeInternal, err.Error()), 0
		}
		s.force.Forget(sess.client.ID)
		sess.authed, sess.client = false, Client{}
		return Response{Type: "ok", Message: "token revoked"}, 0
	case "open":
		if !sess.authed {
			// The first contact from an unpaired device: rather than a bare
			// refusal, start pairing right away so the client can show its
			// code UI at once.
			return s.challengeFor(sess, req.Client)
		}
		link, err := LinkFromRequest(req)
		if err != nil {
			return errorResponse(CodeInvalidLink, err.Error()), 0
		}
		s.opts.Deliver(link)
		return Response{Type: "ok", Link: link, Message: "link handed to IKE"}, 0
	case "status":
		// Unlike open, status never starts pairing implicitly: a device that
		// cannot open anything has no business making a popup appear either.
		if !sess.authed {
			return errorResponse(CodeUnauthorized, "status needs a paired token"), 0
		}
		if s.opts.State == nil {
			return errorResponse(CodeUnavailable, "this IKE does not report its state"), 0
		}
		st := s.opts.State()
		if strings.TrimSpace(st.Root) == "" {
			return errorResponse(CodeUnavailable, "no project is open"), 0
		}
		return statusResponse(st), 0
	case "close":
		// Guarded like status: an unpaired asker is refused without a
		// pairing popup — a device that cannot open anything has even less
		// business closing something.
		if !sess.authed {
			return errorResponse(CodeUnauthorized, "close needs a paired token"), 0
		}
		return s.closeProject(sess, req), 0
	case "":
		return errorResponse(CodeBadRequest, "missing cmd"), 0
	default:
		return errorResponse(CodeBadRequest, "unknown cmd "+req.Cmd), 0
	}
}

// pair handles the two halves of pairing: a request without a code asks
// for a challenge; one with a code is a guess.
func (s *Server) pair(sess *session, req Request) (Response, time.Duration) {
	text := strings.TrimSpace(req.Code)
	if text == "" {
		text = strings.TrimSpace(req.CodeText)
	}
	if text == "" {
		return s.challengeFor(sess, req.Client)
	}
	guess, err := ParseCode(text)
	if err != nil {
		return errorResponse(CodeBadRequest, err.Error()), 0
	}
	// The device name given when the code was requested carries over to the
	// guess, so a client need not repeat it.
	name := strings.TrimSpace(req.Client)
	if cur, ok := s.pairing.Current(); ok && name == "" {
		name = cur.Client
	}
	if name == "" {
		name = hostOf(sess.addr)
	}
	verdict, next, delay := s.pairing.Attempt(sess.addr, guess)
	switch verdict {
	case VerdictOK:
		token, c, err := s.store.Issue(name, sess.addr, s.now())
		if err != nil {
			return errorResponse(CodeInternal, "cannot store the pairing: "+err.Error()), 0
		}
		sess.client, sess.authed = c, true
		if s.opts.Events != nil {
			s.opts.Events.Paired(c)
		}
		return Response{Type: "paired", Token: token, ClientID: c.ID,
			Message: "paired — send this token with every request"}, 0
	case VerdictWrong, VerdictExpired:
		return challengeResponse(next, s.now()), delay
	case VerdictBlocked:
		return errorResponse(CodeBlocked, "too many wrong codes — try again later"), 0
	default: // VerdictNone
		return errorResponse(CodeNoChallenge, "no code is being shown — send pair without a code first"), 0
	}
}

// closeProject handles close (#2703) for an authenticated session: the
// target is resolved and guarded by the IDE on its update loop; a busy
// workspace comes back as blocked with the guard's reasons and a one-time
// force token, and a second close echoing that token discards the listed
// activity — provided the token is the client's live one and the activity
// is still exactly what the client was shown.
func (s *Server) closeProject(sess *session, req Request) Response {
	if s.opts.Close == nil {
		return errorResponse(CodeUnavailable, "this IKE does not close projects over the network")
	}
	ask := CloseRequest{Project: strings.TrimSpace(req.Project), Client: sess.client}
	if remote := strings.TrimSpace(req.Remote); remote != "" {
		key, ok := deeplink.NormalizeRemote(remote)
		if !ok {
			return errorResponse(CodeBadRequest, "remote is not a git remote spelling")
		}
		ask.Remote = key
	}
	if ask.Project != "" && ask.Remote != "" {
		return errorResponse(CodeBadRequest, "close takes project or remote, not both")
	}
	if force := strings.TrimSpace(req.Force); force != "" {
		// The token is consumed here whatever the IDE says next: right or
		// wrong, it is spent, and the next plain close mints a fresh one.
		grant, ok := s.force.Consume(sess.client.ID, force, s.now())
		if !ok {
			return forbiddenResponse(ReasonStaleForceToken, "the force token is wrong, expired or already used — send close again for a fresh one")
		}
		ask.Force, ask.Grant = true, grant
	}
	res, ok := s.askClose(ask)
	if !ok {
		return errorResponse(CodeUnavailable, "IKE did not answer the close in time")
	}
	switch res.Outcome {
	case CloseClosed:
		return Response{Type: "ok", Project: res.Project, Message: "closed " + res.Project}
	case CloseBlocked:
		grant := ForceGrant{Root: res.Root, Project: res.Project, Reasons: res.Reasons}
		token, left, err := s.force.Issue(sess.client.ID, grant, s.now())
		if err != nil {
			return errorResponse(CodeInternal, "cannot mint a force token: "+err.Error())
		}
		reasons := res.Reasons
		if reasons == nil {
			reasons = []string{}
		}
		return Response{Type: "blocked", Project: res.Project, Reasons: reasons,
			ForceToken: token, ExpiresIn: left,
			Message: "still busy — send close again with force set to the token to discard the listed activity"}
	case CloseUnknown:
		return errorResponse(CodeUnavailable, "no open project matches the request")
	case CloseStale:
		return forbiddenResponse(ReasonStaleForceToken, "the activity changed since the blocked reply — send close again for the current reasons")
	default:
		msg := res.Message
		if msg == "" {
			msg = "IKE cannot close a project right now"
		}
		return errorResponse(CodeUnavailable, msg)
	}
}

// askClose posts req to the IDE and waits for its verdict; ok is false on
// timeout (or a server shutting down meanwhile).
func (s *Server) askClose(req CloseRequest) (CloseResult, bool) {
	timeout := s.opts.CloseTimeout
	if timeout <= 0 {
		timeout = DefaultCloseTimeout
	}
	// Buffered so a reply landing after the timeout never blocks the update
	// loop on a reader that gave up.
	replies := make(chan CloseResult, 1)
	var once sync.Once
	s.opts.Close(req, func(r CloseResult) { once.Do(func() { replies <- r }) })
	select {
	case r := <-replies:
		return r, true
	case <-time.After(timeout):
		return CloseResult{}, false
	case <-s.done:
		return CloseResult{}, false
	}
}

// forbiddenResponse builds a type=forbidden response: the request was well
// formed and the asker is paired, but this particular action is refused.
func forbiddenResponse(reason, msg string) Response {
	return Response{Type: "forbidden", Reason: reason, Message: msg}
}

// challengeFor issues a fresh code for the session's address.
func (s *Server) challengeFor(sess *session, client string) (Response, time.Duration) {
	c, err := s.pairing.Begin(strings.TrimSpace(client), sess.addr)
	if errors.Is(err, ErrBlocked) {
		return errorResponse(CodeBlocked, "pairing refused for now — try again later"), 0
	}
	return challengeResponse(c, s.now()), 0
}

// helloMessage tells a fresh client what to do next.
func helloMessage(authed bool) string {
	if authed {
		return "authenticated — send commands"
	}
	return "not paired — send {\"cmd\":\"pair\"} to get a code"
}

// errLineTooLong marks a request beyond maxLineLen.
var errLineTooLong = errors.New("line too long")

// readLine reads one newline-terminated line, refusing lines beyond limit
// bytes so a hostile peer cannot make the reader buffer without bound.
func readLine(r *bufio.Reader, limit int) (string, error) {
	var b strings.Builder
	for {
		chunk, err := r.ReadSlice('\n')
		b.Write(chunk)
		if b.Len() > limit {
			return "", errLineTooLong
		}
		if err == nil {
			return b.String(), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			// A final unterminated line still parses (nc without a trailing
			// newline); any other failure ends the connection.
			if b.Len() > 0 && errors.Is(err, io.EOF) {
				return b.String(), nil
			}
			return "", err
		}
	}
}

// writeResponse writes one response line; false when the peer is gone.
func writeResponse(conn net.Conn, resp Response) bool {
	data, err := json.Marshal(resp)
	if err != nil {
		data = []byte(fmt.Sprintf(`{"type":"error","error":%q,"message":"cannot encode response"}`, CodeInternal))
	}
	_, err = conn.Write(append(data, '\n'))
	return err == nil
}
