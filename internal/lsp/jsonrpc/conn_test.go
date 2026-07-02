package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
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

func TestCallFailsAfterClose(t *testing.T) {
	cli, _ := newPipePair()
	conn := NewConn(cli, Handler{})
	_ = conn.Close()
	if _, err := conn.Call(context.Background(), "x", nil); err == nil {
		t.Fatal("expected error after close")
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
