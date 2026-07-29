package bridge

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// TestStopWithUnresponsiveEngineClosesConn (#1375): disconnect during an
// attached listen-mode session must never wait forever on the polite detach —
// an engine that swallows the command gets its connection closed after the
// bounded teardown wait, and the listener is gone so nothing new can wedge.
func TestStopWithUnresponsiveEngineClosesConn(t *testing.T) {
	old := teardownTimeout
	teardownTimeout = 100 * time.Millisecond
	defer func() { teardownTimeout = old }()

	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	engine := dialFakeXdebug(t, port)
	engine.serveFeatureSets()
	engine.serveRunBreak("file:///proj/test.php", 3)
	waitEvent(t, events, "stopped")

	// Stop the session while attached; the DAP round trip must return.
	if err := s.Disconnect(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	// The engine sees the polite detach but never answers it.
	_ = engine.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if name, _, _ := engine.next(); name != "detach" {
		t.Fatalf("expected detach on stop, got %q", name)
	}

	// Bounded teardown: the bridge force-closes the connection instead of
	// waiting on the response forever.
	buf := make([]byte, 1)
	if _, err := engine.conn.Read(buf); err == nil {
		t.Fatal("engine connection still open after the bounded teardown wait")
	}

	// The listener is down: a request arriving after stop is rejected, not
	// left hanging against a dead bridge.
	if conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, rerr := conn.Read(buf); rerr == nil {
			t.Fatal("post-stop connection was neither refused nor closed")
		}
		_ = conn.Close()
	}
}
