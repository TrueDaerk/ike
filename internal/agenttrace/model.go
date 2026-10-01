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
	"sort"
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
	// Result is the harness's structured result (Claude Code's toolUseResult
	// object) of a file-changing call — Edit, MultiEdit, Write, NotebookEdit
	// — kept for the diffs: it carries originalFile and structuredPatch.
	// nil for the other tools and for a plain-text result.
	Result json.RawMessage
	// Span is the window of the file a Read returned, as its structured
	// result reports it; nil for the other tools and a plain-text result.
	// The diffs (#2859) take a Read's output as the file's content only
	// when the span says it is the whole file.
	Span *ReadSpan
	// AgentID is the id of the subagent an Agent (Task) call spawned, as its
	// result reports it; "" until the result arrived.
	AgentID string
	// Subagent is the sidechain session this Agent (Task) call spawned, once
	// the Reader found its transcript (#2861); nil otherwise.
	Subagent *Subagent
}

// ReadSpan is the window of a file a Read returned: the 1-based first
// line, the lines returned and the file's total.
type ReadSpan struct {
	Start, Lines, Total int
}

// Subagent is a sidechain session an Agent (Task) tool call spawned. Claude
// Code writes it next to the main transcript as
// <session-id>/subagents/agent-<id>.jsonl (every line isSidechain) with an
// agent-<id>.meta.json naming the agent type, the description and the
// tool_use id of the spawning call — the join key.
type Subagent struct {
	// ID is the agent id, the file name's agent-<id>.
	ID string
	// Type and Description come from the meta file; "" until it was read.
	Type        string
	Description string
	// ToolUseID is the tool_use id of the spawning Agent call.
	ToolUseID string
	// Session is the subagent's own timeline. Its tool calls may spawn
	// subagents in turn.
	Session *Session
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

// Files returns every FileRef of every tool event, its subagents' included,
// in timeline order: a subagent's refs interleave with the calls the session
// made meanwhile by their timestamps (an event without one keeps the time of
// the event before it in its own timeline).
func (s *Session) Files() []FileRef {
	type stamped struct {
		at  time.Time
		ref FileRef
	}
	var all []stamped
	var walk func(evs []Event)
	walk = func(evs []Event) {
		var at time.Time
		for i := range evs {
			ev := &evs[i]
			if !ev.At.IsZero() {
				at = ev.At
			}
			t := ev.Tool
			if t == nil {
				continue
			}
			for _, ref := range t.Paths {
				all = append(all, stamped{at, ref})
			}
			if t.Subagent != nil && t.Subagent.Session != nil {
				walk(t.Subagent.Session.Events)
			}
		}
	}
	walk(s.Events)
	sort.SliceStable(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
	out := make([]FileRef, 0, len(all))
	for _, st := range all {
		out = append(out, st.ref)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Timeline resolves a tree node to its event: Events[n.Event], then down
// n.Agent through the subagent sessions. It returns the timeline holding the
// event and the event's index in it; nil, -1 when the node has no event.
func (s *Session) Timeline(n *Node) ([]Event, int) {
	if s == nil || n == nil || n.Event < 0 || n.Event >= len(s.Events) {
		return nil, -1
	}
	evs, idx := s.Events, n.Event
	for _, j := range n.Agent {
		t := evs[idx].Tool
		if t == nil || t.Subagent == nil || t.Subagent.Session == nil {
			return nil, -1
		}
		evs = t.Subagent.Session.Events
		if j < 0 || j >= len(evs) {
			return nil, -1
		}
		idx = j
	}
	return evs, idx
}
