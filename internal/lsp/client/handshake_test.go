package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
)

// nextMsg reads the next method the fake server received, or fails the test.
func nextMsg(t *testing.T, order <-chan string) string {
	t.Helper()
	select {
	case m := <-order:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message to reach the server")
		return ""
	}
}

// TestHandshakeHoldsBackEarlyTraffic guards #937: no client message may reach
// the server between the initialize request and the initialized notification —
// Intelephense crashes when didOpen or initialized races its (async)
// initialize handler. Notifications fired during the handshake are queued and
// flushed after initialized; requests wait for the gate.
func TestHandshakeHoldsBackEarlyTraffic(t *testing.T) {
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	conn := jsonrpc.NewConn(rwc{Reader: cr, Writer: cw}, jsonrpc.Handler{})
	c := New(conn)
	t.Cleanup(func() { conn.Close() })

	order := make(chan string, 16)
	release := make(chan struct{}) // gates the initialize response, like an async server
	go func() {
		in := bufio.NewReader(sr)
		for {
			payload, err := readFrame(in)
			if err != nil {
				return
			}
			var msg struct {
				ID     *json.RawMessage `json:"id"`
				Method string           `json:"method"`
			}
			if json.Unmarshal(payload, &msg) != nil || msg.Method == "" {
				continue
			}
			order <- msg.Method
			switch msg.Method {
			case "initialize":
				<-release
				resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{}}}`, string(*msg.ID))
				_ = writeFrameRaw(sw, []byte(resp))
			case "textDocument/hover":
				resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":null}`, string(*msg.ID))
				_ = writeFrameRaw(sw, []byte(resp))
			}
		}
	}()

	// Traffic fired before/while the handshake is in flight: a notification
	// (queued) and a request (blocks on the gate).
	if err := c.DidOpen(protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: "file:///a.php", LanguageID: "php", Version: 1},
	}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}
	hoverDone := make(chan error, 1)
	go func() {
		ctx, cancel := ctx2s()
		defer cancel()
		_, err := c.Hover(ctx, protocol.HoverParams{})
		hoverDone <- err
	}()

	initDone := make(chan error, 1)
	go func() {
		ctx, cancel := ctx2s()
		defer cancel()
		_, err := c.Initialize(ctx, InitParams{RootURI: "file:///tmp"})
		initDone <- err
	}()

	// The server sees the initialize request — and, while its handler is
	// still "working", nothing else.
	if got := nextMsg(t, order); got != "initialize" {
		t.Fatalf("first message = %q, want initialize", got)
	}
	select {
	case got := <-order:
		t.Fatalf("message %q reached the server before the initialize response", got)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-initDone; err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Strict order after the response: initialized first, then the queued
	// notification, then the gated request.
	if got := nextMsg(t, order); got != "initialized" {
		t.Fatalf("post-response message = %q, want initialized", got)
	}
	if got := nextMsg(t, order); got != "textDocument/didOpen" {
		t.Fatalf("queued notification = %q, want textDocument/didOpen", got)
	}
	if got := nextMsg(t, order); got != "textDocument/hover" {
		t.Fatalf("gated request = %q, want textDocument/hover", got)
	}
	if err := <-hoverDone; err != nil {
		t.Fatalf("hover: %v", err)
	}
}
