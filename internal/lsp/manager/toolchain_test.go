package manager

import (
	"bufio"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	langreg "ike/internal/lang"
	"ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
)

type fakeToolchain struct{}

func (fakeToolchain) Detect(root string) (map[string]any, bool) {
	return map[string]any{"marker": map[string]any{"interpreter": "/detected/python"}}, true
}

// capturingConnector runs a fake server that forwards each initialize's
// initializationOptions to a channel, so a test can assert what reached the server.
func capturingConnector(initOpts chan<- string) Connector {
	return func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), error) {
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
					Params struct {
						InitializationOptions json.RawMessage `json:"initializationOptions"`
					} `json:"params"`
				}
				_ = json.Unmarshal(payload, &msg)
				if msg.Method == "initialize" {
					initOpts <- string(msg.Params.InitializationOptions)
					respond(sw, msg.ID, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{TextDocumentSync: json.RawMessage(`1`)}})
					continue
				}
				if msg.ID != nil {
					respond(sw, msg.ID, nil)
				}
			}
		}()
		conn := jsonrpc.NewConn(cli, handler)
		return client.New(conn), func() { conn.Close() }, nil
	}
}

// A language's Toolchain.Detect result must reach the server as merged settings.
func TestToolchainSettingsReachServer(t *testing.T) {
	langreg.Register(langreg.Language{ID: "toolx", Toolchain: fakeToolchain{}})
	initOpts := make(chan string, 1)
	spec := lsp.ServerSpec{Language: "toolx", Command: "fake"}
	m := New(resolver(spec), capturingConnector(initOpts), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := m.Open(filepath.Join(dir, "a.tx"), "toolx", "x"); err != nil {
		t.Fatal(err)
	}
	select {
	case opts := <-initOpts:
		if !strings.Contains(opts, "/detected/python") {
			t.Fatalf("initializationOptions missing toolchain value: %s", opts)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server never initialized")
	}
}
