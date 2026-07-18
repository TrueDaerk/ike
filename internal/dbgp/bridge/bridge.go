// Package bridge is the in-process DAP adapter for PHP (0360, #699): it
// serves the Debug Adapter Protocol on one end of an in-memory pipe — the
// debug manager speaks to it exactly like to a spawned adapter process —
// and translates to DBGp against Xdebug on a loopback TCP listener.
//
// Connection direction is DBGp's quirk: the bridge listens, the PHP process
// (told where to connect via -d ini overrides) dials in when it starts. The
// debuggee itself is spawned by the client through the runInTerminal reverse
// request, so it gets a real tty like every other debuggee (#625).
package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"ike/internal/dbgp"
	"ike/internal/lsp/jsonrpc"
)

// acceptTimeout bounds the wait for the PHP process to dial back after the
// client confirmed the terminal launch.
const acceptTimeout = 30 * time.Second

// New starts a bridge for the given PHP interpreter and returns the client
// end of its DAP connection. The bridge lives until the connection closes.
func New(php string) io.ReadWriteCloser {
	client, server := net.Pipe()
	b := &bridge{php: php, rwc: server, revPending: map[int]chan revReply{}}
	go b.serve()
	return client
}

// envelope mirrors the DAP wire message (the dap package keeps its own
// unexported copy; the bridge is the server side of the same wire).
type envelope struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    bool            `json:"success,omitempty"`
	Message    string          `json:"message,omitempty"`
	Event      string          `json:"event,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// revReply is the client's answer to a bridge-initiated reverse request.
type revReply struct {
	success bool
	message string
}

// bridge is one live adapter instance.
type bridge struct {
	php string
	rwc io.ReadWriteCloser

	writeMu sync.Mutex
	seqMu   sync.Mutex
	seq     int

	mu         sync.Mutex
	dc         *dbgp.Conn
	listener   net.Listener
	bpIDs      map[string][]string // path → engine breakpoint ids
	vars       *varTable           // live variablesReferences; nil while running
	ended      bool
	revPending map[int]chan revReply
}

// serve runs the DAP read loop until the pipe closes. A panic anywhere in
// the bridge tears the session down instead of the app (in-process!).
func (b *bridge) serve() {
	defer b.recoverClose()
	r := bufio.NewReader(b.rwc)
	for {
		data, err := jsonrpc.ReadFrame(r)
		if err != nil {
			b.shutdown()
			return
		}
		var msg envelope
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "request":
			go func(msg envelope) {
				defer b.recoverClose()
				b.handleRequest(msg)
			}(msg)
		case "response":
			b.mu.Lock()
			ch := b.revPending[msg.RequestSeq]
			delete(b.revPending, msg.RequestSeq)
			b.mu.Unlock()
			if ch != nil {
				ch <- revReply{success: msg.Success, message: msg.Message}
			}
		}
	}
}

// recoverClose converts a bridge panic into a closed session.
func (b *bridge) recoverClose() {
	if r := recover(); r != nil {
		b.shutdown()
	}
}

// shutdown tears everything down; safe to call repeatedly.
func (b *bridge) shutdown() {
	b.mu.Lock()
	if b.ended {
		b.mu.Unlock()
		return
	}
	b.ended = true
	dc, l := b.dc, b.listener
	for seq, ch := range b.revPending {
		close(ch)
		delete(b.revPending, seq)
	}
	b.mu.Unlock()
	if dc != nil {
		go func() {
			_, _ = dc.Stop() // best effort: ends the script if still alive
			_ = dc.Close()
		}()
	}
	if l != nil {
		_ = l.Close()
	}
	_ = b.rwc.Close()
}

// --- wire helpers ---

func (b *bridge) nextSeq() int {
	b.seqMu.Lock()
	defer b.seqMu.Unlock()
	b.seq++
	return b.seq
}

func (b *bridge) writeMsg(msg envelope) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	_ = jsonrpc.WriteFrame(b.rwc, data)
}

func (b *bridge) respond(req envelope, body any) {
	data, _ := json.Marshal(body)
	b.writeMsg(envelope{Seq: b.nextSeq(), Type: "response", RequestSeq: req.Seq, Command: req.Command, Success: true, Body: data})
}

func (b *bridge) fail(req envelope, msg string) {
	b.writeMsg(envelope{Seq: b.nextSeq(), Type: "response", RequestSeq: req.Seq, Command: req.Command, Success: false, Message: msg})
}

func (b *bridge) event(name string, body any) {
	var data json.RawMessage
	if body != nil {
		data, _ = json.Marshal(body)
	}
	b.writeMsg(envelope{Seq: b.nextSeq(), Type: "event", Event: name, Body: data})
}

// reverseRequest sends a bridge-initiated request (runInTerminal) and waits
// for the client's reply.
func (b *bridge) reverseRequest(command string, args any, timeout time.Duration) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	seq := b.nextSeq()
	ch := make(chan revReply, 1)
	b.mu.Lock()
	if b.ended {
		b.mu.Unlock()
		return fmt.Errorf("session ended")
	}
	b.revPending[seq] = ch
	b.mu.Unlock()
	b.writeMsg(envelope{Seq: seq, Type: "request", Command: command, Arguments: data})
	select {
	case rep, ok := <-ch:
		if !ok {
			return fmt.Errorf("session ended")
		}
		if !rep.success {
			if rep.message == "" {
				rep.message = command + " refused"
			}
			return fmt.Errorf("%s", rep.message)
		}
		return nil
	case <-time.After(timeout):
		b.mu.Lock()
		delete(b.revPending, seq)
		b.mu.Unlock()
		return fmt.Errorf("%s: no reply", command)
	}
}

// --- request dispatch ---

func (b *bridge) handleRequest(req envelope) {
	switch req.Command {
	case "initialize":
		b.respond(req, map[string]any{
			"supportsConfigurationDoneRequest": true,
			"supportsSetVariable":              true,
		})
	case "launch":
		b.handleLaunch(req)
	case "setBreakpoints":
		b.handleSetBreakpoints(req)
	case "configurationDone":
		b.respond(req, map[string]any{})
		go b.resume(req, "breakpoint", (*dbgp.Conn).Run)
	case "continue":
		b.respond(req, map[string]any{"allThreadsContinued": true})
		go b.resume(req, "breakpoint", (*dbgp.Conn).Run)
	case "next":
		b.respond(req, map[string]any{})
		go b.resume(req, "step", (*dbgp.Conn).StepOver)
	case "stepIn":
		b.respond(req, map[string]any{})
		go b.resume(req, "step", (*dbgp.Conn).StepInto)
	case "stepOut":
		b.respond(req, map[string]any{})
		go b.resume(req, "step", (*dbgp.Conn).StepOut)
	case "threads":
		b.respond(req, map[string]any{"threads": []map[string]any{{"id": 1, "name": "main thread"}}})
	case "stackTrace":
		b.handleStackTrace(req)
	case "scopes":
		b.handleScopes(req)
	case "variables":
		b.handleVariables(req)
	case "setVariable":
		b.handleSetVariable(req)
	case "disconnect":
		b.respond(req, map[string]any{})
		b.shutdown()
	default:
		b.fail(req, "unsupported request: "+req.Command)
	}
}

// launchArgs is the launch request vocabulary the PHP provider emits (#701).
type launchArgs struct {
	Program string            `json:"program"`
	Args    []string          `json:"args"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
}

// handleLaunch opens the DBGp listener, has the client spawn PHP in a
// terminal pointed back at it, and completes the DBGp handshake. The
// initialized event follows a successful handshake, which is when the
// manager pushes breakpoints and finishes configuration.
func (b *bridge) handleLaunch(req envelope) {
	var args launchArgs
	if err := json.Unmarshal(req.Arguments, &args); err != nil || args.Program == "" {
		b.fail(req, "invalid launch arguments")
		return
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.fail(req, "dbgp listen: "+err.Error())
		return
	}
	b.mu.Lock()
	if b.ended {
		b.mu.Unlock()
		_ = l.Close()
		b.fail(req, "session ended")
		return
	}
	b.listener = l
	b.mu.Unlock()
	port := l.Addr().(*net.TCPAddr).Port

	argv := append([]string{
		b.php,
		"-dxdebug.mode=debug",
		"-dxdebug.start_with_request=yes",
		"-dxdebug.client_host=127.0.0.1",
		fmt.Sprintf("-dxdebug.client_port=%d", port),
	}, args.Program)
	argv = append(argv, args.Args...)

	env := map[string]*string{}
	for k, v := range args.Env {
		v := v
		env[k] = &v
	}
	rit := map[string]any{
		"kind":  "integrated",
		"title": "php debug",
		"cwd":   args.Cwd,
		"args":  argv,
	}
	if len(env) > 0 {
		rit["env"] = env
	}
	if err := b.reverseRequest("runInTerminal", rit, acceptTimeout); err != nil {
		b.fail(req, "launching php: "+err.Error())
		b.shutdown()
		return
	}

	_ = l.(*net.TCPListener).SetDeadline(time.Now().Add(acceptTimeout))
	conn, err := l.Accept()
	_ = l.Close()
	b.mu.Lock()
	b.listener = nil
	b.mu.Unlock()
	if err != nil {
		b.fail(req, "xdebug did not connect — is the Xdebug extension loaded?")
		b.shutdown()
		return
	}

	dc := dbgp.NewConn(conn, func(s dbgp.Stream) {
		category := "stdout"
		if s.Type == "stderr" {
			category = "stderr"
		}
		b.event("output", map[string]any{"category": category, "output": s.Text()})
	})
	if _, err := dc.WaitInit(acceptTimeout); err != nil {
		_ = dc.Close()
		b.fail(req, "dbgp handshake: "+err.Error())
		b.shutdown()
		return
	}
	// Reasonable variable limits; failures are cosmetic, not fatal.
	_ = dc.FeatureSet("max_depth", "1")
	_ = dc.FeatureSet("max_children", "100")
	_ = dc.FeatureSet("max_data", "4096")

	b.mu.Lock()
	if b.ended {
		b.mu.Unlock()
		_ = dc.Close()
		b.fail(req, "session ended")
		return
	}
	b.dc = dc
	b.bpIDs = map[string][]string{}
	b.mu.Unlock()

	b.respond(req, map[string]any{})
	b.event("initialized", nil)
}

// conn returns the live DBGp connection, or nil after teardown/before launch.
func (b *bridge) conn() *dbgp.Conn {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dc
}

// handleSetBreakpoints replaces one file's breakpoints: remove the engine
// ids set earlier for that path, set the new lines. Lines arrive 1-based
// (the client declares linesStartAt1), which is DBGp's convention too.
func (b *bridge) handleSetBreakpoints(req envelope) {
	var args struct {
		Source struct {
			Path string `json:"path"`
		} `json:"source"`
		Breakpoints []struct {
			Line int `json:"line"`
		} `json:"breakpoints"`
	}
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		b.fail(req, "invalid setBreakpoints arguments")
		return
	}
	dc := b.conn()
	if dc == nil {
		b.fail(req, "no debug session")
		return
	}
	b.mu.Lock()
	old := b.bpIDs[args.Source.Path]
	delete(b.bpIDs, args.Source.Path)
	b.mu.Unlock()
	for _, id := range old {
		_ = dc.BreakpointRemove(id)
	}
	uri := dbgp.ToURI(args.Source.Path)
	ids := make([]string, 0, len(args.Breakpoints))
	verdicts := make([]map[string]any, 0, len(args.Breakpoints))
	for _, bp := range args.Breakpoints {
		id, err := dc.BreakpointSet(uri, bp.Line)
		if err != nil {
			verdicts = append(verdicts, map[string]any{"verified": false, "line": bp.Line})
			continue
		}
		ids = append(ids, id)
		verdicts = append(verdicts, map[string]any{"verified": true, "line": bp.Line})
	}
	b.mu.Lock()
	b.bpIDs[args.Source.Path] = ids
	b.mu.Unlock()
	b.respond(req, map[string]any{"breakpoints": verdicts})
}

// resume drives one continuation command and turns its (eventual) response
// into the matching DAP event: break → stopped, end of run → terminated.
// The DAP response was already sent — DAP semantics: continue/step return
// immediately, the stop arrives as an event.
func (b *bridge) resume(req envelope, stopReason string, cmd func(*dbgp.Conn) (*dbgp.Response, error)) {
	defer b.recoverClose()
	dc := b.conn()
	if dc == nil {
		return
	}
	// The debuggee runs: every variablesReference handed out while paused
	// dies with the resume.
	b.resetVars()
	resp, err := cmd(dc)
	if err != nil {
		// A dead connection mid-run means the script finished (Xdebug drops
		// the link on exit); anything else ends the session the same way.
		b.finish()
		return
	}
	switch {
	case resp.Status == "break":
		reason := stopReason
		if resp.Message != nil && resp.Message.Exception != "" {
			reason = "exception"
		}
		b.event("stopped", map[string]any{
			"reason":            reason,
			"threadId":          1,
			"allThreadsStopped": true,
		})
	case dbgp.StatusEnded(resp.Status):
		b.finish()
	}
}

// finish emits the end-of-session events once and tears down.
func (b *bridge) finish() {
	b.mu.Lock()
	already := b.ended
	b.mu.Unlock()
	if already {
		return
	}
	b.event("terminated", nil)
	b.shutdown()
}

// handleStackTrace maps stack_get to DAP frames. Frame id encodes the DBGp
// depth as id-1, so scopes/variables (#700) can find the frame again.
func (b *bridge) handleStackTrace(req envelope) {
	dc := b.conn()
	if dc == nil {
		b.fail(req, "no debug session")
		return
	}
	entries, err := dc.StackGet()
	if err != nil {
		b.fail(req, err.Error())
		return
	}
	frames := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		name := e.Where
		if name == "" {
			name = "{main}"
		}
		fr := map[string]any{
			"id":     e.Level + 1,
			"name":   name,
			"line":   e.Lineno,
			"column": 1,
		}
		if path := dbgp.FromURI(e.Filename); !strings.Contains(path, "://") {
			fr["source"] = map[string]any{"path": path}
		}
		frames = append(frames, fr)
	}
	b.respond(req, map[string]any{"stackFrames": frames, "totalFrames": len(frames)})
}
