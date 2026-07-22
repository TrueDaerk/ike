package bridge

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/dap"
)

// freePort grabs an ephemeral port and releases it for the bridge to bind.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// listenClient wires a DAP session to a bridge in listen mode (#823).
func listenClient(t *testing.T, args map[string]any) (*dap.Session, chan dap.Event) {
	t.Helper()
	rwc := New("php")
	events := make(chan dap.Event, 64)
	s := dap.Connect(rwc, func(ev dap.Event) { events <- ev })
	t.Cleanup(s.Close)
	if err := s.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := <-s.LaunchAsync(args); err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitEvent(t, events, "initialized")
	return s, events
}

// serveRequestCycle scripts one accepted request on the engine side:
// feature sets, expected breakpoint replays, then the run that breaks.
func (e *fakeXdebug) serveBreakpointReplay(wantURI string, wantLine int) {
	name, tid, line := e.next()
	if name != "breakpoint_set" || !strings.Contains(line, wantURI) ||
		!strings.Contains(line, fmt.Sprintf("-n %d", wantLine)) {
		e.t.Errorf("unexpected breakpoint replay: %q (want %s line %d)", line, wantURI, wantLine)
	}
	e.ack(name, tid, `id="7"`)
}

func (e *fakeXdebug) serveRunBreak(fileURI string, line int) {
	name, tid, _ := e.next()
	if name != "run" {
		e.t.Errorf("expected run, got %q", name)
	}
	e.send(`<response xmlns="urn:debugger_protocol_v1" xmlns:xdebug="https://xdebug.org/dbgp/xdebug" command="run" transaction_id="` + tid + `" status="break" reason="ok"><xdebug:message filename="` + fileURI + fmt.Sprintf(`" lineno="%d"/></response>`, line))
}

func (e *fakeXdebug) serveRunEnd() {
	name, tid, _ := e.next()
	if name != "run" {
		e.t.Errorf("expected run, got %q", name)
	}
	e.ack(name, tid, `status="stopping" reason="ok"`)
}

// TestListenMultiAccept guards #823: the listener survives a finished
// request and debugs the next one — breakpoints replayed per connection.
func TestListenMultiAccept(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})

	// Breakpoints land before any connection: cached, optimistically verified.
	bps, err := s.SetBreakpoints("/proj/test.php", []int{4})
	if err != nil || len(bps) != 1 || !bps[0].Verified {
		t.Fatalf("setBreakpoints while idle: %v %+v", err, bps)
	}
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	for request := 1; request <= 2; request++ {
		engine := dialFakeXdebug(t, port)
		engine.serveFeatureSets()
		engine.serveBreakpointReplay("file:///proj/test.php", 5)
		engine.serveRunBreak("file:///proj/test.php", 5)
		if ev := waitEvent(t, events, "stopped").Stopped(); ev.Reason != "breakpoint" {
			t.Fatalf("request %d: unexpected stop: %+v", request, ev)
		}
		go engine.serveRunEnd()
		if err := s.Continue(1); err != nil {
			t.Fatalf("request %d: continue: %v", request, err)
		}
		// The session survives the request's end: continued, not terminated.
		waitEvent(t, events, "continued")
	}

	// Stopping the listener ends the DAP session cleanly.
	_ = s.Disconnect()
}

// TestListenHostnameFilter guards #823: only requests whose HTTP_HOST
// matches the filter attach; others are detached without disturbing the
// listener.
func TestListenHostnameFilter(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{
		"request": "launch", "mode": "listen", "port": port, "hostname": "onpage.local",
	})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	// The host probe is an eval (#938): property_get searches context 0 while
	// superglobals live in context 1, and auto_globals_jit leaves $_SERVER
	// uninitialized until referenced — eval is the only probe that works on
	// stock php-fpm setups. The expression travels base64-encoded after "--".
	serveHostProbe := func(e *fakeXdebug, host string) {
		name, tid, _ := e.next()
		if name != "step_into" {
			e.t.Errorf("expected step_into host probe, got %q", name)
		}
		e.ack(name, tid, `status="break" reason="ok"`)
		name, tid, line := e.next()
		expr := ""
		if i := strings.Index(line, "-- "); i >= 0 {
			if dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line[i+3:])); err == nil {
				expr = string(dec)
			}
		}
		if name != "eval" || !strings.Contains(expr, "HTTP_HOST") {
			e.t.Errorf("expected eval of HTTP_HOST, got %q (expr %q)", line, expr)
		}
		v := base64.StdEncoding.EncodeToString([]byte(host))
		e.send(`<response xmlns="urn:debugger_protocol_v1" command="eval" transaction_id="` + tid + `">` +
			`<property type="string" encoding="base64">` + v + `</property></response>`)
	}
	expectDetach := func(e *fakeXdebug, why string) {
		name, tid, _ := e.next()
		if name != "detach" {
			t.Fatalf("%s must be detached, got %q", why, name)
		}
		e.ack(name, tid, `status="stopping" reason="ok"`)
	}

	// Wrong vhost: detached, listener stays up — and the drop is announced,
	// never silent (#938).
	wrong := dialFakeXdebug(t, port)
	serveHostProbe(wrong, "other.local")
	expectDetach(wrong, "mismatching host")
	fd := waitEvent(t, events, "ike.filterDetach")
	var fdBody struct{ Host, Filter string }
	if err := json.Unmarshal(fd.Body, &fdBody); err != nil ||
		fdBody.Host != "other.local" || fdBody.Filter != "onpage.local" {
		t.Fatalf("filterDetach body = %s (%v)", fd.Body, err)
	}

	// No HTTP_HOST (a CLI request): detached with notice too.
	cli := dialFakeXdebug(t, port)
	serveHostProbe(cli, "")
	expectDetach(cli, "a hostless request")
	fd = waitEvent(t, events, "ike.filterDetach")
	if err := json.Unmarshal(fd.Body, &fdBody); err != nil || fdBody.Host != "" {
		t.Fatalf("hostless filterDetach body = %s (%v)", fd.Body, err)
	}

	// Matching vhost (port suffix ignored): attaches and breaks.
	right := dialFakeXdebug(t, port)
	serveHostProbe(right, "ONPAGE.local:8080")
	right.serveFeatureSets()
	right.serveRunBreak("file:///proj/test.php", 3)
	if ev := waitEvent(t, events, "stopped").Stopped(); ev.Reason != "breakpoint" {
		t.Fatalf("matching host must attach, got %+v", ev)
	}
	_ = s.Disconnect()
}

// TestListenPathMappings guards #823: breakpoints translate local→server on
// replay, stack frames translate server→local.
func TestListenPathMappings(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{
		"request": "launch", "mode": "listen", "port": port,
		"pathMappings": []map[string]string{{"server": "/var/www/html", "local": "/proj/src"}},
	})
	if _, err := s.SetBreakpoints("/proj/src/index.php", []int{9}); err != nil {
		t.Fatalf("setBreakpoints: %v", err)
	}
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	engine := dialFakeXdebug(t, port)
	engine.serveFeatureSets()
	engine.serveBreakpointReplay("file:///var/www/html/index.php", 10)
	engine.serveRunBreak("file:///var/www/html/index.php", 10)
	waitEvent(t, events, "stopped")

	go func() {
		name, tid, _ := engine.next()
		if name != "stack_get" {
			t.Errorf("expected stack_get, got %q", name)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="stack_get" transaction_id="` + tid + `">` +
			`<stack where="{main}" level="0" type="file" filename="file:///var/www/html/index.php" lineno="10"/></response>`)
	}()
	frames, err := s.StackTrace(1)
	if err != nil || len(frames) != 1 {
		t.Fatalf("stackTrace: %v %+v", err, frames)
	}
	if frames[0].Source.Path != "/proj/src/index.php" {
		t.Fatalf("frame path = %q, want the mapped local path", frames[0].Source.Path)
	}
	_ = s.Disconnect()
}

// dialFakeXdebugURI is dialFakeXdebug with a custom init fileuri.
func dialFakeXdebugURI(t *testing.T, port int, fileuri string) *fakeXdebug {
	t.Helper()
	var conn net.Conn
	var err error
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	e := &fakeXdebug{t: t, conn: conn, r: bufio.NewReader(conn)}
	e.send(`<init xmlns="urn:debugger_protocol_v1" fileuri="` + fileuri + `" language="PHP" protocol_version="1.0" appid="1" idekey="ike"/>`)
	return e
}

// TestListenPathMappingHint guards #832: an accepted request whose entry
// file does not resolve locally raises the mapping hint; one that resolves
// (through a mapping) stays silent.
func TestListenPathMappingHint(t *testing.T) {
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

	// Unresolvable entry file: the hint fires with the server directory.
	miss := dialFakeXdebugURI(t, port, "file:///var/www/html/index.php")
	miss.serveFeatureSets()
	go miss.serveRunEnd()
	hint := waitEvent(t, events, "ike.pathMappingHint")
	var body struct{ Server, File string }
	if err := json.Unmarshal(hint.Body, &body); err != nil || body.Server != "/var/www/html" {
		t.Fatalf("hint body = %s (%v)", hint.Body, err)
	}
	waitEvent(t, events, "continued")

	// Mapped entry file exists locally: no hint for this request.
	hit := dialFakeXdebugURI(t, port, "file:///srv/web/index.php")
	hit.serveFeatureSets()
	go hit.serveRunEnd()
	waitEvent(t, events, "continued")
	for {
		select {
		case ev := <-events:
			if ev.Name == "ike.pathMappingHint" {
				t.Fatal("a resolving entry file must not raise the hint")
			}
		default:
			_ = s.Disconnect()
			return
		}
	}
}

// TestListenBusyDetachesSecondConnection guards #823's sequential model: a
// request arriving while another is being debugged is detached untouched.
func TestListenBusyDetachesSecondConnection(t *testing.T) {
	port := freePort(t)
	s, events := listenClient(t, map[string]any{"request": "launch", "mode": "listen", "port": port})
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}

	first := dialFakeXdebug(t, port)
	first.serveFeatureSets()
	first.serveRunBreak("file:///proj/a.php", 1)
	waitEvent(t, events, "stopped")

	second := dialFakeXdebug(t, port)
	name, tid, _ := second.next()
	if name != "detach" {
		t.Fatalf("second concurrent connection must be detached, got %q", name)
	}
	second.ack(name, tid, `status="stopping" reason="ok"`)

	// The first session is undisturbed: stepping still works.
	go func() {
		name, tid, _ := first.next()
		if name != "step_over" {
			t.Errorf("expected step_over, got %q", name)
		}
		first.ack(name, tid, `status="break" reason="ok"`)
	}()
	if err := s.Next(1); err != nil {
		t.Fatalf("next: %v", err)
	}
	waitEvent(t, events, "stopped")
	_ = s.Disconnect()
}
