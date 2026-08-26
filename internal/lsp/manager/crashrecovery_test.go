package manager

// crashrecovery_test.go covers the crash-recovery state machine of restart.go
// and manualrestart.go (#2148): the exponential backoff schedule, the attempt
// counter on the status line, the give-up (disable + toast naming the palette
// command, no further spawns) and the manual restart that revives a disabled
// server together with its open documents.

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
)

// statusEvent is one Callbacks.Status call, kept with its classification so a
// test can tell status-line state from toasts.
type statusEvent struct {
	text string
	kind lsp.ServerStatusKind
}

// recordingConnector is the crash-recovery test double: it answers initialize,
// reports every didOpen URI on didOpens, and closes its output — a crash —
// whenever crash reports true at that moment. connects counts the spawns.
func recordingConnector(crash func() bool, connects *int32, didOpens chan string) Connector {
	return func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), func() string, error) {
		if connects != nil {
			atomic.AddInt32(connects, 1)
		}
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
						TextDocument struct {
							URI string `json:"uri"`
						} `json:"textDocument"`
					} `json:"params"`
				}
				_ = json.Unmarshal(payload, &msg)
				switch {
				case msg.Method == "initialize":
					respond(sw, msg.ID, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{TextDocumentSync: json.RawMessage(`1`)}})
				case msg.Method == "textDocument/didOpen":
					if didOpens != nil {
						select {
						case didOpens <- msg.Params.TextDocument.URI:
						default:
						}
					}
					if crash != nil && crash() {
						_ = sw.Close()
						return
					}
				case msg.ID != nil:
					respond(sw, msg.ID, nil)
				}
			}
		}()
		conn := jsonrpc.NewConn(cli, handler)
		return client.New(conn), func() { conn.Close() }, nil, nil
	}
}

// TestRestartBackoffIsExponential (#2148): the schedule is 1s, 5s, 30s, and
// attempts past the table hold at the longest wait — an instantly dying server
// must never be respawned in a tight loop.
func TestRestartBackoffIsExponential(t *testing.T) {
	want := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, w := range want {
		if got := backoff(i + 1); got != w {
			t.Fatalf("backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	if got := backoff(0); got != time.Second {
		t.Fatalf("backoff(0) = %v, want the first delay", got)
	}
	for i := 1; i < len(restartDelays); i++ {
		if restartDelays[i] <= restartDelays[i-1] {
			t.Fatalf("delay %d (%v) does not exceed its predecessor (%v)", i, restartDelays[i], restartDelays[i-1])
		}
	}
}

// TestCrashRecoveryGivesUpAfterMaxRestarts (#2148): a server that dies on every
// spawn is retried exactly maxRestarts times — with the attempt counter on the
// status line — and then disabled: the status line says so, the toast names the
// manual restart command, and nothing spawns it again, not even a file open.
func TestCrashRecoveryGivesUpAfterMaxRestarts(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	var connects int32
	events := make(chan statusEvent, 64)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), recordingConnector(func() bool { return true }, &connects, nil), Callbacks{
		Status: func(lang, text string, kind lsp.ServerStatusKind) { events <- statusEvent{text, kind} },
	})
	m.backoffFn = func(int) time.Duration { return 5 * time.Millisecond }
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(path, []byte("package main"), 0o644)
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}

	var attempts []string
	var failedState string
	deadline := time.After(15 * time.Second)
	for done := false; !done; {
		select {
		case ev := <-events:
			switch {
			case ev.kind == lsp.ServerState && strings.Contains(ev.text, "restarting (attempt"):
				attempts = append(attempts, ev.text)
			case ev.kind == lsp.ServerState && strings.Contains(ev.text, "failed"):
				failedState = ev.text
			case ev.kind == lsp.ServerEventError && strings.Contains(ev.text, "disabled after repeated crashes"):
				if !strings.Contains(ev.text, restartCommandTitle) {
					t.Fatalf("give-up toast must name the manual restart command, got %q", ev.text)
				}
				done = true
			}
		case <-deadline:
			t.Fatalf("server was never disabled; attempts=%v connects=%d", attempts, atomic.LoadInt32(&connects))
		}
	}

	if len(attempts) != maxRestarts {
		t.Fatalf("expected %d restart attempts, got %v", maxRestarts, attempts)
	}
	for i, a := range attempts {
		want := "attempt " + strconv.Itoa(i+1) + "/" + strconv.Itoa(maxRestarts)
		if !strings.Contains(a, want) {
			t.Fatalf("attempt %d reported %q, want it to contain %q", i+1, a, want)
		}
	}
	if failedState != disabledStateText("go") {
		t.Fatalf("status-line state = %q, want %q", failedState, disabledStateText("go"))
	}

	// No restart storm: the initial spawn plus maxRestarts respawns, and
	// nothing after the give-up.
	want := int32(maxRestarts + 1)
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(&connects); got != want {
		t.Fatalf("spawned %d servers, want %d", got, want)
	}
	other := filepath.Join(dir, "other.go")
	_ = os.WriteFile(other, []byte("package main"), 0o644)
	if err := m.Open(other, "go", "package main"); !errors.Is(err, errServerDisabled) {
		t.Fatalf("Open on a disabled server = %v, want errServerDisabled", err)
	}
	if got := atomic.LoadInt32(&connects); got != want {
		t.Fatalf("a file open respawned a disabled server: %d spawns, want %d", got, want)
	}
}

// TestCrashRestartReportsAttemptAndRefreshes (#2148): a recoverable crash puts
// "restarting (attempt 1/N)" on the status line while the backoff runs, and the
// successful restart asks the host to re-pull its decorations, so hints, lenses
// and semantic tokens come back without an edit.
func TestCrashRestartReportsAttemptAndRefreshes(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	var connects int32
	events := make(chan statusEvent, 64)
	refreshes := make(chan string, 8)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), crashOnceConnector(&connects), Callbacks{
		Status:  func(lang, text string, kind lsp.ServerStatusKind) { events <- statusEvent{text, kind} },
		Refresh: func(kind string) { refreshes <- kind },
	})
	m.backoffFn = func(int) time.Duration { return 5 * time.Millisecond }
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(path, []byte("package main"), 0o644)
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}

	var sawRestarting bool
	deadline := time.After(10 * time.Second)
	for done := false; !done; {
		select {
		case ev := <-events:
			if ev.kind == lsp.ServerState && strings.Contains(ev.text, "restarting (attempt 1/"+strconv.Itoa(maxRestarts)+")") {
				sawRestarting = true
			}
			if ev.kind == lsp.ServerEventInfo && strings.Contains(ev.text, "restarted") {
				done = true
			}
		case <-deadline:
			t.Fatal("no successful restart observed")
		}
	}
	if !sawRestarting {
		t.Fatal("the status line never reported the restart attempt")
	}

	got := map[string]bool{}
	for len(got) < 3 {
		select {
		case kind := <-refreshes:
			got[kind] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("decorations were not re-pulled after the restart, got %v", got)
		}
	}
	for _, kind := range []string{"semanticTokens", "inlayHint", "codeLens"} {
		if !got[kind] {
			t.Fatalf("missing %s refresh, got %v", kind, got)
		}
	}
}

// TestManualRestartRevivesDisabledServer (#2148): once crash recovery gave up,
// the palette command's Manager.RestartAll clears the block, spawns a fresh
// server and re-opens the tracked documents — the user reopens no files.
func TestManualRestartRevivesDisabledServer(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	var crash atomic.Bool
	crash.Store(true)
	var connects int32
	didOpens := make(chan string, 32)
	events := make(chan statusEvent, 64)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), recordingConnector(crash.Load, &connects, didOpens), Callbacks{
		Status: func(lang, text string, kind lsp.ServerStatusKind) { events <- statusEvent{text, kind} },
	})
	m.backoffFn = func(int) time.Duration { return 5 * time.Millisecond }
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(path, []byte("package main"), 0o644)
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(15 * time.Second)
	for done := false; !done; {
		select {
		case ev := <-events:
			done = ev.kind == lsp.ServerEventError && strings.Contains(ev.text, "disabled after repeated crashes")
		case <-deadline:
			t.Fatal("server was never disabled")
		}
	}

	// The server behaves from now on; the user runs "LSP: Restart Servers".
	crash.Store(false)
	for drained := false; !drained; {
		select {
		case <-didOpens:
		default:
			drained = true
		}
	}
	before := atomic.LoadInt32(&connects)
	m.RestartAll()

	select {
	case uri := <-didOpens:
		if want := string(protocol.PathToURI(path)); uri != want {
			t.Fatalf("re-opened %q, want %q", uri, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the tracked document was not re-opened after the manual restart")
	}
	if got := atomic.LoadInt32(&connects); got <= before {
		t.Fatalf("manual restart spawned no server (%d spawns, was %d)", got, before)
	}
	m.mu.Lock()
	blocked := m.disabled[key("go", dir)]
	m.mu.Unlock()
	if blocked {
		t.Fatal("manual restart must clear the give-up block")
	}
}

// TestStableRunOpensFreshRestartBudget (#2148): a server that ran healthily for
// a while before dying starts a new restart budget, while one that dies right
// after spawning keeps consuming the current one — that is what keeps the
// give-up reachable without disabling LSP for the rest of a long session.
func TestStableRunOpensFreshRestartBudget(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	m.backoffFn = func(int) time.Duration { return 0 }
	m.stableRun = 10 * time.Millisecond

	// No documents are tracked, so each restart stops right after the backoff
	// — the attempt counting is what this test is about.
	old := &server{lang: "go", root: t.TempDir(), spec: spec, startedAt: time.Now().Add(-time.Second)}
	k := old.key()
	m.restarts[k] = maxRestarts
	m.restart(old, nil, "")
	if m.restarts[k] != 1 {
		t.Fatalf("a long healthy run must reset the budget, attempts = %d", m.restarts[k])
	}
	if m.disabled[k] {
		t.Fatal("a long healthy run must not disable the server")
	}

	old.startedAt = time.Now()
	m.restarts[k] = maxRestarts
	m.restart(old, nil, "")
	if !m.disabled[k] {
		t.Fatal("a server dying right after spawn must exhaust its budget")
	}
}
