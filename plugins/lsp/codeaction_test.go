package lsp

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// codeaction_test.go covers the lazy half of the offer (#2252): an action that
// arrives without an edit is resolved through codeAction/resolve — once, for
// both the popup's preview and the apply that follows it — and an action that
// only runs a command previews as "no preview" while still applying.

// lazyActionConnector dials an in-memory server that offers two actions
// without edits: a lazy one carrying a data token, whose edit only
// codeAction/resolve produces, and a command-only one that never has an edit.
// resolves counts the resolve round trips so a test can assert they happen
// once per action rather than per keystroke.
func lazyActionConnector(uriOf func() string, resolves *int) manager.Connector {
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
						CodeActionProvider: json.RawMessage(`{"resolveProvider":true}`),
					}}, nil)
				case "textDocument/codeAction":
					_ = srv.Respond(id, []protocol.CodeAction{
						{Title: "Lazy fix", Kind: "quickfix", Data: json.RawMessage(`{"id":7}`)},
						{Title: "Run generator", Kind: "source", Command: &protocol.Command{Title: "gen", Command: "server.gen"}},
					}, nil)
				case "codeAction/resolve":
					*resolves++
					var act protocol.CodeAction
					_ = json.Unmarshal(params, &act)
					if len(act.Data) == 0 {
						// Nothing lazy about this one: a command action stays
						// edit-less however often it is resolved.
						_ = srv.Respond(id, act, nil)
						return
					}
					act.Edit = &protocol.WorkspaceEdit{Changes: map[string][]protocol.TextEdit{
						uriOf(): {{
							Range: protocol.Range{
								Start: protocol.Position{Line: 2, Character: 0},
								End:   protocol.Position{Line: 2, Character: 4},
							},
							NewText: "FUNC",
						}},
					}}
					_ = srv.Respond(id, act, nil)
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

// lazyActionBridge wires a bridge over one open buffer whose server offers the
// lazy and the command-only action.
func lazyActionBridge(t *testing.T, resolves *int) (*bridge, string, chan tea.Msg) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.go")
	text := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
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
	uri := protocol.PathToURI(path)
	b := &bridge{h: h, mgr: manager.New(resolve,
		lazyActionConnector(func() string { return uri }, resolves), manager.Callbacks{})}
	if err := b.mgr.Open(path, "go", text); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(b.mgr.Shutdown)
	return b, path, msgs
}

// recvActionPreview drains messages until a preview reply arrives.
func recvActionPreview(t *testing.T, msgs chan tea.Msg) ilsp.ActionPreviewMsg {
	t.Helper()
	m, _ := recvMsgOf[ilsp.ActionPreviewMsg](t, msgs, "action preview")
	return m
}

// recvMsgOf drains messages until one of type T arrives.
func recvMsgOf[T tea.Msg](t *testing.T, msgs chan tea.Msg, what string) (T, bool) {
	t.Helper()
	var zero T
	for i := 0; i < 32; i++ {
		m := recvMsg(t, msgs, what)
		if typed, ok := m.(T); ok {
			return typed, true
		}
	}
	t.Fatalf("timed out waiting for %s", what)
	return zero, false
}

// offerFor runs a quick-fix request (the code-action path that needs no caret)
// and returns the resulting offer.
func offerFor(t *testing.T, b *bridge, path string, msgs chan tea.Msg) ilsp.CodeActionsMsg {
	t.Helper()
	b.quickFixAt(b.h, ilsp.QuickFixRequest{
		Path:  path,
		Range: buffer.Range{Start: buffer.Position{Line: 2}, End: buffer.Position{Line: 2, Col: 4}},
	})
	return recvQuickFix(t, msgs)
}

// TestActionPreviewResolvesLazyEdit: previewing an action that arrived without
// an edit resolves it via codeAction/resolve and answers with the file's text
// before and after — computed, not applied.
func TestActionPreviewResolvesLazyEdit(t *testing.T) {
	resolves := 0
	b, path, msgs := lazyActionBridge(t, &resolves)
	offer := offerFor(t, b, path, msgs)
	if offer.Preview == nil {
		t.Fatal("an offer from a server must carry a preview continuation")
	}

	offer.Preview(0)
	prev := recvActionPreview(t, msgs)
	if prev.Index != 0 || prev.Path != path {
		t.Fatalf("preview = %+v, want it addressed to row 0 of %q", prev, path)
	}
	if len(prev.Files) != 1 {
		t.Fatalf("preview files = %+v, want the one edited file", prev.Files)
	}
	f := prev.Files[0]
	if f.Before != "package main\n\nfunc main() {}\n" {
		t.Fatalf("before = %q, want the buffer as it stands", f.Before)
	}
	if f.After != "package main\n\nFUNC main() {}\n" {
		t.Fatalf("after = %q, want the resolved edit applied to a copy", f.After)
	}
	if resolves != 1 {
		t.Fatalf("resolve round trips = %d, want exactly one", resolves)
	}
}

// TestActionPreviewLeavesTheDocumentUntouched is the "preview applies nothing"
// contract: after previewing, the manager's synced document — the very text
// the editor buffer holds — is unchanged, and no edit was dispatched.
func TestActionPreviewLeavesTheDocumentUntouched(t *testing.T) {
	resolves := 0
	b, path, msgs := lazyActionBridge(t, &resolves)
	offer := offerFor(t, b, path, msgs)

	offer.Preview(0)
	recvActionPreview(t, msgs)

	lines, ok := b.mgr.DocLines(path)
	if !ok {
		t.Fatal("the document must still be open")
	}
	if got := joinLines(lines); got != "package main\n\nfunc main() {}\n" {
		t.Fatalf("document = %q — previewing must not touch the buffer", got)
	}
	select {
	case m := <-msgs:
		if fm, isEdit := m.(ilsp.FormatEditsMsg); isEdit {
			t.Fatalf("previewing dispatched edits: %#v", fm)
		}
	default:
	}
}

// TestActionPreviewCommandActionHasNoPreview: a command-style action carries
// no edit at any point, so it answers with a note rather than an empty diff.
func TestActionPreviewCommandActionHasNoPreview(t *testing.T) {
	resolves := 0
	b, path, msgs := lazyActionBridge(t, &resolves)
	offer := offerFor(t, b, path, msgs)

	offer.Preview(1)
	prev := recvActionPreview(t, msgs)
	if len(prev.Files) != 0 {
		t.Fatalf("a command action must preview no files, got %+v", prev.Files)
	}
	if prev.Note == "" {
		t.Fatal("a command action must say why there is no preview")
	}
}

// TestActionApplyUsesThePreviewedEdit: applying after a preview reuses the
// resolved action — the same edit the preview showed, with no second resolve
// whose answer could differ.
func TestActionApplyUsesThePreviewedEdit(t *testing.T) {
	resolves := 0
	b, path, msgs := lazyActionBridge(t, &resolves)
	offer := offerFor(t, b, path, msgs)

	offer.Preview(0)
	prev := recvActionPreview(t, msgs)
	offer.Apply(0)
	fm, _ := recvMsgOf[ilsp.FormatEditsMsg](t, msgs, "buffer edits")
	if fm.Path != path || len(fm.Edits) != 1 || fm.Edits[0].Text != "FUNC" {
		t.Fatalf("apply = %#v, want exactly the previewed edit", fm)
	}
	if resolves != 1 {
		t.Fatalf("resolve round trips = %d, want the preview's single one reused", resolves)
	}
	if prev.Files[0].After != "package main\n\nFUNC main() {}\n" {
		t.Fatalf("preview showed %q — apply must produce it", prev.Files[0].After)
	}
}

// TestActionApplyResolvesWithoutAPreview: an action run straight from the list
// resolves on apply, instead of reporting that it returned no edit.
func TestActionApplyResolvesWithoutAPreview(t *testing.T) {
	resolves := 0
	b, path, msgs := lazyActionBridge(t, &resolves)
	offer := offerFor(t, b, path, msgs)

	offer.Apply(0)
	fm, _ := recvMsgOf[ilsp.FormatEditsMsg](t, msgs, "buffer edits")
	if len(fm.Edits) != 1 || fm.Edits[0].Text != "FUNC" {
		t.Fatalf("apply = %#v, want the lazily resolved edit", fm)
	}
	if resolves != 1 {
		t.Fatalf("resolve round trips = %d, want one", resolves)
	}
}

// joinLines rebuilds a document's text from the manager's line view.
func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
