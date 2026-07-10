package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
)

// rwc is a duplex stream made of a reader and a writer.
type rwc struct {
	io.Reader
	io.Writer
}

func (rwc) Close() error { return nil }

// fakeServer is a scripted LSP peer over a pipe: it reads framed requests and
// replies from a method→responder table. Notifications it can push on demand.
type fakeServer struct {
	in  *bufio.Reader
	out io.Writer
}

func newClientWithFake(t *testing.T, responders map[string]func(params json.RawMessage) any) (*Client, *fakeServer) {
	t.Helper()
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	cli := rwc{Reader: cr, Writer: cw}
	srv := &fakeServer{in: bufio.NewReader(sr), out: sw}

	go srv.serve(responders)

	conn := jsonrpc.NewConn(cli, jsonrpc.Handler{})
	c := New(conn)
	t.Cleanup(func() { conn.Close() })
	return c, srv
}

// serve reads requests and answers them; unknown methods get a null result.
func (s *fakeServer) serve(responders map[string]func(params json.RawMessage) any) {
	for {
		payload, err := readFrame(s.in)
		if err != nil {
			return
		}
		var req struct {
			ID     *json.RawMessage `json:"id"`
			Method string           `json:"method"`
			Params json.RawMessage  `json:"params"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue // a notification; nothing to answer
		}
		var result any = nil
		if r, ok := responders[req.Method]; ok {
			result = r(req.Params)
		}
		resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, string(*req.ID), mustJSON(result))
		_ = writeFrameRaw(s.out, []byte(resp))
	}
}

// push sends a server→client notification.
func (s *fakeServer) push(method string, params any) {
	msg := fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"params":%s}`, method, mustJSON(params))
	_ = writeFrameRaw(s.out, []byte(msg))
}

func mustJSON(v any) string {
	if v == nil {
		return "null"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// --- minimal framing (test-local, mirrors jsonrpc internals) ---

func writeFrameRaw(w io.Writer, payload []byte) error {
	if _, err := io.WriteString(w, fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, _ = strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
		}
	}
	if n < 0 {
		return nil, io.ErrUnexpectedEOF
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func ctx2s() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}

// --- tests ---

func TestInitializeNegotiatesCapabilities(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"initialize": func(json.RawMessage) any {
			return protocol.InitializeResult{
				Capabilities: protocol.ServerCapabilities{
					PositionEncoding:   protocol.EncodingUTF8,
					TextDocumentSync:   json.RawMessage(`2`),
					CompletionProvider: &protocol.CompletionOptions{TriggerCharacters: []string{"."}},
					HoverProvider:      json.RawMessage(`true`),
					DefinitionProvider: json.RawMessage(`true`),
				},
				ServerInfo: &protocol.ServerInfo{Name: "fakels"},
			}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	if _, err := c.Initialize(ctx, InitParams{RootURI: "file:///tmp"}); err != nil {
		t.Fatal(err)
	}
	caps := c.Caps()
	if caps.Encoding != protocol.EncodingUTF8 {
		t.Errorf("encoding = %q", caps.Encoding)
	}
	if caps.SyncKind != protocol.SyncIncremental {
		t.Errorf("sync = %d, want incremental", caps.SyncKind)
	}
	if !caps.Completion || !caps.Hover || !caps.Definition {
		t.Errorf("expected all features gated on: %+v", caps)
	}
	if len(caps.CompletionTriggers) != 1 || caps.CompletionTriggers[0] != "." {
		t.Errorf("triggers = %v", caps.CompletionTriggers)
	}
	if !c.Ready() {
		t.Error("client should be ready after initialize")
	}
}

func TestCompletionNormalisesList(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/completion": func(json.RawMessage) any {
			return protocol.CompletionList{Items: []protocol.CompletionItem{{Label: "Println"}}}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	items, err := c.Completion(ctx, protocol.CompletionParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Label != "Println" {
		t.Fatalf("items = %+v", items)
	}
}

func TestDefinitionNormalisesSingleLocation(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/definition": func(json.RawMessage) any {
			return protocol.Location{URI: "file:///tmp/a.go", Range: protocol.Range{Start: protocol.Position{Line: 3}}}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	locs, err := c.Definition(ctx, protocol.DefinitionParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 || locs[0].URI != "file:///tmp/a.go" {
		t.Fatalf("locs = %+v", locs)
	}
}

func TestReferencesNormalisesLocations(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/references": func(json.RawMessage) any {
			return []protocol.Location{
				{URI: "file:///tmp/a.go", Range: protocol.Range{Start: protocol.Position{Line: 3}}},
				{URI: "file:///tmp/b.go", Range: protocol.Range{Start: protocol.Position{Line: 7}}},
			}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	locs, err := c.References(ctx, protocol.ReferenceParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 2 || locs[1].URI != "file:///tmp/b.go" {
		t.Fatalf("locs = %+v", locs)
	}
}

func TestReferencesNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/references": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	locs, err := c.References(ctx, protocol.ReferenceParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 0 {
		t.Fatalf("null result should yield no locations, got %+v", locs)
	}
}

func TestHoverNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/hover": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	h, err := c.Hover(ctx, protocol.HoverParams{})
	if err != nil {
		t.Fatal(err)
	}
	if h != nil {
		t.Fatalf("hover = %+v, want nil", h)
	}
}
