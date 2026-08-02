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

// inheritanceConnector dials an in-memory server for the inheritance commands
// (#1451). typeHierarchy toggles the capability; implLocs answers every
// textDocument/implementation, superItems every typeHierarchy/supertypes.
func inheritanceConnector(typeHierarchy bool, implLocs func() []protocol.Location, superItems func() []protocol.TypeHierarchyItem) manager.Connector {
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
					caps := protocol.ServerCapabilities{
						TextDocumentSync:       json.RawMessage(`1`),
						ImplementationProvider: json.RawMessage(`true`),
					}
					if typeHierarchy {
						caps.TypeHierarchyProvider = json.RawMessage(`true`)
					}
					_ = srv.Respond(id, protocol.InitializeResult{Capabilities: caps}, nil)
				case "textDocument/implementation":
					_ = srv.Respond(id, implLocs(), nil)
				case "textDocument/prepareTypeHierarchy":
					_ = srv.Respond(id, []protocol.TypeHierarchyItem{{
						Name: "Circle", URI: "file:///tmp/na.go", Data: json.RawMessage(`"tok"`),
					}}, nil)
				case "typeHierarchy/supertypes":
					_ = srv.Respond(id, superItems(), nil)
				case "typeHierarchy/subtypes":
					_ = srv.Respond(id, []protocol.TypeHierarchyItem{{Name: "Sub", URI: "file:///tmp/na.go"}}, nil)
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

// inheritanceBridge builds a bridge over the scripted connector with one open
// Go document and returns it plus the sender channel.
func inheritanceBridge(t *testing.T, conn manager.Connector, cfg ...host.Config) (*bridge, string, chan tea.Msg) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "package main\n\ntype Circle struct{}\n"
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
	msgs := make(chan tea.Msg, 16)
	var hc host.Config
	if len(cfg) > 0 {
		hc = cfg[0]
	}
	h := host.New(hc)
	h.SetSender(func(m tea.Msg) { msgs <- m })
	b := &bridge{h: h, mgr: manager.New(resolve, conn, manager.Callbacks{})}
	if err := b.mgr.Open(path, "go", content); err != nil {
		t.Fatalf("Open: %v", err)
	}
	b.setCur(path, 2, 6)
	return b, path, msgs
}

func waitMsg[T tea.Msg](t *testing.T, msgs chan tea.Msg) T {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-msgs:
			if v, ok := m.(T); ok {
				return v
			}
		case <-deadline:
			var zero T
			t.Fatalf("timed out waiting for %T", zero)
			return zero
		}
	}
}

// TestImplementationsDelivered covers lsp.implementations: the locations
// arrive as an ImplementationsMsg with converted references.
func TestImplementationsDelivered(t *testing.T) {
	var path string
	implLocs := func() []protocol.Location {
		return []protocol.Location{{URI: protocol.PathToURI(path), Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 5}}}}
	}
	b, p, msgs := inheritanceBridge(t, inheritanceConnector(false, implLocs, nil))
	path = p

	b.implementations(b.h)
	msg := waitMsg[ilsp.ImplementationsMsg](t, msgs)
	if msg.Super {
		t.Fatal("implementations must not be flagged Super")
	}
	if len(msg.Refs) != 1 || msg.Refs[0].Path != path || msg.Refs[0].Line != 2 || msg.Refs[0].Col != 5 {
		t.Fatalf("refs = %#v", msg.Refs)
	}
	if msg.Refs[0].Preview != "type Circle struct{}" {
		t.Fatalf("preview = %q", msg.Refs[0].Preview)
	}
}

// TestImplementationsEmptyToasts covers #858: an empty answer becomes an info
// toast, never silence.
func TestImplementationsEmptyToasts(t *testing.T) {
	b, _, msgs := inheritanceBridge(t, inheritanceConnector(false, func() []protocol.Location { return nil }, nil))
	b.implementations(b.h)
	st := waitMsg[ilsp.ServerStatusMsg](t, msgs)
	if st.Text != "no implementations found" {
		t.Fatalf("toast = %q", st.Text)
	}
}

// TestGoToSuperViaTypeHierarchy covers the capability-first path: prepare +
// supertypes answer, delivered with Super set.
func TestGoToSuperViaTypeHierarchy(t *testing.T) {
	var path string
	supers := func() []protocol.TypeHierarchyItem {
		return []protocol.TypeHierarchyItem{{
			Name: "Shape", URI: protocol.PathToURI(path),
			SelectionRange: protocol.Range{Start: protocol.Position{Line: 2, Character: 5}},
		}}
	}
	b, p, msgs := inheritanceBridge(t, inheritanceConnector(true, func() []protocol.Location { return nil }, supers))
	path = p

	b.goToSuper(b.h)
	msg := waitMsg[ilsp.ImplementationsMsg](t, msgs)
	if !msg.Super {
		t.Fatal("goToSuper must flag Super")
	}
	if len(msg.Refs) != 1 || msg.Refs[0].Path != path || msg.Refs[0].Line != 2 {
		t.Fatalf("refs = %#v", msg.Refs)
	}
}

// TestGoToSuperFallsBackToImplementation covers the no-typeHierarchy path: the
// bidirectional implementation answer stands in for supertypes.
func TestGoToSuperFallsBackToImplementation(t *testing.T) {
	var path string
	implLocs := func() []protocol.Location {
		return []protocol.Location{{URI: protocol.PathToURI(path), Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 8}}}}
	}
	b, p, msgs := inheritanceBridge(t, inheritanceConnector(false, implLocs, nil))
	path = p

	b.goToSuper(b.h)
	msg := waitMsg[ilsp.ImplementationsMsg](t, msgs)
	if !msg.Super || len(msg.Refs) != 1 || msg.Refs[0].Line != 0 {
		t.Fatalf("msg = %#v", msg)
	}
}

// TestGoToSuperEmptyToasts: both paths empty → toast.
func TestGoToSuperEmptyToasts(t *testing.T) {
	b, _, msgs := inheritanceBridge(t, inheritanceConnector(true, func() []protocol.Location { return nil },
		func() []protocol.TypeHierarchyItem { return nil }))
	b.goToSuper(b.h)
	st := waitMsg[ilsp.ServerStatusMsg](t, msgs)
	if st.Text != "no super declaration found" {
		t.Fatalf("toast = %q", st.Text)
	}
}

// TestTypeHierarchyRootsAndFetch covers lsp.typeHierarchy: roots arrive with a
// working Fetch continuation that expands supertypes and subtypes.
func TestTypeHierarchyRootsAndFetch(t *testing.T) {
	supers := func() []protocol.TypeHierarchyItem {
		return []protocol.TypeHierarchyItem{{Name: "Shape", URI: "file:///tmp/na.go"}}
	}
	b, path, msgs := inheritanceBridge(t, inheritanceConnector(true, func() []protocol.Location { return nil }, supers))

	b.typeHierarchy(b.h)
	msg := waitMsg[ilsp.TypeHierarchyMsg](t, msgs)
	if msg.Path != path || len(msg.Roots) != 1 || msg.Roots[0].Name != "Circle" {
		t.Fatalf("msg = %#v", msg)
	}
	if msg.Fetch == nil {
		t.Fatal("roots must carry a fetch continuation")
	}

	msg.Fetch(7, msg.Roots[0].Item, true)
	up := waitMsg[ilsp.TypeHierarchyItemsMsg](t, msgs)
	if up.ReqID != 7 || !up.Supertypes || len(up.Items) != 1 || up.Items[0].Name != "Shape" {
		t.Fatalf("supertypes expansion = %#v", up)
	}

	msg.Fetch(8, msg.Roots[0].Item, false)
	down := waitMsg[ilsp.TypeHierarchyItemsMsg](t, msgs)
	if down.ReqID != 8 || down.Supertypes || len(down.Items) != 1 || down.Items[0].Name != "Sub" {
		t.Fatalf("subtypes expansion = %#v", down)
	}
}

// marksConnector dials an in-memory server for the gutter-mark batch: one
// interface symbol whose implementation probe answers non-empty.
func marksConnector() manager.Connector {
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
						TextDocumentSync:       json.RawMessage(`1`),
						DocumentSymbolProvider: json.RawMessage(`true`),
						ImplementationProvider: json.RawMessage(`true`),
					}}, nil)
				case "textDocument/documentSymbol":
					_ = srv.Respond(id, json.RawMessage(`[{"name":"Shape","kind":11,
						"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":20}},
						"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":10}}}]`), nil)
				case "textDocument/implementation":
					_ = srv.Respond(id, []protocol.Location{{URI: "file:///tmp/other.go"}}, nil)
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

// TestRequestInheritanceMarksDelivers covers the passive batch (#1453): the
// coalesced request delivers a version-stamped InheritanceMarksMsg.
func TestRequestInheritanceMarksDelivers(t *testing.T) {
	b, path, msgs := inheritanceBridge(t, marksConnector())
	b.requestInheritanceMarks(path)
	msg := waitMsg[ilsp.InheritanceMarksMsg](t, msgs)
	if msg.Path != path || msg.Version != 1 {
		t.Fatalf("msg = %#v", msg)
	}
	if len(msg.Marks) != 1 || msg.Marks[0].Line != 2 || msg.Marks[0].Kind != ilsp.InheritanceImplemented {
		t.Fatalf("marks = %#v", msg.Marks)
	}
}

// TestInheritanceMarksToggleGatesTraffic: the config switch off means no
// request and no message.
func TestInheritanceMarksToggleGatesTraffic(t *testing.T) {
	b, path, msgs := inheritanceBridge(t, marksConnector(), host.MapConfig{"editor.marks.inheritance": "false"})
	b.requestInheritanceMarks(path)
	select {
	case m := <-msgs:
		if _, ok := m.(ilsp.InheritanceMarksMsg); ok {
			t.Fatal("toggle off must suppress the batch")
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// TestTypeHierarchyUnsupportedToasts covers the capability gate wording.
func TestTypeHierarchyUnsupportedToasts(t *testing.T) {
	b, _, msgs := inheritanceBridge(t, inheritanceConnector(false, func() []protocol.Location { return nil }, nil))
	b.typeHierarchy(b.h)
	st := waitMsg[ilsp.ServerStatusMsg](t, msgs)
	if st.Text != "type hierarchy unavailable: the language server does not support it" {
		t.Fatalf("toast = %q", st.Text)
	}
}
