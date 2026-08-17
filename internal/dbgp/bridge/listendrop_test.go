package bridge

import (
	"encoding/json"
	"net"
	"strconv"
	"testing"
	"time"

	"ike/internal/dap"
)

// dropBody is the ike.debugDrop payload the listener emits for a connection
// it turns away (#1328).
type dropBody struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
	Count  int    `json:"count"`
}

// waitDrop waits for the next ike.debugDrop with the given reason.
func waitDrop(t *testing.T, events chan dap.Event, reason string) dropBody {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Name != "ike.debugDrop" {
				continue
			}
			var b dropBody
			if err := json.Unmarshal(ev.Body, &b); err != nil {
				t.Fatalf("debugDrop body %s: %v", ev.Body, err)
			}
			if b.Reason == reason {
				return b
			}
		case <-deadline:
			t.Fatalf("no ike.debugDrop with reason %q arrived", reason)
		}
	}
}

// TestListenBusyConnectionIsAnnounced guards #1328: a request arriving while
// another is being debugged is detached *and* reported, so it can never look
// like the listener simply ignored it.
func TestListenBusyConnectionIsAnnounced(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})
	if _, err := s.SetBreakpoints("/proj/test.php", []dap.SourceBreakpoint{{Line: 4}}); err != nil {
		t.Fatalf("setBreakpoints: %v", err)
	}
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	first := dialFakeXdebug(t, port)
	first.serveFeatureSets()
	first.serveBreakpointReplay("file:///proj/test.php", 5)
	first.serveRunBreak("file:///proj/test.php", 5)
	waitEvent(t, events, "stopped")

	// A second request dials in while the first one is paused.
	second := dialFakeXdebug(t, port)
	if name, tid, _ := second.next(); name == "detach" {
		second.ack(name, tid, `status="stopping" reason="ok"`)
	} else {
		t.Fatalf("the concurrent connection must be detached, got %q", name)
	}
	if b := waitDrop(t, events, "busy"); b.Count != 1 || b.Detail == "" {
		t.Fatalf("busy drop = %+v, want a described first drop", b)
	}
	// A third one keeps counting, so a client can throttle its notifications.
	third := dialFakeXdebug(t, port)
	if name, tid, _ := third.next(); name == "detach" {
		third.ack(name, tid, `status="stopping" reason="ok"`)
	}
	if b := waitDrop(t, events, "busy"); b.Count != 2 {
		t.Fatalf("second busy drop count = %d, want 2", b.Count)
	}
	_ = s.Disconnect()
}

// TestListenSilentHandshakeFailureIsReported guards the drop path that used
// to leave no evidence at all (#1328): a connection that never sends its
// DBGp init.
func TestListenSilentHandshakeFailureIsReported(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}
	// Dial and close without a handshake: the bridge's WaitInit fails.
	conn, err := net.Dial("tcp", listenAddr(port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()
	if b := waitDrop(t, events, "handshake"); b.Count != 1 {
		t.Fatalf("handshake drop = %+v", b)
	}
	// The listener is still up: a proper connection right after attaches.
	engine := dialFakeXdebug(t, port)
	engine.serveFeatureSets()
	engine.serveRunBreak("file:///proj/test.php", 3)
	waitEvent(t, events, "stopped")
	_ = s.Disconnect()
}

// TestListenAcceptsWhileVetting guards the accept loop's lifecycle (#1328):
// a connection that stalls its handshake must not stop the listener from
// serving the requests behind it. Before the fix the vetting ran inside the
// accept loop, so a silent connection blocked everything for the accept
// timeout.
func TestListenAcceptsWhileVetting(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}
	// A connection that dials in and says nothing.
	stalled, err := net.Dial("tcp", listenAddr(port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer stalled.Close()

	// The next request must still be served — well within the accept timeout.
	engine := dialFakeXdebug(t, port)
	engine.serveFeatureSets()
	engine.serveRunBreak("file:///proj/test.php", 7)
	waitEvent(t, events, "stopped")
	_ = s.Disconnect()
}

// TestListenReapsDeadSession guards the stale-session path (#1328): when the
// debugged request's connection dies while it is paused, nothing is
// outstanding to notice — the bridge used to keep reporting "busy" and turn
// every later request away. The next connection now collects the corpse and
// becomes the session.
func TestListenReapsDeadSession(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	first := dialFakeXdebug(t, port)
	first.serveFeatureSets()
	first.serveRunBreak("file:///proj/test.php", 5)
	waitEvent(t, events, "stopped")
	// The php-fpm worker dies while we are paused: the socket goes away with
	// no reply to any command.
	_ = first.conn.Close()

	second := dialFakeXdebug(t, port)
	second.serveFeatureSets()
	second.serveRunBreak("file:///proj/test.php", 9)
	if ev := waitEvent(t, events, "stopped").Stopped(); ev.Reason != "breakpoint" {
		t.Fatalf("the request after a dead session must attach, got %+v", ev)
	}
	_ = s.Disconnect()
}

// listenAddr is the loopback address of the bridge's listener.
func listenAddr(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
