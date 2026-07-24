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

// chainCaps is the fake server's capability answer for the save-chain tests:
// full sync, whole-document formatting and a code-action provider declaring
// the organize-imports kind.
func chainCaps() protocol.ServerCapabilities {
	return protocol.ServerCapabilities{
		TextDocumentSync:           json.RawMessage(`1`),
		DocumentFormattingProvider: json.RawMessage(`true`),
		CodeActionProvider:         json.RawMessage(`{"codeActionKinds":["source.organizeImports"]}`),
	}
}

// chainConnector dials an in-memory server that answers codeAction with one
// organize-imports action (an inline edit inserting "organized") and
// formatting with one edit inserting "formatted". Request methods are logged
// to reqs in arrival order; a method listed in mute is never answered — the
// timeout fall-through shape.
func chainConnector(caps protocol.ServerCapabilities, reqs chan string, mute map[string]bool) manager.Connector {
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
				if method != "initialize" && method != "shutdown" {
					select {
					case reqs <- method:
					default:
					}
				}
				if mute[method] {
					return // never answered: the per-step timeout must fire
				}
				switch method {
				case "initialize":
					_ = srv.Respond(id, protocol.InitializeResult{Capabilities: caps}, nil)
				case "textDocument/codeAction":
					var p protocol.CodeActionParams
					_ = json.Unmarshal(params, &p)
					if len(p.Context.Only) != 1 || p.Context.Only[0] != protocol.KindSourceOrganizeImports {
						_ = srv.Respond(id, []protocol.CodeAction{}, nil)
						return
					}
					_ = srv.Respond(id, []protocol.CodeAction{{
						Title: "Organize Imports",
						Kind:  protocol.KindSourceOrganizeImports,
						Edit: &protocol.WorkspaceEdit{Changes: map[string][]protocol.TextEdit{
							p.TextDocument.URI: {{NewText: "organized "}},
						}},
					}}, nil)
				case "textDocument/formatting":
					_ = srv.Respond(id, []protocol.TextEdit{{NewText: "formatted "}}, nil)
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

// chainBridge wires a bridge to the given connector over a synced temp Go
// file and returns it with the host's message channel.
func chainBridge(t *testing.T, connect manager.Connector) (*bridge, string, chan tea.Msg) {
	t.Helper()
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
	msgs := make(chan tea.Msg, 32)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })
	b := &bridge{h: h, mgr: manager.New(resolve, connect, manager.Callbacks{})}
	if err := b.mgr.Open(path, "go", "package main\n"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return b, path, msgs
}

// recvMsg waits for the next host message or fails the test.
func recvMsg(t *testing.T, msgs chan tea.Msg, what string) tea.Msg {
	t.Helper()
	select {
	case m := <-msgs:
		return m
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return nil
	}
}

// TestSaveChainOrderOrganizeThenFormatThenDone is the core ordering contract
// (#1148): the chain delivers the organize-imports edits first, waits for the
// applied ack, then the format edits, then reports done — and the server saw
// codeAction strictly before formatting.
func TestSaveChainOrderOrganizeThenFormatThenDone(t *testing.T) {
	reqs := make(chan string, 8)
	b, path, msgs := chainBridge(t, chainConnector(chainCaps(), reqs, nil))

	cmd := b.saveChainCmd(path, true, true)
	if cmd == nil {
		t.Fatal("capable server + both flags must start a chain")
	}
	_ = cmd()

	m1, ok := recvMsg(t, msgs, "organize edits").(ilsp.FormatEditsMsg)
	if !ok || len(m1.Edits) == 0 || m1.Edits[0].Text != "organized " {
		t.Fatalf("first delivery must be the organize edits, got %#v", m1)
	}
	if m1.Applied == nil {
		t.Fatal("chain deliveries must carry the applied ack")
	}
	m1.Applied()

	m2, ok := recvMsg(t, msgs, "format edits").(ilsp.FormatEditsMsg)
	if !ok || len(m2.Edits) == 0 || m2.Edits[0].Text != "formatted " {
		t.Fatalf("second delivery must be the format edits, got %#v", m2)
	}
	m2.Applied()

	if _, ok := recvMsg(t, msgs, "chain done").(ilsp.SaveChainDoneMsg); !ok {
		t.Fatal("chain must finish with SaveChainDoneMsg")
	}
	if first, second := <-reqs, <-reqs; first != "textDocument/codeAction" || second != "textDocument/formatting" {
		t.Fatalf("server request order = %s, %s; want codeAction then formatting", first, second)
	}
	b.mu.Lock()
	pending := b.saveChains[path]
	b.mu.Unlock()
	if pending {
		t.Fatal("finished chain must clear its pending mark")
	}
}

// TestSaveChainTimeoutFallsThrough guards the never-block rule: a server that
// never answers codeAction must not stall the chain — the format step still
// runs, and an unacked format delivery still ends in SaveChainDoneMsg.
func TestSaveChainTimeoutFallsThrough(t *testing.T) {
	old := saveChainStepTimeout
	saveChainStepTimeout = 200 * time.Millisecond
	defer func() { saveChainStepTimeout = old }()

	reqs := make(chan string, 8)
	b, path, msgs := chainBridge(t, chainConnector(chainCaps(), reqs, map[string]bool{"textDocument/codeAction": true}))

	cmd := b.saveChainCmd(path, true, true)
	if cmd == nil {
		t.Fatal("chain must start")
	}
	start := time.Now()
	_ = cmd()

	m, ok := recvMsg(t, msgs, "format edits").(ilsp.FormatEditsMsg)
	if !ok || len(m.Edits) == 0 || m.Edits[0].Text != "formatted " {
		t.Fatalf("format step must run after the organize timeout, got %#v", m)
	}
	// Deliberately no Applied() call: the ack timeout must release the chain.
	if _, ok := recvMsg(t, msgs, "chain done").(ilsp.SaveChainDoneMsg); !ok {
		t.Fatal("chain must finish despite the dead step")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("timed-out chain took %v, want bounded by the step timeouts", d)
	}
}

// TestSaveChainSkipsWithoutCapability: no formatting and no matching
// code-action kind means no chain at all — the editor writes immediately.
func TestSaveChainSkipsWithoutCapability(t *testing.T) {
	caps := protocol.ServerCapabilities{
		TextDocumentSync:   json.RawMessage(`1`),
		CodeActionProvider: json.RawMessage(`{"codeActionKinds":["quickfix"]}`),
	}
	b, path, _ := chainBridge(t, chainConnector(caps, make(chan string, 8), nil))
	if cmd := b.saveChainCmd(path, true, true); cmd != nil {
		t.Fatal("no formatting + no organize-imports kind must skip the chain")
	}
	if cmd := b.saveChainCmd(path, false, false); cmd != nil {
		t.Fatal("disabled flags must never chain")
	}
}

// TestSaveChainCoalescesReentrantSaves: a second save for a path with a chain
// in flight returns a no-op command instead of stacking a second chain.
func TestSaveChainCoalescesReentrantSaves(t *testing.T) {
	reqs := make(chan string, 8)
	b, path, _ := chainBridge(t, chainConnector(chainCaps(), reqs, nil))
	b.mu.Lock()
	b.saveChains = map[string]bool{path: true} // a chain is pending
	b.mu.Unlock()

	cmd := b.saveChainCmd(path, true, true)
	if cmd == nil {
		t.Fatal("re-entrant save must coalesce, not fall back to a raw write")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("coalesced command must be a no-op, got %#v", msg)
	}
	select {
	case m := <-reqs:
		t.Fatalf("coalesced save must not hit the server, got %s", m)
	case <-time.After(100 * time.Millisecond):
	}
}
