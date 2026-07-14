package dap

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"ike/internal/lsp/jsonrpc"
)

// fakeAdapter answers DAP requests over an in-memory pipe like a minimal
// debugpy: it verifies breakpoints, emits a stopped event after
// configurationDone, and serves a canned stack/scopes/variables tree.
type fakeAdapter struct {
	in  *io.PipeReader // client → adapter
	out *io.PipeWriter // adapter → client

	mu   sync.Mutex
	seq  int
	seen []string // commands received, in order
}

// pipes builds the duplex streams: the client side implements io.ReadWriteCloser.
type clientPipe struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (c clientPipe) Read(b []byte) (int, error)  { return c.r.Read(b) }
func (c clientPipe) Write(b []byte) (int, error) { return c.w.Write(b) }
func (c clientPipe) Close() error                { c.w.Close(); return c.r.Close() }

func startFake(t *testing.T) (clientPipe, *fakeAdapter) {
	t.Helper()
	cr, aw := io.Pipe() // adapter writes → client reads
	ar, cw := io.Pipe() // client writes → adapter reads
	fa := &fakeAdapter{in: ar, out: aw}
	go fa.serve()
	t.Cleanup(func() { aw.Close(); ar.Close() })
	return clientPipe{r: cr, w: cw}, fa
}

func (f *fakeAdapter) send(msg envelope) {
	f.mu.Lock()
	f.seq++
	msg.Seq = f.seq
	data, _ := json.Marshal(msg)
	_ = jsonrpc.WriteFrame(f.out, data)
	f.mu.Unlock()
}

func (f *fakeAdapter) respond(req envelope, body any) {
	data, _ := json.Marshal(body)
	f.send(envelope{Type: typeResponse, RequestSeq: req.Seq, Command: req.Command, Success: true, Body: data})
}

func (f *fakeAdapter) event(name string, body any) {
	data, _ := json.Marshal(body)
	f.send(envelope{Type: typeEvent, Event: name, Body: data})
}

func (f *fakeAdapter) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

func (f *fakeAdapter) serve() {
	r := bufio.NewReader(f.in)
	for {
		data, err := jsonrpc.ReadFrame(r)
		if err != nil {
			return
		}
		var req envelope
		if json.Unmarshal(data, &req) != nil || req.Type != typeRequest {
			continue
		}
		f.mu.Lock()
		f.seen = append(f.seen, req.Command)
		f.mu.Unlock()
		switch req.Command {
		case "initialize":
			f.respond(req, map[string]any{"supportsConfigurationDoneRequest": true})
			f.event("initialized", map[string]any{})
		case "launch":
			f.respond(req, map[string]any{})
		case "setBreakpoints":
			var args struct {
				Breakpoints []SourceBreakpoint `json:"breakpoints"`
			}
			_ = json.Unmarshal(req.Arguments, &args)
			out := make([]Breakpoint, len(args.Breakpoints))
			for i, b := range args.Breakpoints {
				out[i] = Breakpoint{Verified: true, Line: b.Line}
			}
			f.respond(req, map[string]any{"breakpoints": out})
		case "configurationDone":
			f.respond(req, map[string]any{})
			f.event("stopped", StoppedEvent{Reason: "breakpoint", ThreadID: 1})
		case "threads":
			f.respond(req, map[string]any{"threads": []Thread{{ID: 1, Name: "MainThread"}}})
		case "stackTrace":
			f.respond(req, map[string]any{"stackFrames": []StackFrame{
				{ID: 11, Name: "inner", Source: Source{Path: "/p/a.py"}, Line: 7},
				{ID: 12, Name: "<module>", Source: Source{Path: "/p/a.py"}, Line: 20},
			}})
		case "scopes":
			f.respond(req, map[string]any{"scopes": []Scope{{Name: "Locals", VariablesReference: 100}}})
		case "variables":
			f.respond(req, map[string]any{"variables": []Variable{{Name: "x", Value: "42", Type: "int"}}})
		case "next", "stepIn", "stepOut", "continue":
			f.respond(req, map[string]any{})
			f.event("stopped", StoppedEvent{Reason: "step", ThreadID: 1})
		case "disconnect":
			f.respond(req, map[string]any{})
			f.event("terminated", map[string]any{})
		default:
			f.send(envelope{Type: typeResponse, RequestSeq: req.Seq, Command: req.Command, Success: false, Message: "unknown"})
		}
	}
}

// collectEvents returns a handler + getter for events seen so far.
func collectEvents() (func(Event), func() []string) {
	var mu sync.Mutex
	var names []string
	return func(e Event) {
			mu.Lock()
			names = append(names, e.Name)
			mu.Unlock()
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), names...)
		}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestSessionLifecycle drives the full handshake and stepping vocabulary
// against the fake adapter.
func TestSessionLifecycle(t *testing.T) {
	pipe, fa := startFake(t)
	onEvent, events := collectEvents()
	s := NewSession(NewConn(pipe, func(name string, body json.RawMessage) {
		onEvent(Event{Name: name, Body: body})
	}))
	defer s.Close()

	if err := s.Initialize(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "initialized event", func() bool {
		for _, n := range events() {
			if n == "initialized" {
				return true
			}
		}
		return false
	})
	launched := s.LaunchAsync(map[string]any{"program": "/p/a.py"})
	bps, err := s.SetBreakpoints("/p/a.py", []int{6}) // 0-based in
	if err != nil {
		t.Fatal(err)
	}
	if len(bps) != 1 || !bps[0].Verified || bps[0].Line != 7 {
		t.Fatalf("breakpoints = %+v, want verified line 7 (1-based)", bps)
	}
	if err := s.ConfigurationDone(); err != nil {
		t.Fatal(err)
	}
	if err := <-launched; err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stopped event", func() bool {
		for _, n := range events() {
			if n == "stopped" {
				return true
			}
		}
		return false
	})

	threads, err := s.Threads()
	if err != nil || len(threads) != 1 || threads[0].ID != 1 {
		t.Fatalf("threads = %v (%v)", threads, err)
	}
	frames, err := s.StackTrace(1)
	if err != nil || len(frames) != 2 || frames[0].Name != "inner" || frames[1].Line != 20 {
		t.Fatalf("frames = %+v (%v)", frames, err)
	}
	scopes, err := s.Scopes(frames[1].ID)
	if err != nil || len(scopes) != 1 || scopes[0].Name != "Locals" {
		t.Fatalf("scopes = %+v (%v)", scopes, err)
	}
	vars, err := s.Variables(scopes[0].VariablesReference)
	if err != nil || len(vars) != 1 || vars[0].Value != "42" {
		t.Fatalf("variables = %+v (%v)", vars, err)
	}

	for _, step := range []func(int) error{s.Next, s.StepIn, s.StepOut, s.Continue} {
		if err := step(1); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "terminated event", func() bool {
		for _, n := range events() {
			if n == "terminated" {
				return true
			}
		}
		return false
	})
	want := []string{"initialize", "launch", "setBreakpoints", "configurationDone", "threads", "stackTrace", "scopes", "variables", "next", "stepIn", "stepOut", "continue", "disconnect"}
	got := fa.commands()
	if len(got) != len(want) {
		t.Fatalf("adapter saw %v, want %v", got, want)
	}
}

// TestCallOnClosedConn fails cleanly.
func TestCallOnClosedConn(t *testing.T) {
	pipe, _ := startFake(t)
	c := NewConn(pipe, nil)
	_ = c.Close()
	if _, err := c.Call("initialize", nil); err == nil {
		t.Fatal("a closed connection must refuse calls")
	}
}

// TestFailedRequestSurfacesMessage maps success=false onto an error.
func TestFailedRequestSurfacesMessage(t *testing.T) {
	pipe, _ := startFake(t)
	c := NewConn(pipe, nil)
	defer c.Close()
	if _, err := c.Call("no-such-command", nil); err == nil {
		t.Fatal("a failed response must surface as an error")
	}
}
