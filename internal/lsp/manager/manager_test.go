package manager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"ike/internal/editor/buffer"
	"ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
)

type rwc struct {
	io.Reader
	io.Writer
}

func (rwc) Close() error { return nil }

// fakeConnector returns a Connector backed by an in-memory scripted server. The
// server answers initialize with full capabilities, echoes completion, and pushes
// a diagnostic when it sees didOpen.
func fakeConnector() Connector {
	return func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), error) {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		cli := rwc{Reader: cr, Writer: cw}
		go runFakeServer(bufio.NewReader(sr), sw)
		conn := jsonrpc.NewConn(cli, handler)
		c := client.New(conn)
		return c, func() { conn.Close() }, nil
	}
}

func runFakeServer(in *bufio.Reader, out io.Writer) {
	for {
		payload, err := readFrame(in)
		if err != nil {
			return
		}
		var msg struct {
			ID     *json.RawMessage `json:"id"`
			Method string           `json:"method"`
			Params json.RawMessage  `json:"params"`
		}
		if json.Unmarshal(payload, &msg) != nil {
			continue
		}
		switch {
		case msg.Method == "initialize":
			result := protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{
				PositionEncoding:   protocol.EncodingUTF16,
				TextDocumentSync:   json.RawMessage(`1`),
				CompletionProvider: &protocol.CompletionOptions{TriggerCharacters: []string{"."}},
				HoverProvider:      json.RawMessage(`true`),
				DefinitionProvider: json.RawMessage(`true`),
				ReferencesProvider: json.RawMessage(`true`),

				DocumentFormattingProvider:      json.RawMessage(`true`),
				DocumentRangeFormattingProvider: json.RawMessage(`true`),
			}}
			respond(out, msg.ID, result)
		case msg.Method == "textDocument/references":
			// Echo the request option so the test can assert it round-trips.
			var p protocol.ReferenceParams
			_ = json.Unmarshal(msg.Params, &p)
			locs := []protocol.Location{{
				URI:   "file:///tmp/other.go",
				Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 1}},
			}}
			if p.Context.IncludeDeclaration {
				locs = append(locs, protocol.Location{
					URI:   p.TextDocument.URI,
					Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 0}},
				})
			}
			respond(out, msg.ID, locs)
		case msg.Method == "textDocument/formatting":
			// One edit whose character offsets are UTF-16 units past an emoji,
			// so the conversion back to rune columns is observable.
			var p protocol.DocumentFormattingParams
			_ = json.Unmarshal(msg.Params, &p)
			respond(out, msg.ID, []protocol.TextEdit{{
				Range:   protocol.Range{Start: protocol.Position{Line: 0, Character: 10}, End: protocol.Position{Line: 0, Character: 10}},
				NewText: fmt.Sprintf("/*tab=%d spaces=%v*/", p.Options.TabSize, p.Options.InsertSpaces),
			}})
		case msg.Method == "textDocument/rangeFormatting":
			// Echo the requested range back as the edit range.
			var p protocol.DocumentRangeFormattingParams
			_ = json.Unmarshal(msg.Params, &p)
			respond(out, msg.ID, []protocol.TextEdit{{Range: p.Range, NewText: "X"}})
		case msg.Method == "textDocument/completion":
			respond(out, msg.ID, protocol.CompletionList{Items: []protocol.CompletionItem{{Label: "Println"}}})
		case msg.Method == "textDocument/didOpen":
			// Push a diagnostic for the opened doc.
			var p protocol.DidOpenTextDocumentParams
			_ = json.Unmarshal(msg.Params, &p)
			notify(out, "textDocument/publishDiagnostics", protocol.PublishDiagnosticsParams{
				URI:     p.TextDocument.URI,
				Version: 1,
				Diagnostics: []protocol.Diagnostic{{
					Range:    protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 3}},
					Severity: protocol.SeverityError,
					Message:  "boom",
				}},
			})
		case msg.ID != nil:
			respond(out, msg.ID, nil)
		}
	}
}

func respond(out io.Writer, id *json.RawMessage, result any) {
	if id == nil {
		return
	}
	b, _ := json.Marshal(result)
	if result == nil {
		b = []byte("null")
	}
	_ = writeFrame(out, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, string(*id), string(b))))
}

func notify(out io.Writer, method string, params any) {
	b, _ := json.Marshal(params)
	_ = writeFrame(out, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"params":%s}`, method, string(b))))
}

func writeFrame(w io.Writer, payload []byte) error {
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

func resolver(spec lsp.ServerSpec) func(string) (lsp.ServerSpec, bool) {
	return func(lang string) (lsp.ServerSpec, bool) {
		if lang == spec.Language {
			return spec, true
		}
		return lsp.ServerSpec{}, false
	}
}

func TestManagerOpenSpawnsAndDiagnostics(t *testing.T) {
	diagCh := make(chan protocol.PublishDiagnosticsParams, 1)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{
		Diagnostics: func(path string, p protocol.PublishDiagnosticsParams, lines []string, enc string) {
			diagCh <- p
		},
	})
	defer m.Shutdown()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	path := filepath.Join(dir, "main.go")

	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-diagCh:
		if len(p.Diagnostics) != 1 || p.Diagnostics[0].Message != "boom" {
			t.Fatalf("diags = %+v", p.Diagnostics)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no diagnostics received")
	}
}

func TestManagerCompletion(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	items, err := m.Completion(context.Background(), path, buffer.Position{Line: 0, Col: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Label != "Println" {
		t.Fatalf("items = %+v", items)
	}
}

func TestManagerReferences(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	locs, err := m.References(context.Background(), path, buffer.Position{Line: 0, Col: 0}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 2 {
		t.Fatalf("includeDeclaration should round-trip to the server, locs = %+v", locs)
	}
	if locs[0].URI != "file:///tmp/other.go" || locs[0].Range.Start.Line != 2 {
		t.Errorf("first loc wrong: %+v", locs[0])
	}
	// Excluding the declaration drops the echoed extra location.
	locs, err = m.References(context.Background(), path, buffer.Position{Line: 0, Col: 0}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 {
		t.Fatalf("without declaration expected 1 loc, got %+v", locs)
	}
}

func TestManagerFormatConvertsPositionsAndOptions(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	// "a🙂bcdefghij": the emoji is 2 UTF-16 units, so unit offset 10 is rune
	// column 9.
	if err := m.Open(path, "go", "a🙂bcdefghij"); err != nil {
		t.Fatal(err)
	}
	edits, err := m.Format(context.Background(), path, protocol.FormattingOptions{TabSize: 3, InsertSpaces: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 {
		t.Fatalf("edits = %+v", edits)
	}
	if edits[0].StartCol != 9 || edits[0].StartLine != 0 {
		t.Errorf("UTF-16 offset should convert to rune col 9, got %+v", edits[0])
	}
	if edits[0].Text != "/*tab=3 spaces=true*/" {
		t.Errorf("FormattingOptions should reach the server, got %q", edits[0].Text)
	}
}

func TestManagerFormatRangeRoundTrip(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "one\ntwo\nthree"); err != nil {
		t.Fatal(err)
	}
	edits, err := m.FormatRange(context.Background(), path,
		buffer.Position{Line: 0, Col: 1}, buffer.Position{Line: 2, Col: 2},
		protocol.FormattingOptions{TabSize: 4, InsertSpaces: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 {
		t.Fatalf("edits = %+v", edits)
	}
	e := edits[0]
	if e.StartLine != 0 || e.StartCol != 1 || e.EndLine != 2 || e.EndCol != 2 {
		t.Errorf("range should round-trip through both conversions, got %+v", e)
	}
}

func TestManagerUnknownLanguageNoOp(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake"}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()
	if err := m.Open("/tmp/a.rb", "ruby", "x"); err != nil {
		t.Fatalf("unknown language should be a no-op, got %v", err)
	}
}

func TestDetectRoot(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644)
	got := detectRoot(filepath.Join(sub, "main.go"), []string{"go.mod"})
	if got != dir {
		t.Fatalf("detectRoot = %q, want %q", got, dir)
	}
}
