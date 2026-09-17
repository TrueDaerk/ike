package lsp

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
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

// autoimport_test.go covers the bridge half of #2610: a completion reply is
// stamped with a sequence, completionItem/resolve round-trips the server's
// data token (pyright and tsserver answer a data-less resolve with the item
// unchanged — no import), an accept resolves immediately instead of waiting
// for the selection debounce, and the reply carries the sequence back.

// autoImportConnector dials an in-memory server whose completion offers one
// unimported symbol carrying a data token; resolve returns the import edit
// only when the token came back. gotData records the resolve's data.
func autoImportConnector(mu *sync.Mutex, gotData *string) manager.Connector {
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
						CompletionProvider: &protocol.CompletionOptions{ResolveProvider: true},
					}}, nil)
				case "textDocument/completion":
					_ = srv.Respond(id, protocol.CompletionList{Items: []protocol.CompletionItem{{
						Label:        "tldextract",
						Kind:         9,
						Detail:       "Auto-import",
						LabelDetails: &protocol.CompletionItemLabelDetails{Description: "tldextract"},
						Data:         json.RawMessage(`{"autoImportText":"import tldextract","symbolLabel":"tldextract"}`),
					}}}, nil)
				case "completionItem/resolve":
					var it protocol.CompletionItem
					_ = json.Unmarshal(params, &it)
					mu.Lock()
					*gotData = string(it.Data)
					mu.Unlock()
					if len(it.Data) == 0 {
						_ = srv.Respond(id, it, nil) // the real servers' behaviour
						return
					}
					it.Documentation = "the tldextract module"
					it.AdditionalTextEdits = []protocol.TextEdit{{
						Range:   protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 0}},
						NewText: "import tldextract\n\n",
					}}
					_ = srv.Respond(id, it, nil)
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

func autoImportBridge(t *testing.T) (*bridge, string, chan tea.Msg, *sync.Mutex, *string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.py")
	content := "x = tld\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := ilsp.ServerSpec{Language: "python", Command: "fake", RootMarkers: []string{"pyproject.toml"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) { return spec, lang == spec.Language }
	msgs := make(chan tea.Msg, 16)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })
	var mu sync.Mutex
	var gotData string
	b := &bridge{h: h, mgr: manager.New(resolve, autoImportConnector(&mu, &gotData), manager.Callbacks{})}
	if err := b.mgr.Open(path, "python", content); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return b, path, msgs, &mu, &gotData
}

func nextCompletion(t *testing.T, msgs chan tea.Msg) ilsp.CompletionMsg {
	t.Helper()
	select {
	case m := <-msgs:
		cm, ok := m.(ilsp.CompletionMsg)
		if !ok {
			t.Fatalf("expected a CompletionMsg, got %#v", m)
		}
		return cm
	case <-time.After(5 * time.Second):
		t.Fatal("no CompletionMsg arrived")
	}
	return ilsp.CompletionMsg{}
}

// TestResolveNowRoundTripsDataAndSeq: an accept resolves without the
// debounce, the resolve carries the item's data token, and the reply is
// stamped with the completion reply's sequence and carries the import.
func TestResolveNowRoundTripsDataAndSeq(t *testing.T) {
	b, path, msgs, mu, gotData := autoImportBridge(t)
	b.requestCompletion(path, 0, 7, "")
	cm := nextCompletion(t, msgs)
	if cm.Seq == 0 || len(cm.Items) != 1 {
		t.Fatalf("CompletionMsg = seq %d, %d items; want a stamped reply with one item", cm.Seq, len(cm.Items))
	}
	if cm.Items[0].Detail != "tldextract Auto-import" {
		t.Fatalf("Detail = %q, want the labelDetails module ahead of the detail", cm.Items[0].Detail)
	}

	start := time.Now()
	b.resolveNow(path, 0, cm.Seq)
	select {
	case m := <-msgs:
		rm, ok := m.(ilsp.CompletionResolveMsg)
		if !ok {
			t.Fatalf("expected a CompletionResolveMsg, got %#v", m)
		}
		if rm.ID != 0 || rm.Seq != cm.Seq || rm.Path != path {
			t.Fatalf("resolve = id %d seq %d path %q, want id 0 seq %d path %q", rm.ID, rm.Seq, rm.Path, cm.Seq, path)
		}
		if len(rm.AdditionalEdits) != 1 || rm.AdditionalEdits[0].Text != "import tldextract\n\n" {
			t.Fatalf("AdditionalEdits = %+v, want the import edit", rm.AdditionalEdits)
		}
		if rm.Doc != "the tldextract module" {
			t.Fatalf("Doc = %q", rm.Doc)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no CompletionResolveMsg arrived")
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("accept resolve took %v — it must not wait for the 120ms selection debounce", d)
	}
	mu.Lock()
	defer mu.Unlock()
	if *gotData != `{"autoImportText":"import tldextract","symbolLabel":"tldextract"}` {
		t.Fatalf("server saw data %q, want the token round-tripped", *gotData)
	}
}

// TestResolveNowIgnoresSupersededReply: an accept naming an older reply's
// sequence (a new completion has been cached since) never resolves against
// the new list's item at the same index.
func TestResolveNowIgnoresSupersededReply(t *testing.T) {
	b, path, msgs, _, _ := autoImportBridge(t)
	b.requestCompletion(path, 0, 7, "")
	first := nextCompletion(t, msgs)
	b.requestCompletion(path, 0, 7, "")
	second := nextCompletion(t, msgs)
	if second.Seq <= first.Seq {
		t.Fatalf("sequences %d then %d, want increasing", first.Seq, second.Seq)
	}
	b.resolveNow(path, 0, first.Seq)
	select {
	case m := <-msgs:
		t.Fatalf("stale accept must not resolve, got %#v", m)
	case <-time.After(300 * time.Millisecond):
	}
	// An in-flight or answered item is not resolved twice: the debounced
	// select fires once, the accept after it stays silent.
	b.scheduleResolve(path, 0, second.Seq)
	select {
	case m := <-msgs:
		if _, ok := m.(ilsp.CompletionResolveMsg); !ok {
			t.Fatalf("expected a CompletionResolveMsg, got %#v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no CompletionResolveMsg arrived")
	}
	b.resolveNow(path, 0, second.Seq)
	select {
	case m := <-msgs:
		t.Fatalf("already resolved item must not resolve again, got %#v", m)
	case <-time.After(300 * time.Millisecond):
	}
}
