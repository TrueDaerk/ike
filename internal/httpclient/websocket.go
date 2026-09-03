package httpclient

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"ike/internal/httpfile"
)

// websocket.go dispatches `WEBSOCKET <url>` blocks (#2422): the connection
// opens, the block's initial messages go out (pausing at `=== wait-for-server`
// until a server message arrives), and every frame — sent and received — is
// appended to a live transcript that travels the existing streaming path: one
// line per frame with a direction marker, a timestamp, and the payload (hex
// for binary frames). The session stays open until the context is canceled
// (http.cancel / x in the pane), the server closes, or the transport fails;
// the finished transcript becomes the Response body, so history, showResponse
// and the diff commands all work on it unchanged.

// WSCallbacks extends the streaming callbacks with the session handle (#2422).
type WSCallbacks struct {
	StreamCallbacks
	// OnSession fires once the connection is open, handing out the handle the
	// interactive input line sends through. Runs on the dispatch goroutine —
	// implementations hand the session off rather than block.
	OnSession func(*WSSession)
}

// WSSession is one open WebSocket connection (#2422). Send is safe to call
// from any goroutine; the transcript lines of sent messages travel the same
// chunk callback as received frames.
type WSSession struct {
	conn *websocket.Conn
	now  func() time.Time

	// writeMu serializes writers: the initial-message loop and interactive
	// sends may overlap.
	writeMu sync.Mutex

	// emitMu serializes transcript appends (sink + chunk callback), which both
	// the reader goroutine and senders reach.
	emitMu sync.Mutex
	sink   *bodySink
	chunk  func([]byte)

	// frames counts every transcript frame, sent and received (#2422): the
	// flight-end telemetry event carries it.
	frames atomic.Int64
	// received counts server frames, for the wait-for-server gate.
	received atomic.Int64
	// notify wakes wait-for-server waiters; the counter is the truth, the
	// channel only the wakeup.
	notify chan struct{}
	// closed marks a session whose connection is gone; Send then fails fast.
	closed atomic.Bool
}

// Frames reports how many frames crossed the session so far.
func (s *WSSession) Frames() int { return int(s.frames.Load()) }

// emit appends one transcript line and hands it to the live view.
func (s *WSSession) emit(line string) {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	data := []byte(line + "\n")
	kept := s.sink.add(data)
	if kept > 0 && s.chunk != nil {
		s.chunk(data[:kept])
	}
}

// Send writes one text message to the server (#2422) and appends its `→` line
// to the transcript. It fails once the session has closed.
func (s *WSSession) Send(text string) error {
	if s.closed.Load() {
		return fmt.Errorf("websocket session is closed")
	}
	s.writeMu.Lock()
	err := s.conn.WriteMessage(websocket.TextMessage, []byte(text))
	s.writeMu.Unlock()
	if err != nil {
		return err
	}
	s.frames.Add(1)
	s.emit(wsFrameLine('→', s.now(), []byte(text), false))
	return nil
}

// Closed reports whether the session's connection is gone (#2422): the pane's
// input line asks before offering to send.
func (s *WSSession) Closed() bool { return s.closed.Load() }

// wsFrameLine renders one transcript line: direction marker, timestamp, and
// the payload — text verbatim (newlines flattened so one frame stays one row),
// binary as a length note plus hex.
func wsFrameLine(dir rune, at time.Time, payload []byte, binary bool) string {
	stamp := at.Format("15:04:05.000")
	if binary {
		return fmt.Sprintf("%c %s (binary %d bytes) %s", dir, stamp, len(payload), hex.EncodeToString(payload))
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(payload), "\r\n", "\n"), "\n", "\\n")
	return fmt.Sprintf("%c %s %s", dir, stamp, text)
}

// wsURL normalizes a resolved target into the ws/wss form the dialer takes:
// http(s) schemes map over, an empty scheme defaults to ws.
func wsURL(target string) (string, error) {
	u, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("invalid target %q: %v", target, err)
	}
	switch u.Scheme {
	case "ws", "wss":
	case "http", "":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("target %q: scheme %q is not a websocket scheme", target, u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("target %q has no host", target)
	}
	return u.String(), nil
}

// wsHandshake splits a resolved request's headers into what the dialer takes:
// the plain header set, the subprotocols (Sec-WebSocket-Protocol becomes the
// dialer's own field — gorilla rejects it as a manual header), and warnings
// for the reserved Sec-WebSocket-* fields the handshake owns.
func wsHandshake(resolved *httpfile.Request) (http.Header, []string, []string) {
	header := http.Header{}
	var subprotocols, warnings []string
	for _, h := range resolved.Headers {
		switch strings.ToLower(h.Name) {
		case "sec-websocket-protocol":
			for _, p := range strings.Split(h.Value, ",") {
				if p = strings.TrimSpace(p); p != "" {
					subprotocols = append(subprotocols, p)
				}
			}
		case "sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions", "connection", "upgrade":
			warnings = append(warnings, fmt.Sprintf("header %s is set by the websocket handshake — ignored", h.Name))
		default:
			header.Add(h.Name, h.Value)
		}
	}
	return header, subprotocols, warnings
}

// DispatchWS opens the WebSocket session of a resolved WEBSOCKET block
// (#2422): placeholders resolve exactly as for HTTP, the handshake response
// reaches cb.OnHeaders, the session handle reaches cb.OnSession, the initial
// messages are sent (pausing at wait-for-server), and every frame streams into
// cb.OnChunk as one transcript line. The call blocks until the session ends —
// ctx cancel, server close, or transport failure — and returns the transcript
// as the Response. Like a canceled stream (#1776), a canceled session is a
// regular result, not an error.
func DispatchWS(ctx context.Context, req *httpfile.Request, opts Options, cb WSCallbacks) (*Response, error) {
	vars := variables(opts)
	resolved, err := req.ResolveVars(vars)
	if err != nil {
		return nil, err
	}
	messages := []httpfile.WSMessage(nil)
	if resolved.WebSocket != nil {
		messages = resolved.WebSocket.Messages
	}
	snap := &RequestSnapshot{
		Method:  httpfile.WebSocketMethod,
		URL:     resolved.Target,
		Headers: http.Header{},
		Body:    []byte(httpfile.JoinWebSocketBody(messages)),
	}
	for _, h := range resolved.Headers {
		snap.Headers.Add(h.Name, h.Value)
	}
	return dispatchWS(ctx, req.Key(), resolved.Target, resolved, messages, snap, opts, cb)
}

// ResendWS replays a stored WEBSOCKET session snapshot (#2422): the connection
// re-opens with the stored URL and headers, and the stored initial messages —
// already resolved when they were first sent — replay verbatim, wait-for-server
// gates included.
func ResendWS(ctx context.Context, key string, snap *RequestSnapshot, opts Options, cb WSCallbacks) (*Response, error) {
	if snap == nil {
		return nil, fmt.Errorf("request %s: no stored request to re-send", key)
	}
	resolved := &httpfile.Request{Method: httpfile.WebSocketMethod, Target: snap.URL}
	for name, values := range snap.Headers {
		for _, v := range values {
			resolved.Headers = append(resolved.Headers, httpfile.Header{Name: name, Value: v})
		}
	}
	messages := httpfile.SplitWebSocketBody(string(snap.Body))
	return dispatchWS(ctx, key, snap.URL, resolved, messages, snap.Clone(), opts, cb)
}

// dispatchWS is the shared session body of DispatchWS and ResendWS.
func dispatchWS(ctx context.Context, key, target string, resolved *httpfile.Request,
	messages []httpfile.WSMessage, snap *RequestSnapshot, opts Options, cb WSCallbacks) (*Response, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	dialURL, err := wsURL(target)
	if err != nil {
		return nil, fmt.Errorf("request %s: %v", key, err)
	}
	header, subprotocols, warnings := wsHandshake(resolved)

	timeout := DefaultTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}
	cfg := &curlConfig{}
	if !opts.DisableConfig {
		path := opts.CurlrcPath
		if path == "" {
			path = curlrcPath()
		}
		cfg = parseCurlrc(path)
	}
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: timeout,
		Subprotocols:     subprotocols,
	}
	if cfg.Insecure {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	start := now()
	conn, handshake, err := dialer.DialContext(ctx, dialURL, header)
	if err != nil {
		if handshake != nil {
			// The server answered but refused the upgrade — the status says why.
			return nil, fmt.Errorf("request %s: websocket handshake failed: %s", key, handshake.Status)
		}
		return nil, fmt.Errorf("request %s: %v", key, err)
	}
	defer conn.Close()
	if cb.OnHeaders != nil {
		cb.OnHeaders(handshake.Status, handshake.StatusCode, handshake.Proto, handshake.Header)
	}

	session := &WSSession{
		conn:   conn,
		now:    now,
		sink:   newBodySink(SpoolThreshold, MaxBodyBytes, ".txt"),
		chunk:  cb.OnChunk,
		notify: make(chan struct{}, 1),
	}
	if cb.OnSession != nil {
		cb.OnSession(session)
	}

	// The context is the session's off switch (#2422): http.cancel and the
	// pane's x land here, closing the connection out from under the reader.
	stop := context.AfterFunc(ctx, func() {
		session.closed.Store(true)
		conn.Close()
	})
	defer stop()

	// The reader is the session's heartbeat: every server frame lands in the
	// transcript, wakes wait-for-server, and counts. It ends the session.
	readErr := make(chan error, 1)
	go func() {
		for {
			kind, payload, err := conn.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
			session.frames.Add(1)
			session.received.Add(1)
			session.emit(wsFrameLine('←', now(), payload, kind == websocket.BinaryMessage))
			select {
			case session.notify <- struct{}{}:
			default:
			}
		}
	}()

	// The initial messages, in file order; a wait-for-server gate pauses until
	// the server has said something since the previous send.
	sendErr := func() error {
		seen := int64(0)
		for _, msg := range messages {
			if msg.WaitForServer {
				for session.received.Load() <= seen {
					select {
					case <-session.notify:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			if err := session.Send(msg.Text); err != nil {
				return err
			}
			seen = session.received.Load()
		}
		return nil
	}()
	if sendErr != nil && ctx.Err() == nil && !session.closed.Load() {
		warnings = append(warnings, fmt.Sprintf("sending initial message failed (%v)", sendErr))
	}

	// The session runs until the reader stops: server close, transport
	// failure, or the canceled context closing the connection.
	err = <-readErr
	session.closed.Store(true)
	end := now()
	switch {
	case ctx.Err() != nil:
		warnings = append(warnings, "websocket session closed — showing the transcript")
	case websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway):
		warnings = append(warnings, "server closed the websocket session")
	case err != nil:
		warnings = append(warnings, fmt.Sprintf("websocket session ended (%v)", err))
	}

	body, spool, total := session.sink.close()
	warnings = append(warnings, session.sink.warnings()...)
	return &Response{
		Status:     handshake.Status,
		StatusCode: handshake.StatusCode,
		Proto:      handshake.Proto,
		Headers:    handshake.Header,
		Body:       body,
		SpoolPath:  spool,
		BodySize:   total,
		Truncated:  session.sink.truncated,
		Duration:   end.Sub(start),
		RequestKey: key,
		Warnings:   warnings,
		Frames:     session.Frames(),
		Request:    snap,
	}, nil
}
