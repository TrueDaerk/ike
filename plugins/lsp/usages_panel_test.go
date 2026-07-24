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

// referencesConnector dials an in-memory server that completes the initialize
// handshake with a references capability and answers every
// textDocument/references request with the given locations. reqs counts the
// reference requests served, so the refresh round-trip is observable.
func referencesConnector(locs func() []protocol.Location, reqs *int) manager.Connector {
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
						ReferencesProvider: json.RawMessage(`true`),
					}}, nil)
				case "textDocument/references":
					*reqs++
					_ = srv.Respond(id, locs(), nil)
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

// TestReferencesPanelRoundTrip covers the panel-target wire (#1155):
// lsp.referencesPanel captures the identifier under the cursor, requests the
// references, and delivers a UsagesMsg carrying the symbol, the origin, the
// converted references, and a working Refresh continuation.
func TestReferencesPanelRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "package main\n\nvar foo = 1\n"
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := ilsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) {
		if lang == spec.Language {
			return spec, true
		}
		return ilsp.ServerSpec{}, false
	}

	uri := protocol.PathToURI(path)
	locs := func() []protocol.Location {
		return []protocol.Location{
			{URI: uri, Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 4}, End: protocol.Position{Line: 2, Character: 7}}},
		}
	}
	reqs := 0
	msgs := make(chan tea.Msg, 16)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })

	b := &bridge{h: h, mgr: manager.New(resolve, referencesConnector(locs, &reqs), manager.Callbacks{})}
	if err := b.mgr.Open(path, "go", content); err != nil {
		t.Fatalf("Open: %v", err)
	}
	b.setCur(path, 2, 5) // cursor inside "foo"

	b.referencesPanel(h)
	msg := waitUsages(t, msgs)
	if msg.Symbol != "foo" {
		t.Fatalf("symbol = %q, want foo", msg.Symbol)
	}
	if msg.Path != path || msg.Line != 2 || msg.Col != 5 {
		t.Fatalf("origin = %s:%d:%d", msg.Path, msg.Line, msg.Col)
	}
	if len(msg.Refs) != 1 || msg.Refs[0].Path != path || msg.Refs[0].Line != 2 || msg.Refs[0].Col != 4 {
		t.Fatalf("refs = %#v", msg.Refs)
	}
	if msg.Refs[0].Preview != "var foo = 1" {
		t.Fatalf("preview = %q", msg.Refs[0].Preview)
	}
	if msg.Refresh == nil {
		t.Fatal("the panel result must carry a refresh continuation")
	}

	// The refresh continuation re-runs the request at the stored origin and
	// delivers a fresh UsagesMsg with the same symbol.
	msg.Refresh()
	again := waitUsages(t, msgs)
	if again.Symbol != "foo" || len(again.Refs) != 1 {
		t.Fatalf("refresh result = %#v", again)
	}
	if reqs != 2 {
		t.Fatalf("server saw %d reference requests, want 2", reqs)
	}
}

// waitUsages pulls the next UsagesMsg off the sender channel.
func waitUsages(t *testing.T, msgs chan tea.Msg) ilsp.UsagesMsg {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-msgs:
			if u, ok := m.(ilsp.UsagesMsg); ok {
				return u
			}
		case <-deadline:
			t.Fatal("timed out waiting for a UsagesMsg")
		}
	}
}

// TestIdentAt covers the symbol capture used for the pane title (#1155).
func TestIdentAt(t *testing.T) {
	cases := []struct {
		text string
		col  int
		want string
	}{
		{"var foo = 1", 4, "foo"},
		{"var foo = 1", 5, "foo"},
		{"var foo = 1", 7, "foo"}, // cursor just past the word
		{"var foo = 1", 3, "var"}, // on the space right after a word
		{"foo", 0, "foo"},
		{"foo", 3, "foo"},
		{"a_b1 c", 1, "a_b1"},
		{"", 0, ""},
		{"  +  ", 2, ""},
		{"höhe=1", 2, "höhe"},
	}
	for _, tc := range cases {
		if got := identAt(tc.text, tc.col); got != tc.want {
			t.Errorf("identAt(%q, %d) = %q, want %q", tc.text, tc.col, got, tc.want)
		}
	}
}
