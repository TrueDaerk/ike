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
	// Events receives pairing state changes (may be nil).
	Events Events
	// CodeTTL is the pairing code lifetime; 0 selects DefaultCodeTTL.
	CodeTTL time.Duration
}

// Server is one listening endpoint.
type Server struct {
	ln      net.Listener
	opts    Options
	pairing *Pairing
	store   *Store
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
		return Response{Type: "hello", Name: "ike", Version: s.opts.Version,
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
