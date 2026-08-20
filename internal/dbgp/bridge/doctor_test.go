package bridge

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/dap"
)

// doctor_test.go guards the Xdebug doctor trace (#1991): the structured
// ike.listenState and ike.debugConn events that make the listener observable.
// Observability only — every scenario here also asserts the pre-existing
// accept/reject behavior stayed put.

// connBody is the ike.debugConn payload.
type connBody struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail"`
	Remote  string `json:"remote"`
	IDEKey  string `json:"ideKey"`
	FileURI string `json:"fileURI"`
	Host    string `json:"host"`
	Local   string `json:"local"`
	Mapped  *bool  `json:"mapped"`
}

// waitConn waits for the next ike.debugConn with the given outcome.
func waitConn(t *testing.T, events chan dap.Event, outcome string) connBody {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Name != "ike.debugConn" {
				continue
			}
			var b connBody
			if err := json.Unmarshal(ev.Body, &b); err != nil {
				t.Fatalf("debugConn body %s: %v", ev.Body, err)
			}
			if b.Outcome == outcome {
				return b
			}
		case <-deadline:
			t.Fatalf("no ike.debugConn with outcome %q arrived", outcome)
		}
	}
}

// TestDoctorListenState guards the listener-status half of #1991: opening the
// listener announces a structured state event with the bound port, the host
// filter and the mapping count.
func TestDoctorListenState(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{
		"request": "launch", "mode": "listen", "port": port, "hostname": "onpage.local",
		"pathMappings": []map[string]string{{"server": "/srv/web", "local": "/proj"}},
	})
	ev := waitEvent(t, events, "ike.listenState")
	var body struct {
		State    string `json:"state"`
		Port     int    `json:"port"`
		Hostname string `json:"hostname"`
		Mappings int    `json:"mappings"`
	}
	if err := json.Unmarshal(ev.Body, &body); err != nil {
		t.Fatalf("listenState body %s: %v", ev.Body, err)
	}
	if body.State != "listening" || body.Port != port || body.Hostname != "onpage.local" || body.Mappings != 1 {
		t.Fatalf("listenState = %+v, want listening on %d with filter and 1 mapping", body, port)
	}
	_ = s.Disconnect()
}

// TestDoctorAcceptedTrace guards the accept half of the trace: an adopted
// connection records its remote address, IDE key, init file URI and whether
// the entry file resolved locally through the path mappings.
func TestDoctorAcceptedTrace(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	s, events := listenClient(t, map[string]any{
		"request": "launch", "mode": "listen", "port": port,
		"pathMappings": []map[string]string{{"server": "/srv/web", "local": dir}},
	})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	// Mapped entry file: accepted, mapped=true.
	hit := dialFakeXdebugURI(t, port, "file:///srv/web/index.php")
	hit.serveFeatureSets()
	go hit.serveRunEnd()
	b := waitConn(t, events, "accepted")
	if b.IDEKey != "ike" || b.FileURI != "file:///srv/web/index.php" || b.Remote == "" {
		t.Fatalf("accepted trace = %+v, want idekey/fileuri/remote filled", b)
	}
	if b.Mapped == nil || !*b.Mapped || b.Local != filepath.Join(dir, "index.php") {
		t.Fatalf("accepted trace = %+v, want mapped local path", b)
	}
	waitEvent(t, events, "continued")

	// Unmapped entry file: still accepted (semantics unchanged, #832), but
	// the trace carries the unmapped-path diagnosis.
	miss := dialFakeXdebugURI(t, port, "file:///var/www/html/other.php")
	miss.serveFeatureSets()
	go miss.serveRunEnd()
	b = waitConn(t, events, "accepted")
	if b.Mapped == nil || *b.Mapped || b.FileURI != "file:///var/www/html/other.php" {
		t.Fatalf("unmapped accepted trace = %+v, want mapped=false", b)
	}
	_ = s.Disconnect()
}

// TestDoctorFilterRejectTrace guards the hostname-filter rejection: the trace
// names the probed host, the filter reason and the request's identity.
func TestDoctorFilterRejectTrace(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{
		"request": "launch", "mode": "listen", "port": port, "hostname": "onpage.local",
	})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	wrong := dialFakeXdebug(t, port)
	// Host probe: step_into, then the HTTP_HOST eval (see listen_test.go).
	name, tid, _ := wrong.next()
	if name != "step_into" {
		t.Fatalf("expected step_into host probe, got %q", name)
	}
	wrong.ack(name, tid, `status="break" reason="ok"`)
	name, tid, _ = wrong.next()
	if name != "eval" {
		t.Fatalf("expected eval host probe, got %q", name)
	}
	wrong.send(`<response xmlns="urn:debugger_protocol_v1" command="eval" transaction_id="` + tid + `">` +
		`<property type="string">other.local</property></response>`)
	if name, tid, _ = wrong.next(); name != "detach" {
		t.Fatalf("mismatching host must still be detached, got %q", name)
	}
	wrong.ack(name, tid, `status="stopping" reason="ok"`)

	b := waitConn(t, events, "rejected")
	if b.Reason != "filter" || b.Host != "other.local" || b.IDEKey != "ike" || b.Remote == "" {
		t.Fatalf("filter reject trace = %+v, want reason=filter with host and identity", b)
	}
	_ = s.Disconnect()
}

// TestDoctorMalformedInitTrace guards the malformed-init rejection (#1991): a
// peer whose first packet is not a parseable DBGp init is rejected with the
// concrete reason — fast, not after the 30s handshake timeout.
func TestDoctorMalformedInitTrace(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	conn, err := net.Dial("tcp", listenAddr(port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	payload := `<init xmlns="urn:debugger_protocol_v1" fileuri="file:///x.php"><broken></init>`
	if _, err := conn.Write([]byte(fmt.Sprintf("%d\x00%s\x00", len(payload), payload))); err != nil {
		t.Fatalf("write: %v", err)
	}

	start := time.Now()
	b := waitConn(t, events, "rejected")
	if b.Reason != "init" || b.Detail == "" || b.Remote == "" {
		t.Fatalf("malformed init trace = %+v, want reason=init with detail", b)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("malformed init took %v to reject, want fast failure", elapsed)
	}
	// The listener is undisturbed: a proper connection right after attaches.
	engine := dialFakeXdebug(t, port)
	engine.serveFeatureSets()
	engine.serveRunBreak("file:///proj/test.php", 3)
	waitEvent(t, events, "stopped")
	_ = s.Disconnect()
}

// TestDoctorHandshakeRejectTrace guards the silent-handshake rejection: a
// connection that closes without a word is traced with only its remote
// address — enough to see that *something* dialed in.
func TestDoctorHandshakeRejectTrace(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}
	conn, err := net.Dial("tcp", listenAddr(port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()
	b := waitConn(t, events, "rejected")
	if b.Reason != "handshake" || b.Remote == "" || b.IDEKey != "" {
		t.Fatalf("handshake reject trace = %+v, want reason=handshake with remote only", b)
	}
	_ = s.Disconnect()
}

// TestDoctorBusyRejectTrace guards the busy rejection: the latecomer's trace
// carries its own init identity, distinct from the live session.
func TestDoctorBusyRejectTrace(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	first := dialFakeXdebug(t, port)
	first.serveFeatureSets()
	first.serveRunBreak("file:///proj/test.php", 5)
	waitEvent(t, events, "stopped")

	second := dialFakeXdebugURI(t, port, "file:///proj/late.php")
	if name, tid, _ := second.next(); name == "detach" {
		second.ack(name, tid, `status="stopping" reason="ok"`)
	} else {
		t.Fatalf("the concurrent connection must be detached, got %q", name)
	}
	b := waitConn(t, events, "rejected")
	if b.Reason != "busy" || b.FileURI != "file:///proj/late.php" {
		t.Fatalf("busy reject trace = %+v, want reason=busy with the latecomer's file", b)
	}
	_ = s.Disconnect()
}
