package dap

import "encoding/json"

// protocol.go holds the typed slices of the DAP structures IKE uses. Only the
// fields the client reads are declared; unknown fields are ignored by
// encoding/json, per the protocol's compatibility rules.

// Source names a file in requests and responses.
type Source struct {
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
}

// SourceBreakpoint is one requested breakpoint (setBreakpoints). Lines are
// 1-based on the wire.
type SourceBreakpoint struct {
	Line int `json:"line"`
}

// Breakpoint is the adapter's verdict on a requested breakpoint.
type Breakpoint struct {
	Verified bool `json:"verified"`
	Line     int  `json:"line,omitempty"`
}

// Thread is one debuggee thread.
type Thread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// StackFrame is one frame of a stackTrace response. Line/Column are 1-based.
type StackFrame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Source Source `json:"source"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Scope is one variable scope of a frame (Locals, Globals, …).
type Scope struct {
	Name               string `json:"name"`
	VariablesReference int    `json:"variablesReference"`
	Expensive          bool   `json:"expensive"`
}

// Variable is one variable (or structured child) in a scope.
type Variable struct {
	Name               string `json:"name"`
	Value              string `json:"value"`
	Type               string `json:"type,omitempty"`
	VariablesReference int    `json:"variablesReference"`
}

// RunInTerminalArgs is the adapter's runInTerminal reverse request (#625): the
// client must launch Args in a terminal it controls and reply with the
// process id, so the debuggee gets a real tty for interactive stdin.
type RunInTerminalArgs struct {
	Kind  string            `json:"kind"` // "integrated" or "external"
	Title string            `json:"title,omitempty"`
	Cwd   string            `json:"cwd"`
	Args  []string          `json:"args"`
	Env   map[string]string `json:"env,omitempty"`
}

// StoppedEvent is the body of a "stopped" event.
type StoppedEvent struct {
	Reason            string `json:"reason"`
	ThreadID          int    `json:"threadId"`
	AllThreadsStopped bool   `json:"allThreadsStopped"`
	Description       string `json:"description,omitempty"`
	Text              string `json:"text,omitempty"`
}

// OutputEvent is the body of an "output" event.
type OutputEvent struct {
	Category string `json:"category,omitempty"`
	Output   string `json:"output"`
}

// ExitedEvent is the body of an "exited" event.
type ExitedEvent struct {
	ExitCode int `json:"exitCode"`
}

// Event is one adapter event, decoded on demand by the consumer.
type Event struct {
	Name string
	Body json.RawMessage
}

// Stopped decodes a stopped event's body (zero value on mismatch).
func (e Event) Stopped() StoppedEvent {
	var s StoppedEvent
	_ = json.Unmarshal(e.Body, &s)
	return s
}

// Output decodes an output event's body.
func (e Event) Output() OutputEvent {
	var o OutputEvent
	_ = json.Unmarshal(e.Body, &o)
	return o
}

// Exited decodes an exited event's body.
func (e Event) Exited() ExitedEvent {
	var x ExitedEvent
	_ = json.Unmarshal(e.Body, &x)
	return x
}
