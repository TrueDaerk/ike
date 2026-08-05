package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// pipePair builds two duplex streams wired head-to-tail, modelling a client and
// its server peer over stdio.
type rwc struct {
	io.Reader
	io.Writer
}

func (r rwc) Close() error { return nil }

func newPipePair() (client io.ReadWriteCloser, server io.ReadWriteCloser) {
	cr, sw := io.Pipe() // server writes -> client reads
	sr, cw := io.Pipe() // client writes -> server reads
	return rwc{Reader: cr, Writer: cw}, rwc{Reader: sr, Writer: sw}
}

func TestFramingRoundTrip(t *testing.T) {
	cli, srv := newPipePair()
	payload := []byte(`{"jsonrpc":"2.0","method":"ping"}`)
	go func() {
		_ = writeFrame(cli, payload)
	}()
	got, err := readFrame(bufio.NewReader(srv))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("round trip = %q, want %q", got, payload)
	}
}

func TestCallReceivesResponse(t *testing.T) {
	cli, srv := newPipePair()
	conn := NewConn(cli, Handler{})
	defer conn.Close()

	// Scripted server: read one request, echo a result mirroring its id.
	go func() {
		r := bufio.NewReader(srv)
		payload, err := readFrame(r)
		if err != nil {
			return
		}
		var req message
		_ = json.Unmarshal(payload, &req)
		resp, _ := json.Marshal(message{JSONRPC: version, ID: req.ID, Result: json.RawMessage(`{"ok":true}`)})
		_ = writeFrame(srv, resp)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := conn.Call(ctx, "initialize", map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"ok":true}` {
		t.Fatalf("result = %q", raw)
	}
}

func TestCallReceivesError(t *testing.T) {
	cli, srv := newPipePair()
	conn := NewConn(cli, Handler{})
	defer conn.Close()

	go func() {
		r := bufio.NewReader(srv)
		payload, _ := readFrame(r)
		var req message
		_ = json.Unmarshal(payload, &req)
		resp, _ := json.Marshal(message{JSONRPC: version, ID: req.ID, Error: &Error{Code: CodeMethodNotFound, Message: "nope"}})
		_ = writeFrame(srv, resp)
	}()

	_, err := conn.Call(context.Background(), "bogus", nil)
	var rpcErr *Error
	if err == nil {
		t.Fatal("expected an error")
	}
	if e, ok := err.(*Error); ok {
		rpcErr = e
	}
	if rpcErr == nil || rpcErr.Code != CodeMethodNotFound {
		t.Fatalf("err = %v, want method-not-found", err)
	}
}

func TestNotificationDispatched(t *testing.T) {
	cli, srv := newPipePair()
	got := make(chan string, 1)
	conn := NewConn(cli, Handler{
		Notify: func(method string, params json.RawMessage) { got <- method },
	})
	defer conn.Close()

	// Server pushes a notification (no id).
	notif, _ := json.Marshal(message{JSONRPC: version, Method: "textDocument/publishDiagnostics", Params: json.RawMessage(`{}`)})
	go func() { _ = writeFrame(srv, notif) }()

	select {
	case m := <-got:
		if m != "textDocument/publishDiagnostics" {
			t.Fatalf("method = %q", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification not dispatched")
	}
}

func TestServerRequestDispatched(t *testing.T) {
	cli, srv := newPipePair()
	type call struct {
		id     ID
		method string
	}
	got := make(chan call, 1)
	conn := NewConn(cli, Handler{
		Request: func(id ID, method string, params json.RawMessage) { got <- call{id, method} },
	})
	defer conn.Close()

	req, _ := json.Marshal(message{JSONRPC: version, ID: &ID{Num: 42}, Method: "workspace/configuration", Params: json.RawMessage(`{}`)})
	go func() { _ = writeFrame(srv, req) }()

	select {
	case c := <-got:
		if c.method != "workspace/configuration" || c.id.Num != 42 {
			t.Fatalf("got %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server request not dispatched")
	}
}

func TestRespondNilResultSerializesNull(t *testing.T) {
	cli, srv := newPipePair()
	conn := NewConn(cli, Handler{})
	defer conn.Close()

	frames := make(chan []byte, 1)
	go func() {
		r := bufio.NewReader(srv)
		payload, err := readFrame(r)
		if err != nil {
			return
		}
		frames <- payload
	}()

	if err := conn.Respond(ID{Num: 1}, nil, nil); err != nil {
		t.Fatal(err)
	}

	select {
	case payload := <-frames:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil {
			t.Fatal(err)
		}
		// A response must carry "result" or "error"; a nil payload has to
		// serialize as an explicit null (#991).
		raw, ok := fields["result"]
		if !ok {
			t.Fatalf("response %s has no result property", payload)
		}
		if string(raw) != "null" {
			t.Fatalf("result = %s, want null", raw)
		}
		if _, ok := fields["error"]; ok {
			t.Fatalf("response %s unexpectedly has an error property", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("response never written")
	}
}

func TestCallFailsAfterClose(t *testing.T) {
	cli, _ := newPipePair()
	conn := NewConn(cli, Handler{})
	_ = conn.Close()
	if _, err := conn.Call(context.Background(), "x", nil); err == nil {
		t.Fatal("expected error after close")
	}
}

// TestNotifyDoesNotBlockOnStalledServer models a server that never drains its
// stdin (io.Pipe writes block until read): the sole writer goroutine stalls on
// the first frame, but callers must still enqueue-and-return. This is the #594
// guarantee — a busy language server can no longer freeze the caller (the
// bubbletea Update goroutine sends didChange from here per keystroke).
func TestNotifyDoesNotBlockOnStalledServer(t *testing.T) {
	cli, _ := newPipePair() // the server end is never read from
	conn := NewConn(cli, Handler{})
	defer conn.Close()

	done := make(chan error, 1)
	go func() {
		done <- conn.Notify("textDocument/didChange", map[string]any{"text": "hello"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Notify err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked on a stalled server stdin")
	}

	// A burst of further notifications also never blocks the caller, even though
	// the writer goroutine is still stalled on the very first frame.
	for i := 0; i < 200; i++ {
		if err := conn.Notify("textDocument/didChange", map[string]any{"n": i}); err != nil {
			t.Fatalf("Notify %d err = %v", i, err)
		}
	}
}

func TestNotifyReturnsErrClosedAfterClose(t *testing.T) {
	cli, _ := newPipePair()
	conn := NewConn(cli, Handler{})
	_ = conn.Close()
	if err := conn.Notify("textDocument/didChange", nil); err != ErrClosed {
		t.Fatalf("Notify after close = %v, want ErrClosed", err)
	}
}

func TestIDMarshalRoundTrip(t *testing.T) {
	for _, id := range []ID{{Num: 7}, {Str: "abc", IsStr: true}} {
		b, err := id.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var back ID
		if err := back.UnmarshalJSON(b); err != nil {
			t.Fatal(err)
		}
		if back != id {
			t.Fatalf("round trip %v -> %v", id, back)
		}
	}
}

func TestCloseReleasesQueuedFrames(t *testing.T) {
	cli, _ := newPipePair() // the server end is never read from
	conn := NewConn(cli, Handler{})
	// Stall the writer on the first frame, then queue more behind it.
	for i := 0; i < 50; i++ {
		if err := conn.Notify("textDocument/didChange", map[string]any{"n": i}); err != nil {
			t.Fatalf("Notify %d err = %v", i, err)
		}
	}
	_ = conn.Close()
	// Queued frames are undeliverable once the stream is gone; keeping them
	// pins megabytes of full-sync payloads for as long as the Conn lives (#1537).
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn.sendMu.Lock()
		empty := conn.sendBuf == nil
		conn.sendMu.Unlock()
		if empty {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("sendBuf still populated after Close")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestNotifyCoalescedReplacesQueuedFrame (#1542): while the writer is stalled,
// notifications with the same key replace the queued payload in place instead
// of growing the queue; distinct keys queue separately.
func TestNotifyCoalescedReplacesQueuedFrame(t *testing.T) {
	cli, _ := newPipePair() // the server end is never read from
	conn := NewConn(cli, Handler{})
	defer conn.Close()
	// First frame stalls in writeFrame (io.Pipe blocks until read); everything
	// after it stays in sendBuf where coalescing can observe it.
	if err := conn.Notify("stall", nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := conn.NotifyCoalesced("didChange:a", "textDocument/didChange", map[string]any{"n": i}); err != nil {
			t.Fatalf("NotifyCoalesced %d err = %v", i, err)
		}
	}
	if err := conn.NotifyCoalesced("didChange:b", "textDocument/didChange", map[string]any{"other": true}); err != nil {
		t.Fatal(err)
	}
	waitQueue := func(cond func(n int) bool) int {
		deadline := time.Now().Add(2 * time.Second)
		for {
			conn.sendMu.Lock()
			n := len(conn.sendBuf)
			conn.sendMu.Unlock()
			if cond(n) || time.Now().After(deadline) {
				return n
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	// At most: the stall frame may still be in sendBuf or already handed to the
	// writer, so the queue holds 2 or 3 frames — never ~101.
	if n := waitQueue(func(n int) bool { return n <= 3 }); n > 3 {
		t.Fatalf("queue holds %d frames, coalescing must keep it at the key count", n)
	}
	conn.sendMu.Lock()
	var last []byte
	for _, f := range conn.sendBuf {
		if f.key == "didChange:a" {
			last = f.data
		}
	}
	conn.sendMu.Unlock()
	if last == nil || !strings.Contains(string(last), `"n":99`) {
		t.Fatalf("coalesced frame must carry the newest payload, got %s", last)
	}
}

// TestQueueOverflowTearsDownConn (#1542): a queue outgrowing maxSendBytes
// terminates the connection with ErrQueueFull instead of buffering forever.
func TestQueueOverflowTearsDownConn(t *testing.T) {
	old := maxSendBytes
	maxSendBytes = 4096
	defer func() { maxSendBytes = old }()

	cli, _ := newPipePair() // the server end is never read from
	conn := NewConn(cli, Handler{})
	defer conn.Close()
	payload := strings.Repeat("x", 512)
	var last error
	for i := 0; i < 64; i++ {
		if last = conn.Notify("textDocument/didChange", map[string]any{"text": payload, "n": i}); last != nil {
			break
		}
	}
	if last != ErrQueueFull && last != ErrClosed {
		t.Fatalf("overflow must fail the enqueue, got %v", last)
	}
	select {
	case <-conn.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("overflow must terminate the connection")
	}
	if err := conn.Err(); err != ErrQueueFull {
		t.Fatalf("Err = %v, want ErrQueueFull", err)
	}
}
