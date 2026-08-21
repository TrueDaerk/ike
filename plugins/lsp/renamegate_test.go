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
	"ike/internal/intention"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// renameCaps is the fake server's capability answer for the gate tests: full
// sync, a code-action provider (so the popup path has something to request)
// and rename with prepareRename — the marksman/gopls shape.
func renameCaps(prepare bool) protocol.ServerCapabilities {
	provider := `true`
	if prepare {
		provider = `{"prepareProvider":true}`
	}
	return protocol.ServerCapabilities{
		TextDocumentSync:   json.RawMessage(`1`),
		RenameProvider:     json.RawMessage(provider),
		CodeActionProvider: json.RawMessage(`true`),
	}
}

// renameConnector dials an in-memory server that accepts prepareRename on the
// first buffer line only — the "caret sits on a heading" half of a Markdown
// document — and rejects every other position with null.
func renameConnector(caps protocol.ServerCapabilities) manager.Connector {
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
					_ = srv.Respond(id, protocol.InitializeResult{Capabilities: caps}, nil)
				case "textDocument/prepareRename":
					var p protocol.PrepareRenameParams
					_ = json.Unmarshal(params, &p)
					if p.Position.Line != 0 {
						_ = srv.Respond(id, nil, nil) // "cannot rename here"
						return
					}
					_ = srv.Respond(id, protocol.Range{
						Start: protocol.Position{Line: 0, Character: 3},
						End:   protocol.Position{Line: 0, Character: 14},
					}, nil)
				case "textDocument/codeAction":
					_ = srv.Respond(id, []protocol.CodeAction{}, nil)
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

// gateBridge wires a bridge over a synced temp Markdown file whose first line
// is a heading and whose third line is prose.
func gateBridge(t *testing.T, caps protocol.ServerCapabilities) (*bridge, string, chan tea.Msg) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "doc.md")
	text := "## Old Heading\n\nplain paragraph\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := ilsp.ServerSpec{Language: "markdown", Command: "fake", RootMarkers: []string{".git"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) {
		if lang == spec.Language {
			return spec, true
		}
		return ilsp.ServerSpec{}, false
	}
	msgs := make(chan tea.Msg, 32)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })
	b := &bridge{h: h, mgr: manager.New(resolve, renameConnector(caps), manager.Callbacks{})}
	if err := b.mgr.Open(path, "markdown", text); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(b.mgr.Shutdown)
	return b, path, msgs
}

// caretAt is the intention context the app snapshots for a caret.
func caretAt(path string, line, col int) intention.Context {
	return intention.Context{Path: path, LangID: "markdown", Line: line, Col: col}
}

// TestRenameIntentionGatedByPosition is #2025's core contract: the popup
// offers "Rename Symbol" where prepareRename accepts (the heading) and stays
// silent where it rejects (the paragraph) — the capability alone is no longer
// enough.
func TestRenameIntentionGatedByPosition(t *testing.T) {
	b, path, _ := gateBridge(t, renameCaps(true))

	b.refreshRenameGate(path, buffer.Position{Line: 0, Col: 4})
	if items := renameIntentionItems(b, caretAt(path, 0, 4)); len(items) != 1 || items[0].CommandID != "lsp.rename" {
		t.Fatalf("renameable caret must offer the entry, got %#v", items)
	}

	b.refreshRenameGate(path, buffer.Position{Line: 2, Col: 2})
	if items := renameIntentionItems(b, caretAt(path, 2, 2)); len(items) != 0 {
		t.Fatalf("rejected caret must offer nothing, got %#v", items)
	}
}

// TestRenameIntentionWithoutVerdictWithholdsEntry guards the strict half of
// the gate: an unvalidated caret (no verdict recorded, or one recorded for a
// different position) is not offered — an entry that can only end in "cannot
// rename here" must never appear.
func TestRenameIntentionWithoutVerdictWithholdsEntry(t *testing.T) {
	b, path, _ := gateBridge(t, renameCaps(true))

	if items := renameIntentionItems(b, caretAt(path, 0, 4)); len(items) != 0 {
		t.Fatalf("no verdict yet must offer nothing, got %#v", items)
	}
	b.refreshRenameGate(path, buffer.Position{Line: 0, Col: 4})
	if items := renameIntentionItems(b, caretAt(path, 0, 7)); len(items) != 0 {
		t.Fatalf("verdict for another column must not be reused, got %#v", items)
	}
}

// TestRenameGateOpenWithoutPrepareSupport guards #426 through the new gate: a
// server offering rename but no prepareRename skips validation entirely, so
// the entry stays offered and the rename attempt decides — unchanged from
// before #2025.
func TestRenameGateOpenWithoutPrepareSupport(t *testing.T) {
	b, path, _ := gateBridge(t, renameCaps(false))

	b.refreshRenameGate(path, buffer.Position{Line: 2, Col: 2})
	if items := renameIntentionItems(b, caretAt(path, 2, 2)); len(items) != 1 {
		t.Fatalf("no prepareRename support must leave the entry offered, got %#v", items)
	}
}

// TestRenameGateInvalidatedByEdit guards the staleness rule: an edit can turn
// a renameable position into an ordinary one, so a recorded verdict for that
// buffer must not survive the change event.
func TestRenameGateInvalidatedByEdit(t *testing.T) {
	b, path, _ := gateBridge(t, renameCaps(true))

	b.refreshRenameGate(path, buffer.Position{Line: 0, Col: 4})
	b.invalidateRenameGate(path)
	if items := renameIntentionItems(b, caretAt(path, 0, 4)); len(items) != 0 {
		t.Fatalf("edited buffer must drop its verdict, got %#v", items)
	}
}

// TestRenamePickReusesGateVerdict is the "cannot rename here is unreachable"
// half: picking the offered entry prompts straight away — on the recorded
// placeholder — instead of asking the server a second time whose answer could
// differ.
func TestRenamePickReusesGateVerdict(t *testing.T) {
	b, path, msgs := gateBridge(t, renameCaps(true))
	b.setCur(path, 0, 4)
	b.refreshRenameGate(path, buffer.Position{Line: 0, Col: 4})

	b.rename(b.h)
	msg, ok := recvMsg(t, msgs, "rename prompt").(ilsp.RenamePromptMsg)
	if !ok {
		t.Fatalf("picking the entry must open the prompt, got %#v", msg)
	}
	if msg.Placeholder != "Old Heading" {
		t.Fatalf("placeholder = %q, want the ranged heading text", msg.Placeholder)
	}
	if _, _, known := b.renameGateAt(path, 0, 4); known {
		t.Error("a consumed verdict must not answer a second rename")
	}
}
