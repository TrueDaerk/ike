package lsp

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// mousehover_test.go covers the position-carrying hover seam (#1129): the
// mouse-idle flow requests hover at an arbitrary buffer position — not the
// tracked cursor — and the reply carries that position back, tagged as a
// mouse reply, so the editor can drop it when stale.

// answeringHoverConnector dials an in-memory server that completes the
// initialize handshake with hover capability, records the position of every
// textDocument/hover request, and answers it with fixed markdown.
func answeringHoverConnector(gotLine, gotChar *int) manager.Connector {
	return func(spec ilsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), func() string, error) {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		connCh := make(chan *jsonrpc.Conn, 1)
		var srv *jsonrpc.Conn
		srvConn := jsonrpc.NewConn(pipeRWC{Reader: sr, Writer: sw}, jsonrpc.Handler{
			Request: func(id jsonrpc.ID, method string, params json.RawMessage) {
				if srv == nil {
					srv = <-connCh
				}
				switch method {
				case "initialize":
					_ = srv.Respond(id, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{
						TextDocumentSync: json.RawMessage(`1`),
						HoverProvider:    json.RawMessage(`true`),
					}}, nil)
				case "textDocument/hover":
					var p protocol.HoverParams
					_ = json.Unmarshal(params, &p)
					*gotLine, *gotChar = p.Position.Line, p.Position.Character
					_ = srv.Respond(id, protocol.Hover{
						Contents: json.RawMessage(`{"kind":"markdown","value":"the docs"}`),
					}, nil)
				default:
					_ = srv.Respond(id, nil, nil)
				}
			},
		})
		connCh <- srvConn
		conn := jsonrpc.NewConn(pipeRWC{Reader: cr, Writer: cw}, handler)
		return client.New(conn), func() { conn.Close(); srvConn.Close() }, nil, nil
	}
}

func TestRequestHoverCarriesArbitraryPosition(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.go")
	content := "package main\n\nfunc target() {}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := ilsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) { return spec, lang == spec.Language }

	msgs := make(chan tea.Msg, 16)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })

	gotLine, gotChar := -1, -1
	b := &bridge{h: h, mgr: manager.New(resolve, answeringHoverConnector(&gotLine, &gotChar), manager.Callbacks{})}
	if err := b.mgr.Open(path, "go", content); err != nil {
		t.Fatalf("Open: %v", err)
	}
	// The tracked cursor sits elsewhere on purpose: the request must use the
	// explicit position, not the cursor.
	b.setCur(path, 0, 0)
	b.requestHover(h, path, 2, 5, true)

	select {
	case m := <-msgs:
		hv, ok := m.(ilsp.HoverMsg)
		if !ok {
			t.Fatalf("expected a HoverMsg, got %#v", m)
		}
		if !hv.Mouse || hv.Line != 2 || hv.Col != 5 {
			t.Fatalf("HoverMsg = mouse %v at (%d,%d), want a mouse reply at (2,5)", hv.Mouse, hv.Line, hv.Col)
		}
		if hv.Contents != "the docs" {
			t.Fatalf("Contents = %q, want %q", hv.Contents, "the docs")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no HoverMsg arrived")
	}
	if gotLine != 2 || gotChar != 5 {
		t.Fatalf("server saw position (%d,%d), want (2,5)", gotLine, gotChar)
	}
}
