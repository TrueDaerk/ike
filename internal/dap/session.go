package dap

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"

	"ike/internal/lsp/transport"
)

// Session is one live debug-adapter session: the adapter process plus the
// DAP connection over its stdio. It exposes the request vocabulary IKE uses;
// sequencing (initialize → launch → initialized event → setBreakpoints →
// configurationDone) is the caller's job — the debug manager (#579) owns it.
type Session struct {
	proc *transport.Process
	conn *Conn

	// caps are the adapter capabilities from the initialize response; only the
	// flags IKE gates on are decoded.
	caps capabilities

	// evalMu guards evalUnsupported, the discovered evaluate capability
	// (#2174). Evaluate runs off the UI goroutine — watches fan out one
	// request per expression — so the latch needs a lock.
	evalMu          sync.Mutex
	evalUnsupported bool
}

// capabilities is the subset of the initialize response IKE reads.
type capabilities struct {
	SupportsSetVariable               bool `json:"supportsSetVariable"`
	SupportsConditionalBreakpoints    bool `json:"supportsConditionalBreakpoints"`
	SupportsHitConditionalBreakpoints bool `json:"supportsHitConditionalBreakpoints"`
	SupportsLogPoints                 bool `json:"supportsLogPoints"`
	SupportsEvaluateForHovers         bool `json:"supportsEvaluateForHovers"`
}

// Start spawns the adapter described by spec and connects. Events (stopped,
// continued, terminated, exited, output, initialized, …) arrive on onEvent
// from the read loop — hand off, don't block.
func Start(spec transport.Spec, onEvent func(Event)) (*Session, error) {
	proc, err := transport.Start(spec)
	if err != nil {
		return nil, err
	}
	handler := func(name string, body json.RawMessage) {
		if onEvent != nil {
			onEvent(Event{Name: name, Body: body})
		}
	}
	return &Session{proc: proc, conn: NewConn(proc.Conn(), handler)}, nil
}

// Connect wraps an in-process adapter connection (0360: PHP's DBGp bridge
// runs inside IKE; there is no adapter process to spawn).
func Connect(rwc io.ReadWriteCloser, onEvent func(Event)) *Session {
	handler := func(name string, body json.RawMessage) {
		if onEvent != nil {
			onEvent(Event{Name: name, Body: body})
		}
	}
	return &Session{conn: NewConn(rwc, handler)}
}

// NewSession wraps an existing connection (tests use an in-memory pipe).
func NewSession(conn *Conn) *Session { return &Session{conn: conn} }

// Initialize performs the capability handshake, retaining the adapter
// capabilities IKE gates features on (e.g. setVariable).
func (s *Session) Initialize() error {
	body, err := s.conn.Call("initialize", map[string]any{
		"adapterID":                    "ike",
		"clientID":                     "ike",
		"linesStartAt1":                true,
		"columnsStartAt1":              true,
		"pathFormat":                   "path",
		"supportsRunInTerminalRequest": true,
	})
	if err != nil {
		return err
	}
	_ = json.Unmarshal(body, &s.caps) // capabilities are best-effort
	return nil
}

// SupportsSetVariable reports whether the adapter accepts setVariable requests.
func (s *Session) SupportsSetVariable() bool { return s.caps.SupportsSetVariable }

// SupportsConditionalBreakpoints reports whether the adapter evaluates
// breakpoint conditions (#1914). Without it, conditions are stripped before
// setBreakpoints and the breakpoint stops unconditionally.
func (s *Session) SupportsConditionalBreakpoints() bool {
	return s.caps.SupportsConditionalBreakpoints
}

// SupportsHitConditionalBreakpoints reports hit-count support (#1914).
func (s *Session) SupportsHitConditionalBreakpoints() bool {
	return s.caps.SupportsHitConditionalBreakpoints
}

// SupportsLogPoints reports logpoint support (#1914). Without it, log
// messages are stripped and the breakpoint behaves as a plain stop.
func (s *Session) SupportsLogPoints() bool { return s.caps.SupportsLogPoints }

// SupportsEvaluateForHovers reports whether the adapter accepts the "hover"
// evaluate context (#2174). It is the only evaluate-related flag DAP puts in
// the initialize response; the evaluate popup falls back to the "repl"
// context without it, which every adapter understands.
func (s *Session) SupportsEvaluateForHovers() bool { return s.caps.SupportsEvaluateForHovers }

// ErrEvaluateUnsupported is what Evaluate answers once the adapter refused
// the request as unimplemented (#2174). It is a sticky verdict: the watch and
// evaluate-popup surfaces disable themselves with a notice instead of
// spending one failing request per expression per stop.
var ErrEvaluateUnsupported = errors.New("dap: adapter does not support evaluate")

// SupportsEvaluate reports whether evaluate requests are worth sending
// (#2174). DAP has no initialize capability for evaluate — the spec requires
// every adapter to implement it — so support is optimistic until an adapter
// refuses the request as unsupported, which latches it off for the rest of
// the session.
func (s *Session) SupportsEvaluate() bool {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	return !s.evalUnsupported
}

// markEvaluateUnsupported latches the verdict.
func (s *Session) markEvaluateUnsupported() {
	s.evalMu.Lock()
	s.evalUnsupported = true
	s.evalMu.Unlock()
}

// unsupportedRequest reports whether an adapter error reads as "I do not
// implement this request" rather than "that expression is bad". Only the
// former latches the capability off — a mistyped watch must never disable
// watches. The phrasings cover IKE's own DBGp bridge ("unsupported request:
// evaluate") and the wordings adapters use for a missing command.
func unsupportedRequest(msg string) bool {
	msg = strings.ToLower(msg)
	for _, needle := range []string{
		"unsupported",
		"not supported",
		"unimplemented",
		"not implemented",
		"unknown command",
		"unknown request",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// OnRunInTerminal registers the handler for the adapter's runInTerminal reverse
// request (#625). fn runs on the read-loop goroutine and MUST hand off (it may
// not block); it replies asynchronously with RespondRunInTerminal or
// RefuseReverse. Other reverse requests keep being refused. Call before launch.
func (s *Session) OnRunInTerminal(fn func(seq int, args RunInTerminalArgs)) {
	s.conn.SetReverseHandler(func(seq int, command string, raw json.RawMessage) bool {
		if command != "runInTerminal" {
			return false
		}
		var args RunInTerminalArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			// Malformed arguments: still claim the request and refuse it with
			// a diagnostic — silently dropping it would hang the adapter, and
			// the generic "unsupported" refusal would hide the cause (#638).
			// Reply off the read loop, like every other reverse reply.
			go func() {
				_ = s.conn.RefuseRequest(seq, command, "invalid runInTerminal arguments: "+err.Error())
			}()
			return true
		}
		fn(seq, args)
		return true
	})
}

// RespondRunInTerminal answers a runInTerminal request with the launched
// process id.
func (s *Session) RespondRunInTerminal(seq, processID int) error {
	return s.conn.Respond(seq, "runInTerminal", map[string]any{"processId": processID})
}

// RefuseReverse rejects an adapter-initiated request (e.g. terminal spawn
// failed), so the adapter can surface the error.
func (s *Session) RefuseReverse(seq int, command, message string) error {
	return s.conn.RefuseRequest(seq, command, message)
}

// LaunchAsync sends the launch request; many adapters (debugpy) answer it
// only after configurationDone, so the response is delivered on the returned
// channel instead of blocking the sequencing.
func (s *Session) LaunchAsync(args map[string]any) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := s.conn.Call("launch", args)
		done <- err
	}()
	return done
}

// SetBreakpoints replaces path's breakpoints. Lines in reqs are IKE's 0-based
// buffer lines; the wire speaks 1-based. Condition/hit-count/log-message
// fields the adapter did not advertise in its capabilities are stripped here
// (#1914) — the breakpoint itself is always sent, so an unsupported
// refinement degrades to a plain stop instead of a silently missing
// breakpoint.
func (s *Session) SetBreakpoints(path string, reqs []SourceBreakpoint) ([]Breakpoint, error) {
	bps := make([]SourceBreakpoint, len(reqs))
	for i, r := range reqs {
		r.Line++
		if !s.caps.SupportsConditionalBreakpoints {
			r.Condition = ""
		}
		if !s.caps.SupportsHitConditionalBreakpoints {
			r.HitCondition = ""
		}
		if !s.caps.SupportsLogPoints {
			r.LogMessage = ""
		}
		bps[i] = r
	}
	body, err := s.conn.Call("setBreakpoints", map[string]any{
		"source":      Source{Path: path},
		"breakpoints": bps,
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Breakpoints []Breakpoint `json:"breakpoints"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Breakpoints, nil
}

// ConfigurationDone finishes the configuration phase; the debuggee starts.
func (s *Session) ConfigurationDone() error {
	_, err := s.conn.Call("configurationDone", map[string]any{})
	return err
}

// Continue resumes threadID (F9).
func (s *Session) Continue(threadID int) error {
	_, err := s.conn.Call("continue", map[string]any{"threadId": threadID})
	return err
}

// Next steps over (F8).
func (s *Session) Next(threadID int) error {
	_, err := s.conn.Call("next", map[string]any{"threadId": threadID})
	return err
}

// StepIn steps into (F7).
func (s *Session) StepIn(threadID int) error {
	_, err := s.conn.Call("stepIn", map[string]any{"threadId": threadID})
	return err
}

// StepOut steps out (shift+F8).
func (s *Session) StepOut(threadID int) error {
	_, err := s.conn.Call("stepOut", map[string]any{"threadId": threadID})
	return err
}

// Threads lists the debuggee's threads.
func (s *Session) Threads() ([]Thread, error) {
	body, err := s.conn.Call("threads", map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Threads []Thread `json:"threads"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Threads, nil
}

// StackTrace returns threadID's frames, newest first.
func (s *Session) StackTrace(threadID int) ([]StackFrame, error) {
	body, err := s.conn.Call("stackTrace", map[string]any{"threadId": threadID})
	if err != nil {
		return nil, err
	}
	var resp struct {
		StackFrames []StackFrame `json:"stackFrames"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.StackFrames, nil
}

// Scopes returns frameID's variable scopes.
func (s *Session) Scopes(frameID int) ([]Scope, error) {
	body, err := s.conn.Call("scopes", map[string]any{"frameId": frameID})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Scopes []Scope `json:"scopes"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Scopes, nil
}

// Variables expands a variablesReference (a scope or a structured value).
func (s *Session) Variables(ref int) ([]Variable, error) {
	body, err := s.conn.Call("variables", map[string]any{"variablesReference": ref})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Variables []Variable `json:"variables"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Variables, nil
}

// SetVariable changes the variable named name within variablesReference ref to
// value (setVariable). It returns the adapter's echo of the new value/type and
// any structured reference, so the panel can refresh the row. Only valid while
// paused and when SupportsSetVariable reports true.
func (s *Session) SetVariable(ref int, name, value string) (Variable, error) {
	body, err := s.conn.Call("setVariable", map[string]any{
		"variablesReference": ref,
		"name":               name,
		"value":              value,
	})
	if err != nil {
		return Variable{}, err
	}
	var resp struct {
		Value              string `json:"value"`
		Type               string `json:"type"`
		VariablesReference int    `json:"variablesReference"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Variable{}, err
	}
	return Variable{Name: name, Value: resp.Value, Type: resp.Type, VariablesReference: resp.VariablesReference}, nil
}

// Evaluate evaluates expr in the context of frameID (0 = the adapter's
// default frame) and returns the rendered result (#1914, watches). context is
// the DAP evaluate context hint ("watch", "repl", "hover"); adapters may use
// it to pick side-effect rules. An adapter that refuses evaluate as
// unimplemented latches SupportsEvaluate off, and every later call answers
// ErrEvaluateUnsupported without touching the wire (#2174).
func (s *Session) Evaluate(expr string, frameID int, context string) (EvaluateResult, error) {
	if !s.SupportsEvaluate() {
		return EvaluateResult{}, ErrEvaluateUnsupported
	}
	args := map[string]any{"expression": expr}
	if frameID != 0 {
		args["frameId"] = frameID
	}
	if context != "" {
		args["context"] = context
	}
	body, err := s.conn.Call("evaluate", args)
	if err != nil {
		if unsupportedRequest(err.Error()) {
			s.markEvaluateUnsupported()
			return EvaluateResult{}, ErrEvaluateUnsupported
		}
		return EvaluateResult{}, err
	}
	var resp EvaluateResult
	if err := json.Unmarshal(body, &resp); err != nil {
		return EvaluateResult{}, err
	}
	return resp, nil
}

// Disconnect asks the adapter to end the session (terminating the debuggee).
func (s *Session) Disconnect() error {
	_, err := s.conn.Call("disconnect", map[string]any{"terminateDebuggee": true})
	return err
}

// Close tears the connection and the adapter process down. Safe after
// Disconnect and on half-dead sessions.
func (s *Session) Close() {
	if s.conn != nil {
		_ = s.conn.Close()
	}
	if s.proc != nil {
		_ = s.proc.Stop()
	}
}

// Stderr exposes the adapter's captured stderr for error surfaces.
func (s *Session) Stderr() string {
	if s.proc == nil {
		return ""
	}
	return s.proc.Stderr()
}

// Exited reports adapter-process death (nil channel for test sessions).
func (s *Session) Exited() <-chan struct{} {
	if s.proc == nil {
		return nil
	}
	return s.proc.Exited()
}
