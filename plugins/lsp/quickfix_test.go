package lsp

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// quickfix_test.go covers the bridge half of #2175: an out-of-editor
// code-action request carries its own location, reaches the server with the
// diagnostic in context, and applies through the shared WorkspaceEdit path —
// in-buffer for an open file, on disk for a closed one.

// quickFixConnector dials an in-memory server that answers
// textDocument/codeAction with one quick fix per diagnostic it was given
// context for, so a test can assert the context actually travelled. The
// action carries whatever WorkspaceEdit changes edit() builds for it.
func quickFixConnector(edit func(uri string) map[string][]protocol.TextEdit) manager.Connector {
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
						TextDocumentSync:   json.RawMessage(`1`),
						CodeActionProvider: json.RawMessage(`true`),
					}}, nil)
				case "textDocument/codeAction":
					var p protocol.CodeActionParams
					_ = json.Unmarshal(params, &p)
					acts := []protocol.CodeAction{{
						Title:       "Declare fooBar (" + itoa(len(p.Context.Diagnostics)) + " diags, line " + itoa(p.Range.Start.Line) + ")",
						Kind:        "quickfix",
						IsPreferred: true,
						Edit:        &protocol.WorkspaceEdit{Changes: edit(p.TextDocument.URI)},
					}}
					_ = srv.Respond(id, acts, nil)
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

// itoa keeps the fake server's titles readable without pulling strconv into
// every assertion.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return "many"
}

// quickFixBridge wires a bridge over an open buffer (a.go, synced into the
// manager) and a closed file on disk (b.go), with one cached diagnostic on
// a.go line 2 so the request has context to pass along.
func quickFixBridge(t *testing.T, edit func(dir string, uri string) map[string][]protocol.TextEdit) (*bridge, string, string, chan tea.Msg) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	openPath := filepath.Join(dir, "a.go")
	openText := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(openPath, []byte(openText), 0o644); err != nil {
		t.Fatal(err)
	}
	closedPath := filepath.Join(dir, "b.go")
	if err := os.WriteFile(closedPath, []byte("old text here\n"), 0o644); err != nil {
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
	b := &bridge{h: h, mgr: manager.New(resolve, quickFixConnector(func(uri string) map[string][]protocol.TextEdit {
		return edit(dir, uri)
	}), manager.Callbacks{})}
	if err := b.mgr.Open(openPath, "go", openText); err != nil {
		t.Fatalf("Open: %v", err)
	}
	// The cached publish the Problems pane lists — and the very context the
	// request must carry back to the server.
	b.diags = map[string][]protocol.Diagnostic{openPath: {{
		Range:    protocol.Range{Start: protocol.Position{Line: 2}, End: protocol.Position{Line: 2, Character: 4}},
		Message:  "undefined: fooBar",
		Severity: 1,
	}}}
	t.Cleanup(b.mgr.Shutdown)
	return b, openPath, closedPath, msgs
}

// sameFileEdit rewrites the first four characters of line 2 in the requested
// file itself — the ordinary "fix it where it is" shape.
func sameFileEdit(_ string, uri string) map[string][]protocol.TextEdit {
	return map[string][]protocol.TextEdit{uri: {{
		Range: protocol.Range{
			Start: protocol.Position{Line: 2, Character: 0},
			End:   protocol.Position{Line: 2, Character: 4},
		},
		NewText: "FUNC",
	}}}
}

// recvQuickFix drains messages until the code-action offer arrives, so an
// incidental status toast cannot make the assertion flaky.
func recvQuickFix(t *testing.T, msgs chan tea.Msg) ilsp.CodeActionsMsg {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-msgs:
			if ca, ok := m.(ilsp.CodeActionsMsg); ok {
				return ca
			}
		case <-deadline:
			t.Fatal("timed out waiting for the quick-fix offer")
			return ilsp.CodeActionsMsg{}
		}
	}
}

// TestQuickFixAtListsActionsForTheGivenRange is the listing contract: the
// request needs no caret, reaches the server at the range it was handed, and
// carries the cached diagnostics as CodeActionContext.
func TestQuickFixAtListsActionsForTheGivenRange(t *testing.T) {
	b, openPath, _, msgs := quickFixBridge(t, sameFileEdit)

	b.quickFixAt(b.h, ilsp.QuickFixRequest{
		Path:  openPath,
		Range: buffer.Range{Start: buffer.Position{Line: 2}, End: buffer.Position{Line: 2, Col: 4}},
	})
	msg := recvQuickFix(t, msgs)
	if !msg.QuickFix || msg.Intentions {
		t.Fatalf("offer = %+v, want QuickFix set and no intention merge", msg)
	}
	if msg.Path != openPath {
		t.Fatalf("offer path = %q, want %q", msg.Path, openPath)
	}
	if len(msg.Actions) != 1 {
		t.Fatalf("actions = %+v", msg.Actions)
	}
	act := msg.Actions[0]
	if act.Kind != "quickfix" || !act.Preferred {
		t.Fatalf("action = %+v, want the preferred quick fix", act)
	}
	// The fake echoes what it received: one diagnostic of context, at line 2.
	if act.Title != "Declare fooBar (1 diags, line 2)" {
		t.Fatalf("title = %q — the diagnostic context and range must reach the server", act.Title)
	}
}

// TestQuickFixApplyEditsOpenBuffer: applying routes through the shared
// WorkspaceEdit path, so an open file is edited in its buffer (one undo unit)
// rather than rewritten behind the editor's back.
func TestQuickFixApplyEditsOpenBuffer(t *testing.T) {
	b, openPath, _, msgs := quickFixBridge(t, sameFileEdit)

	b.quickFixAt(b.h, ilsp.QuickFixRequest{
		Path:  openPath,
		Range: buffer.Range{Start: buffer.Position{Line: 2}, End: buffer.Position{Line: 2, Col: 4}},
	})
	offer := recvQuickFix(t, msgs)
	offer.Apply(0)

	fm, ok := recvMsg(t, msgs, "buffer edits").(ilsp.FormatEditsMsg)
	if !ok || fm.Path != openPath {
		t.Fatalf("apply must edit the open buffer, got %#v", fm)
	}
	if len(fm.Edits) != 1 || fm.Edits[0].Text != "FUNC" {
		t.Fatalf("edits = %+v", fm.Edits)
	}
	// The buffer keeps the change; nothing was written behind its back.
	data, _ := os.ReadFile(openPath)
	if string(data) != "package main\n\nfunc main() {}\n" {
		t.Fatalf("an open file must not be rewritten on disk, got %q", data)
	}
}

// TestQuickFixApplyEditsClosedFile is the unopened-file half of the same
// contract: a fix landing in a file no editor holds is written to disk, just
// as the intention path does it.
func TestQuickFixApplyEditsClosedFile(t *testing.T) {
	b, openPath, closedPath, msgs := quickFixBridge(t, func(dir, _ string) map[string][]protocol.TextEdit {
		return map[string][]protocol.TextEdit{protocol.PathToURI(filepath.Join(dir, "b.go")): {{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 3},
			},
			NewText: "new",
		}}}
	})

	b.quickFixAt(b.h, ilsp.QuickFixRequest{
		Path:  openPath,
		Range: buffer.Range{Start: buffer.Position{Line: 2}, End: buffer.Position{Line: 2, Col: 4}},
	})
	offer := recvQuickFix(t, msgs)
	offer.Apply(0)

	// The summary toast confirms the apply; the file itself carries the edit.
	if _, ok := recvMsg(t, msgs, "apply summary").(ilsp.ServerStatusMsg); !ok {
		t.Fatal("applying must report its outcome")
	}
	data, err := os.ReadFile(closedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new text here\n" {
		t.Fatalf("closed file = %q, want the edit written to disk", data)
	}
}

// TestQuickFixWithoutManagerAnswersEmptyOnTheUpdateGoroutine: with nothing to
// ask, the bridge still answers — as a tea.Cmd, not a Send (#2027) — so the
// app can report "no quick fixes" instead of hanging on a reply that never
// comes.
func TestQuickFixWithoutManagerAnswersEmptyOnTheUpdateGoroutine(t *testing.T) {
	sent := make(chan tea.Msg, 4)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { sent <- m })
	b := &bridge{h: h}

	cmd := b.quickFixAt(h, ilsp.QuickFixRequest{})
	if cmd == nil {
		t.Fatal("a request with no path must still answer")
	}
	msg, ok := cmd().(ilsp.CodeActionsMsg)
	if !ok {
		t.Fatalf("command produced %#v, want a CodeActionsMsg", cmd())
	}
	if !msg.QuickFix || len(msg.Actions) != 0 {
		t.Fatalf("offer = %+v, want an empty QuickFix answer", msg)
	}
}
