package manager

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
)

// cleardiags_test.go covers #994: a disabled or deliberately stopped server
// must not leave its last diagnostics frozen in open editors.

// diagRecorder collects every Diagnostics callback per path.
type diagRecorder struct {
	mu     sync.Mutex
	counts map[string][]int // per path: diagnostic count of each publish, in order
}

func newDiagRecorder() *diagRecorder { return &diagRecorder{counts: map[string][]int{}} }

func (r *diagRecorder) record(path string, p protocol.PublishDiagnosticsParams, lines []string, enc string) {
	r.mu.Lock()
	r.counts[path] = append(r.counts[path], len(p.Diagnostics))
	r.mu.Unlock()
}

// sawThenCleared reports whether path first received a non-empty publish and
// its latest publish is empty.
func (r *diagRecorder) sawThenCleared(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	seq := r.counts[path]
	if len(seq) == 0 || seq[len(seq)-1] != 0 {
		return false
	}
	for _, n := range seq {
		if n > 0 {
			return true
		}
	}
	return false
}

// crashingDiagConnector produces servers that answer initialize, publish one
// diagnostic on didOpen and immediately drop the connection — every single
// time, so the manager runs through all restart attempts into the disable.
func crashingDiagConnector() Connector {
	return func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), func() string, error) {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		cli := rwc{Reader: cr, Writer: cw}
		go func() {
			in := bufio.NewReader(sr)
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
				_ = json.Unmarshal(payload, &msg)
				switch {
				case msg.Method == "initialize":
					respond(sw, msg.ID, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{TextDocumentSync: json.RawMessage(`1`)}})
				case msg.Method == "textDocument/didOpen":
					var p protocol.DidOpenTextDocumentParams
					_ = json.Unmarshal(msg.Params, &p)
					notify(sw, "textDocument/publishDiagnostics", protocol.PublishDiagnosticsParams{
						URI:     p.TextDocument.URI,
						Version: 1,
						Diagnostics: []protocol.Diagnostic{{
							Range:    protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 3}},
							Severity: protocol.SeverityError,
							Message:  "boom",
						}},
					})
					_ = sw.Close() // crash right after publishing
					return
				case msg.ID != nil:
					respond(sw, msg.ID, nil)
				}
			}
		}()
		conn := jsonrpc.NewConn(cli, handler)
		return client.New(conn), func() { conn.Close() }, nil, nil
	}
}

func waitCleared(t *testing.T, rec *diagRecorder, path, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if rec.sawThenCleared(path) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	rec.mu.Lock()
	seq := rec.counts[path]
	rec.mu.Unlock()
	t.Fatalf("%s: diagnostics not cleared, publish counts = %v", what, seq)
}

func TestDisableAfterRepeatedCrashesClearsDiagnostics(t *testing.T) {
	rec := newDiagRecorder()
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), crashingDiagConnector(), Callbacks{
		Diagnostics: rec.record,
	})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(path, []byte("package main"), 0o644)
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}

	// Every spawn crashes after its publish; after maxRestarts the manager
	// disables the server and must clear the last publish.
	waitCleared(t, rec, path, "disable after repeated crashes")
}

func TestStopLangClearsDiagnostics(t *testing.T) {
	rec := newDiagRecorder()
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{
		Diagnostics: rec.record,
	})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(path, []byte("package main"), 0o644)
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	waitForPublish(t, rec, path)

	m.StopLang("go")
	waitCleared(t, rec, path, "StopLang")
}

func TestShutdownClearsDiagnostics(t *testing.T) {
	rec := newDiagRecorder()
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{
		Diagnostics: rec.record,
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(path, []byte("package main"), 0o644)
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	waitForPublish(t, rec, path)

	m.Shutdown()
	waitCleared(t, rec, path, "Shutdown")
}

// waitForPublish blocks until path received at least one non-empty publish.
func waitForPublish(t *testing.T, rec *diagRecorder, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		var seen bool
		for _, n := range rec.counts[path] {
			if n > 0 {
				seen = true
			}
		}
		rec.mu.Unlock()
		if seen {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no diagnostics published before the stop")
}

// workspaceDiagConnector answers initialize and, on didOpen, publishes one
// diagnostic for the opened file AND one for a project file nobody opened —
// the workspace-diagnostic shape (#1102).
func workspaceDiagConnector(extraURI string) Connector {
	return func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), func() string, error) {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		cli := rwc{Reader: cr, Writer: cw}
		go func() {
			in := bufio.NewReader(sr)
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
				_ = json.Unmarshal(payload, &msg)
				switch msg.Method {
				case "initialize":
					respond(sw, msg.ID, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{TextDocumentSync: json.RawMessage(`1`)}})
				case "textDocument/didOpen":
					var p protocol.DidOpenTextDocumentParams
					_ = json.Unmarshal(msg.Params, &p)
					diag := []protocol.Diagnostic{{Message: "boom"}}
					notify(sw, "textDocument/publishDiagnostics", protocol.PublishDiagnosticsParams{URI: p.TextDocument.URI, Diagnostics: diag})
					notify(sw, "textDocument/publishDiagnostics", protocol.PublishDiagnosticsParams{URI: extraURI, Diagnostics: diag})
				case "shutdown":
					respond(sw, msg.ID, nil)
				}
			}
		}()
		conn := jsonrpc.NewConn(cli, handler)
		return client.New(conn), func() { conn.Close() }, nil, nil
	}
}

// TestStopLangFlushesUnopenedPublishes guards #1102: a path published without
// an open document gets an empty publish on StopLang, so the Problems store
// drops the stale findings.
func TestStopLangFlushesUnopenedPublishes(t *testing.T) {
	rec := newDiagRecorder()
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	dir := t.TempDir()
	unopened := filepath.Join(dir, "unopened.go")
	m := New(resolver(spec), workspaceDiagConnector(protocol.PathToURI(unopened)), Callbacks{
		Diagnostics: rec.record,
	})
	defer m.Shutdown()

	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(path, []byte("package main"), 0o644)
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	waitForPublish(t, rec, path)
	waitForPublish(t, rec, unopened)

	m.StopLang("go")
	waitCleared(t, rec, path, "StopLang open doc")
	waitCleared(t, rec, unopened, "StopLang unopened path")
}
