package dap

import (
	"encoding/json"

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
}

// capabilities is the subset of the initialize response IKE reads.
type capabilities struct {
	SupportsSetVariable bool `json:"supportsSetVariable"`
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
		"supportsRunInTerminalRequest": false,
	})
	if err != nil {
		return err
	}
	_ = json.Unmarshal(body, &s.caps) // capabilities are best-effort
	return nil
}

// SupportsSetVariable reports whether the adapter accepts setVariable requests.
func (s *Session) SupportsSetVariable() bool { return s.caps.SupportsSetVariable }

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

// SetBreakpoints replaces path's breakpoints. lines are IKE's 0-based buffer
// lines; the wire speaks 1-based.
func (s *Session) SetBreakpoints(path string, lines []int) ([]Breakpoint, error) {
	bps := make([]SourceBreakpoint, len(lines))
	for i, l := range lines {
		bps[i] = SourceBreakpoint{Line: l + 1}
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
