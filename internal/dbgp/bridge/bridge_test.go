package bridge

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"ike/internal/dap"
)

// fakeXdebug is a scripted engine: it dials the bridge's listener like the
// real Xdebug and answers commands by name.
type fakeXdebug struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func dialFakeXdebug(t *testing.T, port int) *fakeXdebug {
	t.Helper()
	var conn net.Conn
	var err error
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	e := &fakeXdebug{t: t, conn: conn, r: bufio.NewReader(conn)}
	e.send(`<init xmlns="urn:debugger_protocol_v1" fileuri="file:///proj/test.php" language="PHP" protocol_version="1.0" appid="1" idekey="ike"/>`)
	return e
}

func (e *fakeXdebug) send(xmlBody string) {
	e.t.Helper()
	payload := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + xmlBody
	if _, err := e.conn.Write([]byte(fmt.Sprintf("%d\x00%s\x00", len(payload), payload))); err != nil {
		e.t.Errorf("engine write: %v", err)
	}
}

// next reads one command line and returns its name and tid.
func (e *fakeXdebug) next() (name, tid, line string) {
	e.t.Helper()
	raw, err := e.r.ReadString(0)
	if err != nil {
		return "", "", ""
	}
	line = strings.TrimSuffix(raw, "\x00")
	fields := strings.Fields(line)
	if len(fields) == 0 {
		e.t.Fatalf("empty command line")
	}
	name = fields[0]
	for i, f := range fields {
		if f == "-i" && i+1 < len(fields) {
			tid = fields[i+1]
		}
	}
	return name, tid, line
}

// ack answers a command with a minimal success response.
func (e *fakeXdebug) ack(name, tid string, extra string) {
	e.send(`<response xmlns="urn:debugger_protocol_v1" command="` + name + `" transaction_id="` + tid + `" ` + extra + `/>`)
}

// serveFeatureSets answers the bridge's post-handshake feature_set burst.
func (e *fakeXdebug) serveFeatureSets() {
	for i := 0; i < 3; i++ {
		name, tid, _ := e.next()
		if name != "feature_set" {
			e.t.Errorf("expected feature_set, got %q", name)
			return
		}
		e.ack(name, tid, `feature="x" success="1"`)
	}
}

// testClient wires a DAP session to a fresh bridge and auto-answers
// runInTerminal by dialing the fake engine into the announced port.
func testClient(t *testing.T) (*dap.Session, chan dap.Event, chan *fakeXdebug) {
	t.Helper()
	rwc := New("php")
	events := make(chan dap.Event, 64)
	s := dap.Connect(rwc, func(ev dap.Event) { events <- ev })
	engines := make(chan *fakeXdebug, 1)
	s.OnRunInTerminal(func(seq int, args dap.RunInTerminalArgs) {
		port := 0
		for _, a := range args.Args {
			if strings.HasPrefix(a, "-dxdebug.client_port=") {
				fmt.Sscanf(a, "-dxdebug.client_port=%d", &port)
			}
		}
		if port == 0 {
			t.Errorf("no client_port in argv %v", args.Args)
		}
		go func() {
			engines <- dialFakeXdebug(t, port)
		}()
		go func() { _ = s.RespondRunInTerminal(seq, 4711) }()
	})
	t.Cleanup(s.Close)
	return s, events, engines
}

func waitEvent(t *testing.T, events chan dap.Event, name string) dap.Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Name == name {
				return ev
			}
		case <-deadline:
			t.Fatalf("event %q did not arrive", name)
		}
	}
}

func TestFullSessionFlow(t *testing.T) {
	s, events, engines := testClient(t)

	if err := s.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	launchDone := s.LaunchAsync(map[string]any{
		"request": "launch",
		"program": "/proj/test.php",
		"args":    []string{"one"},
		"cwd":     "/proj",
	})
	engine := <-engines
	engine.serveFeatureSets()
	if err := <-launchDone; err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitEvent(t, events, "initialized")

	// Configuration: breakpoint on 0-based line 4 → wire line 5.
	go func() {
		name, tid, line := engine.next()
		if name != "breakpoint_set" || !strings.Contains(line, "-n 5") ||
			!strings.Contains(line, "file:///proj/test.php") {
			t.Errorf("unexpected breakpoint command: %q", line)
		}
		engine.ack(name, tid, `id="9"`)
	}()
	bps, err := s.SetBreakpoints("/proj/test.php", []int{4})
	if err != nil {
		t.Fatalf("setBreakpoints: %v", err)
	}
	if len(bps) != 1 || !bps[0].Verified || bps[0].Line != 5 {
		t.Fatalf("unexpected breakpoints: %+v", bps)
	}

	// configurationDone starts the run; the engine breaks at the breakpoint.
	go func() {
		name, tid, _ := engine.next()
		if name != "run" {
			t.Errorf("expected run, got %q", name)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" xmlns:xdebug="https://xdebug.org/dbgp/xdebug" command="run" transaction_id="` + tid + `" status="break" reason="ok"><xdebug:message filename="file:///proj/test.php" lineno="5"/></response>`)
	}()
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}
	stopped := waitEvent(t, events, "stopped").Stopped()
	if stopped.Reason != "breakpoint" || stopped.ThreadID != 1 {
		t.Fatalf("unexpected stopped event: %+v", stopped)
	}

	// Threads + stack while paused.
	threads, err := s.Threads()
	if err != nil || len(threads) != 1 || threads[0].ID != 1 {
		t.Fatalf("threads: %v %+v", err, threads)
	}
	go func() {
		name, tid, _ := engine.next()
		if name != "stack_get" {
			t.Errorf("expected stack_get, got %q", name)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="stack_get" transaction_id="` + tid + `">` +
			`<stack where="foo" level="0" type="file" filename="file:///proj/test.php" lineno="5"/>` +
			`<stack where="{main}" level="1" type="file" filename="file:///proj/test.php" lineno="12"/>` +
			`</response>`)
	}()
	frames, err := s.StackTrace(1)
	if err != nil {
		t.Fatalf("stackTrace: %v", err)
	}
	if len(frames) != 2 || frames[0].ID != 1 || frames[0].Line != 5 ||
		frames[0].Source.Path != "/proj/test.php" || frames[1].Name != "{main}" {
		t.Fatalf("unexpected frames: %+v", frames)
	}

	// Step over → stopped with reason step.
	go func() {
		name, tid, _ := engine.next()
		if name != "step_over" {
			t.Errorf("expected step_over, got %q", name)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="step_over" transaction_id="` + tid + `" status="break" reason="ok"/>`)
	}()
	if err := s.Next(1); err != nil {
		t.Fatalf("next: %v", err)
	}
	if ev := waitEvent(t, events, "stopped").Stopped(); ev.Reason != "step" {
		t.Fatalf("unexpected step stop: %+v", ev)
	}

	// Continue to the end of the script → terminated.
	go func() {
		name, tid, _ := engine.next()
		if name != "run" {
			t.Errorf("expected run, got %q", name)
		}
		engine.ack(name, tid, `status="stopping" reason="ok"`)
	}()
	if err := s.Continue(1); err != nil {
		t.Fatalf("continue: %v", err)
	}
	waitEvent(t, events, "terminated")
}

func TestLaunchFailsWithoutEngine(t *testing.T) {
	rwc := New("php")
	events := make(chan dap.Event, 8)
	s := dap.Connect(rwc, func(ev dap.Event) { events <- ev })
	t.Cleanup(s.Close)
	// Refuse the terminal spawn: launch must fail, not hang.
	s.OnRunInTerminal(func(seq int, args dap.RunInTerminalArgs) {
		go func() { _ = s.RefuseReverse(seq, "runInTerminal", "no terminal in test") }()
	})
	if err := s.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	err := <-s.LaunchAsync(map[string]any{"request": "launch", "program": "/x.php"})
	if err == nil || !strings.Contains(err.Error(), "no terminal in test") {
		t.Fatalf("want refused launch, got %v", err)
	}
}
