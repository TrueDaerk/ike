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

// TestRequestFailedSendsErrorToast covers the shared error seam (#372): a
// non-nil error must reach the user as an error ServerStatusMsg naming the
// action and the server's message; a nil error must stay silent.
func TestRequestFailedSendsErrorToast(t *testing.T) {
	msgs := make(chan tea.Msg, 1)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })

	if requestFailed(h, "find usages", nil) {
		t.Fatal("nil error must not report a failure")
	}
	select {
	case m := <-msgs:
		t.Fatalf("nil error must not send anything, got %#v", m)
	default:
	}

	if !requestFailed(h, "find usages", io.ErrUnexpectedEOF) {
		t.Fatal("non-nil error must report a failure")
	}
	msg, ok := (<-msgs).(ilsp.ServerStatusMsg)
	if !ok || msg.Kind != ilsp.ServerEventError {
		t.Fatalf("expected an error ServerStatusMsg, got %#v", msg)
	}
	if want := "find usages failed: " + io.ErrUnexpectedEOF.Error(); msg.Text != want {
		t.Fatalf("toast text = %q, want %q", msg.Text, want)
	}
}

type pipeRWC struct {
	io.Reader
	io.Writer
}

func (pipeRWC) Close() error { return nil }

// erroringConnector dials an in-memory server that completes the initialize
// handshake with full request capabilities and then answers every request
// with a JSON-RPC error — the shape of a server-side request failure.
func erroringConnector() manager.Connector {
	return func(spec ilsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), error) {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		connCh := make(chan *jsonrpc.Conn, 1)
		var srv *jsonrpc.Conn
		srvConn := jsonrpc.NewConn(pipeRWC{Reader: sr, Writer: sw}, jsonrpc.Handler{
			Request: func(id jsonrpc.ID, method string, params json.RawMessage) {
				if srv == nil {
					srv = <-connCh
				}
				if method == "initialize" {
					_ = srv.Respond(id, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{
						TextDocumentSync:   json.RawMessage(`1`),
						HoverProvider:      json.RawMessage(`true`),
						DefinitionProvider: json.RawMessage(`true`),
						ReferencesProvider: json.RawMessage(`true`),
						CodeActionProvider: json.RawMessage(`true`),
					}}, nil)
					return
				}
				_ = srv.Respond(id, nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "boom"})
			},
		})
		connCh <- srvConn
		conn := jsonrpc.NewConn(pipeRWC{Reader: cr, Writer: cw}, handler)
		return client.New(conn), func() { conn.Close(); srvConn.Close() }, nil
	}
}

// TestRequestErrorsSurfaceToasts wires a bridge to a server that fails every
// request and asserts each user-initiated command (#372: references,
// definition, hover, code actions) surfaces the server error instead of
// silently doing nothing.
func TestRequestErrorsSurfaceToasts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := ilsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) {
		if lang == spec.Language {
			return spec, true
		}
		return ilsp.ServerSpec{}, false
	}

	msgs := make(chan tea.Msg, 16)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })

	b := &bridge{h: h, mgr: manager.New(resolve, erroringConnector(), manager.Callbacks{})}
	if err := b.mgr.Open(path, "go", "package main\n"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	b.setCur(path, 0, 0)

	cases := []struct {
		name string
		run  func() tea.Cmd
		want string
	}{
		{"references", func() tea.Cmd { return b.references(h) }, "find usages failed: "},
		{"definition", func() tea.Cmd { return b.definition(h) }, "go to definition failed: "},
		{"hover", func() tea.Cmd { return b.hover(h) }, "hover failed: "},
		{"codeAction", func() tea.Cmd { return b.codeAction(h) }, "code actions failed: "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run()
			select {
			case m := <-msgs:
				msg, ok := m.(ilsp.ServerStatusMsg)
				if !ok || msg.Kind != ilsp.ServerEventError {
					t.Fatalf("expected an error ServerStatusMsg, got %#v", m)
				}
				if msg.Text != tc.want+"boom" {
					t.Fatalf("toast text = %q, want %q", msg.Text, tc.want+"boom")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no toast arrived — the error was swallowed")
			}
		})
	}
}

// TestDefinitionNotice guards #858: an empty definition answer names its
// cause instead of failing silently.
func TestDefinitionNotice(t *testing.T) {
	if got := definitionNotice(true); got != "no definition found under the cursor" {
		t.Fatalf("supported notice = %q", got)
	}
	if got := definitionNotice(false); got != "go to definition unavailable: no ready language server for this file" {
		t.Fatalf("unsupported notice = %q", got)
	}
}
