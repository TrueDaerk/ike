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
		return client.New(conn), func() { conn.Close() }, nil, nil
	}
}

// configConnector runs a fake server that, right after initialize, issues a
// workspace/configuration request for the given section and forwards the
// client's response payload to a channel. It proves the client both advertises
// the capability and answers with the toolchain-detected settings (#563).
func configConnector(section string, gotResult chan<- string) Connector {
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
					Result json.RawMessage  `json:"result"`
				}
				_ = json.Unmarshal(payload, &msg)
				switch {
				case msg.Method == "initialize":
					respond(sw, msg.ID, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{TextDocumentSync: json.RawMessage(`1`)}})
					// Now play server: ask the client for its config.
					req := `{"jsonrpc":"2.0","id":"cfg","method":"workspace/configuration","params":{"items":[{"section":"` + section + `"}]}}`
					_ = writeFrame(sw, []byte(req))
				case msg.Method == "" && msg.ID != nil:
					// A response to our workspace/configuration request.
					gotResult <- string(msg.Result)
				case msg.ID != nil:
					respond(sw, msg.ID, nil)
				}
			}
		}()
		conn := jsonrpc.NewConn(cli, handler)
		return client.New(conn), func() { conn.Close() }, nil, nil
	}
}

// The client must answer a server's workspace/configuration request with the
// toolchain-detected section (pyright reads the Python interpreter this way).
func TestWorkspaceConfigurationAnswersDetectedSection(t *testing.T) {
	langreg.Register(langreg.Language{ID: "toolc", Toolchain: fakeToolchain{}})
	gotResult := make(chan string, 1)
	spec := lsp.ServerSpec{Language: "toolc", Command: "fake"}
	m := New(resolver(spec), configConnector("marker", gotResult), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := m.Open(filepath.Join(dir, "a.tc"), "toolc", "x"); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-gotResult:
		// Response is an array with one entry: the "marker" section.
		if !strings.Contains(res, "/detected/python") {
			t.Fatalf("configuration response missing detected value: %s", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no workspace/configuration response")
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
