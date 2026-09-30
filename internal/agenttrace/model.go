// Package agenttrace parses coding-agent session transcripts into a
// harness-neutral event model and discovers the transcript that belongs to a
// working directory.
//
// The first (and so far only) harness is Claude Code, which appends each
// session as JSONL under ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl
// while the session runs. The package is pure: no UI, no config, no clock.
// Hosts feed it a reader or a path and get back a Session whose Events are
// the user prompts, assistant text and reasoning, tool calls with the files
// they touched, and separators such as context compactions.
package agenttrace

import (
	"encoding/json"
	"time"
)

// HarnessClaude names the Claude Code harness in Session.Harness.
const HarnessClaude = "claude"

// Kind classifies an Event.
type Kind int

const (
	// KindUser is a prompt the user typed (or a harness fed in).
	KindUser Kind = iota
	// KindAssistant is assistant text; Reasoning marks a thinking block.
	KindAssistant
	// KindTool is one tool call together with its result once it arrived.
	KindTool
	// KindSeparator marks a boundary that is not a prompt: a context
	// compaction or a summary line.
	KindSeparator
)

// String returns the lowercase name used in logs and tests.
func (k Kind) String() string {
	switch k {
	case KindUser:
		return "user"
	case KindAssistant:
		return "assistant"
	case KindTool:
		return "tool"
	case KindSeparator:
		return "separator"
	}
	return "unknown"
}

// Op says what a tool call did to a file.
type Op int

const (
	OpRead Op = iota
	OpEdit
	OpWrite
	OpCreate
	OpDelete
)

// String returns the lowercase name used in logs and tests.
func (o Op) String() string {
	switch o {
	case OpRead:
		return "read"
	case OpEdit:
		return "edit"
	case OpWrite:
		return "write"
	case OpCreate:
		return "create"
	case OpDelete:
		return "delete"
	}
	return "unknown"
}

// FileRef is one file a tool call touched. Line is 1-based and 0 when the
// location could not be resolved.
type FileRef struct {
	Path string
	Line int
	Op   Op
}

// Tool is the call half of a KindTool event, filled in as the transcript
// delivers first the tool_use block and later the tool_result.
type Tool struct {
	// ID is the harness's tool_use id, used to pair the result.
	ID string
	// Name is the tool's name as the harness reports it (Edit, Bash, ...).
	Name string
	// Title is a one-line label derived from the input: the path for file
	// tools, the description or command for Bash, the pattern for Grep.
	Title string
	// Input is the raw tool input as the harness recorded it.
	Input json.RawMessage
	// Output is the result text (joined text blocks), capped at MaxOutput.
	Output string
	// IsError is the harness's is_error flag on the result.
	IsError bool
	// Truncated reports that Output was cut at MaxOutput bytes.
	Truncated bool
	// Done reports that a result has arrived for this call.
	Done bool
	// DoneAt is when the result arrived; zero until Done.
	DoneAt time.Time
	// Paths are the files this call touched, in input order.
	Paths []FileRef
}

// Event is one entry of a Session's timeline.
type Event struct {
	Kind Kind
	// Turn counts user prompts; every event carries the turn it belongs to.
	Turn int
	At   time.Time
	// Text is the prompt, assistant text or separator label.
	Text string
	// Reasoning marks an assistant thinking block.
	Reasoning bool
	// UUID is the harness's line id; parent links are not modelled.
	UUID string
	// Tool is set for KindTool events.
	Tool *Tool
}

// Session is one transcript.
type Session struct {
	ID string
	// ParentID is the session this one was forked from. Claude Code writes no
	// marker into the fork itself; discovery fills this in by matching the
	// copied root line (see List).
	ParentID  string
	Harness   string
	CWD       string
	StartedAt time.Time
	EndedAt   time.Time
	Events    []Event
	// Malformed counts lines that were not valid JSON objects and were
	// skipped.
	Malformed int
}

// Turns returns the number of user prompts seen so far.
func (s *Session) Turns() int {
	n := 0
	for i := range s.Events {
		if s.Events[i].Kind == KindUser {
			n++
		}
	}
	return n
}

// Files returns every FileRef of every tool event, in timeline order.
func (s *Session) Files() []FileRef {
	var out []FileRef
	for i := range s.Events {
		if t := s.Events[i].Tool; t != nil {
			out = append(out, t.Paths...)
		}
	}
	return out
}
