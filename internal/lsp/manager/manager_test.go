package manager

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

// fakeOpts tunes the scripted server: the advertised sync kind and an
// optional channel receiving every didChange notification.
type fakeOpts struct {
	syncKind   int
	didChanges chan protocol.DidChangeTextDocumentParams
	didOpens   chan protocol.DidOpenTextDocumentParams
	didCloses  chan protocol.DidCloseTextDocumentParams
	// noCallHierarchy withholds the callHierarchyProvider capability, so the
	// manager's gate is observable (#173).
	noCallHierarchy bool
	// noRename withholds the renameProvider capability (intelephense without a
	// licence does this), so the ErrRenameUnsupported gate is observable (#426).
	noRename bool
	// noDocumentHighlight withholds the documentHighlightProvider capability,
	// so the manager's gate is observable (#172).
	noDocumentHighlight bool
	// noInlayHint withholds the inlayHintProvider capability, so the
	// manager's gate is observable (#171).
	noInlayHint bool
}

// fakeConnector returns a Connector backed by an in-memory scripted server. The
// server answers initialize with full capabilities, echoes completion, and pushes
// a diagnostic when it sees didOpen.
func fakeConnector() Connector { return fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull}) }

// callHierarchyCap is the initialize capability the fake advertises unless
// the options withhold it.
func callHierarchyCap(opts fakeOpts) json.RawMessage {
	if opts.noCallHierarchy {
		return nil
	}
	return json.RawMessage(`true`)
}

// documentHighlightCap is the initialize capability the fake advertises
// unless the options withhold it.
func documentHighlightCap(opts fakeOpts) json.RawMessage {
	if opts.noDocumentHighlight {
		return nil
	}
	return json.RawMessage(`true`)
}

// inlayHintCap is the initialize capability the fake advertises unless the
// options withhold it.
func inlayHintCap(opts fakeOpts) json.RawMessage {
	if opts.noInlayHint {
		return nil
	}
	return json.RawMessage(`true`)
}

// renameCap is the rename capability the fake advertises unless the options
// withhold it.
func renameCap(opts fakeOpts) json.RawMessage {
	if opts.noRename {
		return json.RawMessage(`false`)
	}
	return json.RawMessage(`{"prepareProvider":true}`)
}

// fakeConnectorOpts is fakeConnector with the server behaviour tuned.
func fakeConnectorOpts(opts fakeOpts) Connector {
	return func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), error) {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		cli := rwc{Reader: cr, Writer: cw}
		go runFakeServer(bufio.NewReader(sr), sw, opts)
		conn := jsonrpc.NewConn(cli, handler)
		c := client.New(conn)
		return c, func() { conn.Close() }, nil
	}
}

func runFakeServer(in *bufio.Reader, out io.Writer, opts fakeOpts) {
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
				TextDocumentSync:   json.RawMessage(strconv.Itoa(opts.syncKind)),
				CompletionProvider: &protocol.CompletionOptions{TriggerCharacters: []string{"."}},
				HoverProvider:      json.RawMessage(`true`),
				DefinitionProvider: json.RawMessage(`true`),
				ReferencesProvider: json.RawMessage(`true`),

				DocumentFormattingProvider:      json.RawMessage(`true`),
				DocumentRangeFormattingProvider: json.RawMessage(`true`),
				RenameProvider:                  renameCap(opts),
				CodeActionProvider:              json.RawMessage(`true`),
				SignatureHelpProvider:           &protocol.SignatureHelpOptions{TriggerCharacters: []string{"(", ","}},
				SemanticTokensProvider: &protocol.SemanticTokensOptions{
					Legend: protocol.SemanticTokensLegend{TokenTypes: []string{"keyword", "function"}},
					Full:   json.RawMessage(`{"delta":true}`),
				},
				ExecuteCommandProvider: json.RawMessage(`{"commands":["test.fix"]}`),
				CallHierarchyProvider:  callHierarchyCap(opts),

				DocumentHighlightProvider: documentHighlightCap(opts),
				InlayHintProvider:         inlayHintCap(opts),
			}}
			respond(out, msg.ID, result)
		case msg.Method == "textDocument/definition":
			// One location in the requested doc covering its first 6 units, so
			// fragment→host URI/range rewriting is observable.
			var p protocol.DefinitionParams
			_ = json.Unmarshal(msg.Params, &p)
			respond(out, msg.ID, []protocol.Location{{
				URI:   p.TextDocument.URI,
				Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 6}},
			}})
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
		case msg.Method == "textDocument/documentHighlight":
			// Two occurrences in the requested doc with UTF-16 unit offsets
			// (an emoji in the text makes the rune conversion observable) and
			// distinct kinds.
			respond(out, msg.ID, []protocol.DocumentHighlight{
				{Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 6}}, Kind: protocol.HighlightRead},
				{Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 7}, End: protocol.Position{Line: 0, Character: 10}}, Kind: protocol.HighlightWrite},
			})
		case msg.Method == "textDocument/inlayHint":
			// Two hints in UTF-16 unit offsets, deliberately out of document
			// order so the manager's sort is observable; one label is a parts
			// array, the other a plain string.
			respond(out, msg.ID, json.RawMessage(`[
				{"position":{"line":0,"character":7},"label":[{"value":"n:"}],"kind":2,"paddingRight":true},
				{"position":{"line":0,"character":3},"label":"int","kind":1,"paddingLeft":true}
			]`))
		case msg.Method == "textDocument/prepareCallHierarchy":
			// One item named after the request position, so the round-trip is
			// observable.
			var p protocol.CallHierarchyPrepareParams
			_ = json.Unmarshal(msg.Params, &p)
			respond(out, msg.ID, []protocol.CallHierarchyItem{{
				Name:           fmt.Sprintf("sym@%d:%d", p.Position.Line, p.Position.Character),
				URI:            string(p.TextDocument.URI),
				SelectionRange: protocol.Range{Start: protocol.Position{Line: 1, Character: 0}},
				Data:           json.RawMessage(`"token"`),
			}})
		case msg.Method == "callHierarchy/incomingCalls":
			// Echo the item's opaque data into the caller name so the test can
			// assert it round-trips verbatim.
			var p protocol.CallHierarchyCallsParams
			_ = json.Unmarshal(msg.Params, &p)
			respond(out, msg.ID, []protocol.CallHierarchyIncomingCall{{
				From: protocol.CallHierarchyItem{
					Name: "caller-of-" + p.Item.Name + "-" + string(p.Item.Data),
					URI:  "file:///tmp/caller.go",
				},
				FromRanges: []protocol.Range{{Start: protocol.Position{Line: 5, Character: 2}}},
			}})
		case msg.Method == "callHierarchy/outgoingCalls":
			var p protocol.CallHierarchyCallsParams
			_ = json.Unmarshal(msg.Params, &p)
			respond(out, msg.ID, []protocol.CallHierarchyOutgoingCall{{
				To:         protocol.CallHierarchyItem{Name: "callee", URI: "file:///tmp/callee.go"},
				FromRanges: []protocol.Range{{Start: protocol.Position{Line: 3, Character: 1}}},
			}})
		case msg.Method == "textDocument/semanticTokens/full":
			// keyword at 0:0 len4.
			respond(out, msg.ID, protocol.SemanticTokens{ResultID: "r1", Data: []uint32{0, 0, 4, 0, 0}})
		case msg.Method == "textDocument/semanticTokens/full/delta":
			var p protocol.SemanticTokensDeltaParams
			_ = json.Unmarshal(msg.Params, &p)
			if p.PreviousResultID != "r1" {
				respond(out, msg.ID, nil)
				break
			}
			// Append a function token on the next line.
			respond(out, msg.ID, protocol.SemanticTokensDelta{ResultID: "r2", Edits: []protocol.SemanticTokensEdit{{Start: 5, DeleteCount: 0, Data: []uint32{1, 0, 4, 1, 0}}}})
		case msg.Method == "textDocument/signatureHelp":
			respond(out, msg.ID, protocol.SignatureHelp{Signatures: []protocol.SignatureInformation{{Label: "Greet(name string)"}}})
		case msg.Method == "textDocument/codeAction":
			// Echo how many context diagnostics arrived in the title.
			var p protocol.CodeActionParams
			_ = json.Unmarshal(msg.Params, &p)
			respond(out, msg.ID, []protocol.CodeAction{{
				Title: fmt.Sprintf("fix (%d diags)", len(p.Context.Diagnostics)),
				Kind:  "quickfix",
			}})
		case msg.Method == "workspace/executeCommand":
			// Effect arrives as a server->client applyEdit request first.
			var p protocol.ExecuteCommandParams
			_ = json.Unmarshal(msg.Params, &p)
			_ = writeFrame(out, []byte(`{"jsonrpc":"2.0","id":9999,"method":"workspace/applyEdit","params":{"edit":{"changes":{"file:///tmp/applyedit.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"x"}]}}}}`))
			respond(out, msg.ID, nil)
		case msg.Method == "textDocument/prepareRename":
			// Reject position line 9; otherwise offer the first 3 characters.
			var p protocol.PrepareRenameParams
			_ = json.Unmarshal(msg.Params, &p)
			if p.Position.Line == 9 {
				respond(out, msg.ID, nil)
				break
			}
			respond(out, msg.ID, protocol.Range{
				Start: protocol.Position{Line: p.Position.Line, Character: 0},
				End:   protocol.Position{Line: p.Position.Line, Character: 3},
			})
		case msg.Method == "textDocument/rename":
			// Rename touches the requested doc and a sibling on disk.
			var p protocol.RenameParams
			_ = json.Unmarshal(msg.Params, &p)
			edit := protocol.TextEdit{
				Range:   protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 3}},
				NewText: p.NewName,
			}
			other := strings.Replace(string(p.TextDocument.URI), "main.go", "other.go", 1)
			respond(out, msg.ID, protocol.WorkspaceEdit{Changes: map[string][]protocol.TextEdit{
				string(p.TextDocument.URI): {edit},
				other:                      {edit},
			}})
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
			// The edit covers the document's first 3 units so range mapping
			// (fragment → host) is observable.
			respond(out, msg.ID, protocol.CompletionList{Items: []protocol.CompletionItem{{
				Label: "Println",
				TextEdit: &protocol.TextEdit{
					Range:   protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 3}},
					NewText: "Println",
				},
			}}})
		case msg.Method == "textDocument/hover":
			// Echo the requested position's line in the contents and cover the
			// first 6 units, so routing and range mapping are observable.
			var p protocol.HoverParams
			_ = json.Unmarshal(msg.Params, &p)
			respond(out, msg.ID, map[string]any{
				"contents": fmt.Sprintf("hover@%d:%d", p.Position.Line, p.Position.Character),
				"range": protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 6},
				},
			})
		case msg.Method == "textDocument/didChange":
			if opts.didChanges != nil {
				var p protocol.DidChangeTextDocumentParams
				_ = json.Unmarshal(msg.Params, &p)
				opts.didChanges <- p
			}
		case msg.Method == "textDocument/didClose":
			if opts.didCloses != nil {
				var p protocol.DidCloseTextDocumentParams
				_ = json.Unmarshal(msg.Params, &p)
				opts.didCloses <- p
			}
		case msg.Method == "textDocument/didOpen":
			// Push a diagnostic for the opened doc.
			var p protocol.DidOpenTextDocumentParams
			_ = json.Unmarshal(msg.Params, &p)
			if opts.didOpens != nil {
				opts.didOpens <- p
			}
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

// TestManagerDocumentHighlight converts the server's UTF-16 highlight ranges
// to editor rune coordinates and keeps the kinds (#172).
func TestManagerDocumentHighlight(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	// "a🙂bcdefghij": the emoji is 2 UTF-16 units, so unit offset 6 is rune
	// column 5 and unit 7 is rune 6.
	if err := m.Open(path, "go", "a🙂bcdefghij"); err != nil {
		t.Fatal(err)
	}
	hs, err := m.DocumentHighlight(context.Background(), path, buffer.Position{Line: 0, Col: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 {
		t.Fatalf("hs = %+v, want 2", hs)
	}
	if hs[0].Range.Start.Col != 0 || hs[0].Range.End.Col != 5 || hs[0].Kind != protocol.HighlightRead {
		t.Errorf("first highlight = %+v, want runes [0,5) read", hs[0])
	}
	if hs[1].Range.Start.Col != 6 || hs[1].Range.End.Col != 9 || hs[1].Kind != protocol.HighlightWrite {
		t.Errorf("second highlight = %+v, want runes [6,9) write", hs[1])
	}
}

// TestManagerDocumentHighlightGated yields nothing when the server lacks the
// capability — no request, no error.
func TestManagerDocumentHighlightGated(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, noDocumentHighlight: true}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	hs, err := m.DocumentHighlight(context.Background(), path, buffer.Position{Line: 0, Col: 0})
	if err != nil || hs != nil {
		t.Fatalf("gated request should be a no-op, got %+v, %v", hs, err)
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

func TestManagerPrepareRename(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "abcdef\nsecond"); err != nil {
		t.Fatal(err)
	}
	ph, ok, err := m.PrepareRename(context.Background(), path, buffer.Position{Line: 0, Col: 1})
	if err != nil || !ok {
		t.Fatalf("prepare should accept, got ok=%v err=%v", ok, err)
	}
	if ph != "abc" {
		t.Errorf("placeholder should be the ranged text, got %q", ph)
	}
	if _, ok, _ := m.PrepareRename(context.Background(), path, buffer.Position{Line: 9, Col: 0}); ok {
		t.Error("rejected position should report ok=false")
	}
}

// TestManagerPrepareRenameUnsupported guards #426: a server without the rename
// capability (intelephense free) reports ErrRenameUnsupported, distinct from a
// rejected position, so the UI can say the feature is missing.
func TestManagerPrepareRenameUnsupported(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, noRename: true}), Callbacks{})
	defer m.Shutdown()

	path := filepath.Join(t.TempDir(), "main.go")
	if err := m.Open(path, "go", "abcdef"); err != nil {
		t.Fatal(err)
	}
	_, ok, err := m.PrepareRename(context.Background(), path, buffer.Position{Line: 0, Col: 1})
	if ok {
		t.Error("unsupported rename must not report ok")
	}
	if !errors.Is(err, ErrRenameUnsupported) {
		t.Fatalf("err = %v, want ErrRenameUnsupported", err)
	}
}

func TestManagerRenameSplitsOpenAndDisk(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	other := filepath.Join(dir, "other.go")
	if err := os.WriteFile(other, []byte("old text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(path, "go", "old text"); err != nil {
		t.Fatal(err)
	}
	files, err := m.Rename(context.Background(), path, buffer.Position{Line: 0, Col: 0}, "new")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	// Sorted by path: main.go before other.go.
	if !files[0].Open || files[0].Path != path {
		t.Errorf("open doc should be flagged Open: %+v", files[0])
	}
	if files[1].Open || files[1].Path != other {
		t.Errorf("disk file should not be flagged Open: %+v", files[1])
	}
	if e := files[1].Edits[0]; e.StartCol != 0 || e.EndCol != 3 || e.Text != "new" {
		t.Errorf("disk edit converted wrong: %+v", e)
	}
}

func TestManagerCodeActionsPassDiagnostics(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	pos := buffer.Position{Line: 0, Col: 0}
	acts, err := m.CodeActions(context.Background(), path, pos, pos, []protocol.Diagnostic{{Message: "a"}, {Message: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 || acts[0].Title != "fix (2 diags)" {
		t.Fatalf("diagnostics context should reach the server, acts = %+v", acts)
	}
}

func TestManagerExecuteCommandAppliesEditViaCallback(t *testing.T) {
	applied := make(chan []FileEdits, 1)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{
		ApplyEdit: func(files []FileEdits) { applied <- files },
	})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/tmp/applyedit.go", []byte("target\n"), 0o644); err != nil {
		t.Skip("cannot stage /tmp target:", err)
	}
	defer os.Remove("/tmp/applyedit.go")

	if err := m.ExecuteCommand(context.Background(), path, protocol.Command{Command: "test.fix"}); err != nil {
		t.Fatal(err)
	}
	select {
	case files := <-applied:
		if len(files) != 1 || files[0].Path != "/tmp/applyedit.go" || files[0].Open {
			t.Fatalf("files = %+v", files)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("workspace/applyEdit never reached the callback")
	}
}

func TestManagerSignatureHelpAndTriggers(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	sh, err := m.SignatureHelp(context.Background(), path, buffer.Position{})
	if err != nil || sh == nil || sh.Signatures[0].Label != "Greet(name string)" {
		t.Fatalf("sh = %+v err = %v", sh, err)
	}
	trig := m.SignatureTriggers(path)
	if len(trig) != 2 || trig[0] != "(" {
		t.Fatalf("triggers = %v", trig)
	}
	if trig := m.SignatureTriggers("/nope/unknown.go"); trig != nil {
		t.Fatalf("unknown doc should have no triggers, got %v", trig)
	}
}

func TestManagerSemanticTokensFullThenDelta(t *testing.T) {
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "func main\nname here"); err != nil {
		t.Fatal(err)
	}
	spans, err := m.SemanticTokens(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 || spans[0].Capture != "keyword" || spans[0].EndCol != 4 {
		t.Fatalf("full spans = %+v", spans)
	}
	// Second request goes through the delta path (previousResultId=r1) and
	// applies the appended edit.
	spans, err = m.SemanticTokens(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 || spans[1].Capture != "function" || spans[1].Line != 1 {
		t.Fatalf("delta spans = %+v", spans)
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
