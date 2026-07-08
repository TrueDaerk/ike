package manager

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
)

// crashOnceConnector returns a connector whose first server answers initialize
// and then closes its output (simulating a crash); later servers behave normally.
func crashOnceConnector(connects *int32) Connector {
	return func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), error) {
		n := atomic.AddInt32(connects, 1)
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		cli := rwc{Reader: cr, Writer: cw}
		crash := n == 1
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
				}
				_ = json.Unmarshal(payload, &msg)
				switch {
				case msg.Method == "initialize":
					respond(sw, msg.ID, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{TextDocumentSync: json.RawMessage(`1`)}})
				case msg.Method == "textDocument/didOpen" && crash:
					_ = sw.Close() // drop the connection mid-session after a clean handshake
					return
				case msg.ID != nil:
					respond(sw, msg.ID, nil)
				}
			}
		}()
		conn := jsonrpc.NewConn(cli, handler)
		return client.New(conn), func() { conn.Close() }, nil
	}
}

func TestManagerRestartsAfterCrash(t *testing.T) {
	var connects int32
	statusCh := make(chan string, 16)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), crashOnceConnector(&connects), Callbacks{
		Status: func(lang, text string, kind lsp.ServerStatusKind) { statusCh <- text },
	})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(path, []byte("package main"), 0o644)
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}

	// Expect a "restarted" status within a few seconds (after backoff).
	deadline := time.After(5 * time.Second)
	for {
		select {
		case s := <-statusCh:
			if containsWord(s, "restarted") {
				if atomic.LoadInt32(&connects) < 2 {
					t.Fatalf("expected a second connect for restart, got %d", connects)
				}
				return
			}
		case <-deadline:
			t.Fatalf("no restart status observed; connects=%d", atomic.LoadInt32(&connects))
		}
	}
}

func containsWord(s, w string) bool {
	return len(s) >= len(w) && (indexOf(s, w) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
