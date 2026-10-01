package agenttrace

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// path.go reduces a Session to the change path the trace pane's graph view
// draws (#2858): for every turn the prompt, one stop per file a tool call
// changed, in order, and the agent's answer — the next turn continues the
// path. Reasoning and the tool calls themselves are secondary; a change
// stop carries them (Calls, Context) for the expanded detail block. Like
// BuildTree it is a pure function of the Session, keyed by event index, so
// a host rebuilds it after every incremental read and carries selection and
// expansion across rebuilds by key.

// StopKind classifies a Stop.
type StopKind int

const (
	// StopPrompt is the user's prompt that opens a turn (or the "session
	// start" of events before the first prompt).
	StopPrompt StopKind = iota
	// StopChange is one file a tool call created, wrote, edited or deleted.
	StopChange
	// StopAnswer is the turn's final assistant text — implicit when the turn
	// ended on a tool call.
	StopAnswer
	// StopSeparator is a compaction or summary boundary, drawn as a marker.
	StopSeparator
)

// String returns the lowercase name used in tests.
func (k StopKind) String() string {
	switch k {
	case StopPrompt:
		return "prompt"
	case StopChange:
		return "change"
	case StopAnswer:
		return "answer"
	case StopSeparator:
		return "separator"
	}
	return "unknown"
}

// StopCall is one tool call behind a change stop.
type StopCall struct {
	Name  string
	Title string
	// Error marks a failed call, Pending one whose result is outstanding.
	Error   bool
	Pending bool
}

// Stop is one box on the change path.
type Stop struct {
	Kind StopKind
	// Key identifies the stop across rebuilds and matches the tree's node
	// keys where both show the same thing: "t<turn>" for a prompt, the file
	// node key "e<index>/f<n>" (a subagent's "e<index>/a<j>/f<n>") for a
	// change, "t<turn>/end" for an answer, "e<index>" for a separator. A
	// change stop therefore takes the change-feed links (#2838) of its file
	// node.
	Key  string
	Turn int
	// Label is the box text: "#<turn> <prompt>", the file name, the answer's
	// first line. Detail is the second line: time, "edit :12 +3 −1", "…".
	Label  string
	Detail string
	// Text is the whole prompt or answer text; "" for the other kinds.
	Text string
	// Ref is the changed file (change stops only).
	Ref *FileRef
	// Events are the session event indices behind the stop: the prompt, the
	// tool call(s), the answer text. Empty for an implicit answer.
	Events []int
	// Agent leads from Events[0] down to a subagent's call, like Node.Agent.
	Agent []int
	At    time.Time
	// Error marks a change whose call failed; Pending a change whose result
	// is outstanding or an answer the agent is still working on.
	Error   bool
	Pending bool
	// Implicit marks an answer the turn never spoke: it ended on a tool call.
	Implicit bool
	// Calls are the tool call(s) that produced a change; Context the
	// assistant text and reasoning that preceded it in the turn (labels),
	// both for the expanded detail block.
	Calls   []StopCall
	Context []string
	// Count is how many edits of the file the call made (MultiEdit).
	Count int
	// Added and Removed count the recorded patch's lines when HasDiff.
	Added, Removed int
	HasDiff        bool
}

// Selectable reports whether the stop is a box the user can land on;
// separators are markers.
func (s Stop) Selectable() bool { return s.Kind != StopSeparator }

// writing reports whether the op changes the file.
func writing(op Op) bool {
	switch op {
	case OpCreate, OpWrite, OpEdit, OpDelete:
		return true
	}
	return false
}

// BuildPath reduces the session to its change path. Events before the first
// prompt form a leading "session start" turn; turns agent.ask prompted
// (IsAskPrompt) are left out, as in BuildTree. The last turn's answer is
// pending while the turn ended on a tool call: the transcript alone cannot
// tell a working agent from one that stopped, so a host that knows the
// session ended (the hook's SessionEnd) settles it with Settle.
func BuildPath(s *Session) []Stop {
	if s == nil {
		return nil
	}
	var out []Stop
	b := pathBuilder{}
	asked := -1
	for i := range s.Events {
		ev := s.Events[i]
		if ev.Kind == KindUser && IsAskPrompt(ev.Text) {
			asked = ev.Turn
		}
		if ev.Turn == asked {
			continue
		}
		if !b.open || b.turn != ev.Turn {
			out = b.close(out, false)
			b = pathBuilder{open: true, turn: ev.Turn, lastAt: ev.At}
			p := Stop{Kind: StopPrompt, Key: "t" + strconv.Itoa(ev.Turn), Turn: ev.Turn, At: ev.At}
			if ev.Kind == KindUser {
				p.Label = "#" + strconv.Itoa(ev.Turn) + " " + Collapse(ev.Text)
				p.Text = ev.Text
				p.Events = []int{i}
			} else {
				p.Label = "session start"
			}
			if !ev.At.IsZero() {
				p.Detail = ev.At.Local().Format("15:04")
			}
			out = append(out, p)
		}
		if !ev.At.IsZero() {
			b.lastAt = ev.At
		}
		switch ev.Kind {
		case KindAssistant:
			b.context = append(b.context, Label(ev.Text))
			if !ev.Reasoning {
				b.text, b.textEvent, b.textAt = ev.Text, i, ev.At
				b.textAfterTool = true
			}
		case KindSeparator:
			out = append(out, Stop{Kind: StopSeparator, Key: "e" + strconv.Itoa(i), Turn: ev.Turn, Label: Label(ev.Text), Events: []int{i}, At: ev.At})
		case KindTool:
			if ev.Tool == nil {
				continue
			}
			b.textAfterTool = false
			b.sawTool = true
			out = b.changes(out, s.Events, i, "e"+strconv.Itoa(i), nil)
		}
	}
	return b.close(out, true)
}

// pathBuilder is the state of the turn being reduced.
type pathBuilder struct {
	open bool
	turn int
	// context collects the assistant labels since the last change.
	context []string
	// text is the turn's latest assistant text; textAfterTool whether it
	// came after the turn's last tool call.
	text          string
	textEvent     int
	textAt        time.Time
	textAfterTool bool
	sawTool       bool
	lastAt        time.Time
}

// close appends the turn's answer stop. running marks the session as still
// open, which makes an implicit answer of the turn pending.
func (b *pathBuilder) close(out []Stop, running bool) []Stop {
	if !b.open {
		return out
	}
	a := Stop{Kind: StopAnswer, Key: "t" + strconv.Itoa(b.turn) + "/end", Turn: b.turn, Text: b.text, At: b.lastAt}
	switch {
	case b.text != "" && (b.textAfterTool || !b.sawTool):
		a.Label = Collapse(b.text)
		a.Events = []int{b.textEvent}
		a.At = b.textAt
		if !a.At.IsZero() {
			a.Detail = a.At.Local().Format("15:04")
		}
	default:
		a.Implicit = true
		if b.text != "" {
			a.Label = Collapse(b.text)
			a.Events = []int{b.textEvent}
		}
		switch {
		case running:
			a.Pending = true
			a.Detail = "working …"
			if a.Label == "" {
				a.Label = "working …"
			}
		default:
			a.Detail = "ended on a tool call"
			if a.Label == "" {
				a.Label = "(no answer)"
			}
		}
	}
	b.open = false
	return append(out, a)
}

// changes appends one stop per file the call at evs[i] changed, then the
// changes of the subagent it spawned, if any. key is the call's node key,
// agent its path into the subagent sessions.
func (b *pathBuilder) changes(out []Stop, evs []Event, i int, key string, agent []int) []Stop {
	ev := evs[i]
	tool := ev.Tool
	call := StopCall{Name: tool.Name, Title: Label(tool.Title), Error: tool.IsError, Pending: !tool.Done}
	// Several edits of one file inside one call stay one stop (MultiEdit).
	first := map[string]int{}
	for j, ref := range tool.Paths {
		if !writing(ref.Op) {
			continue
		}
		if k, seen := first[ref.Path]; seen {
			out[k].Count++
			continue
		}
		r := ref
		st := Stop{
			Kind: StopChange, Key: key + "/f" + strconv.Itoa(j), Turn: b.turn,
			Label: filepath.Base(ref.Path), Ref: &r, Events: []int{i}, Agent: agent, At: ev.At,
			Error: tool.IsError, Pending: !tool.Done, Calls: []StopCall{call},
			Context: append([]string(nil), b.context...), Count: 1,
		}
		st.Added, st.Removed, st.HasDiff = patchCounts(tool.Result)
		first[ref.Path] = len(out)
		out = append(out, st)
	}
	for _, k := range first {
		out[k].Detail = changeDetail(out[k])
	}
	if len(first) > 0 {
		b.context = nil
	}
	if sub := tool.Subagent; sub != nil && sub.Session != nil {
		saved := b.context
		b.context = nil
		for j := range sub.Session.Events {
			sev := sub.Session.Events[j]
			switch {
			case sev.Kind == KindAssistant:
				b.context = append(b.context, Label(sev.Text))
			case sev.Kind == KindTool && sev.Tool != nil:
				path := append(append(make([]int, 0, len(agent)+1), agent...), j)
				out = b.changes(out, sub.Session.Events, j, key+"/a"+strconv.Itoa(j), path)
			}
		}
		b.context = saved
	}
	return out
}

// changeDetail is the second line of a change box: the op, the line, the
// edit count and the patch summary, then the call's status.
func changeDetail(st Stop) string {
	d := st.Ref.Op.String()
	if st.Ref.Line > 0 {
		d += " :" + strconv.Itoa(st.Ref.Line)
	}
	if st.Count > 1 {
		d += " ×" + strconv.Itoa(st.Count)
	}
	if st.HasDiff {
		d += " +" + strconv.Itoa(st.Added) + " −" + strconv.Itoa(st.Removed)
	}
	switch {
	case st.Error:
		d += " ✗"
	case st.Pending:
		d += " …"
	}
	return d
}

// patchCounts sums the added and removed lines of a structured result's
// patch; ok is false when the result records no patch.
func patchCounts(raw json.RawMessage) (added, removed int, ok bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return 0, 0, false
	}
	var r struct {
		StructuredPatch []struct {
			Lines []string `json:"lines"`
		} `json:"structuredPatch"`
	}
	if json.Unmarshal(raw, &r) != nil || len(r.StructuredPatch) == 0 {
		return 0, 0, false
	}
	for _, h := range r.StructuredPatch {
		for _, l := range h.Lines {
			switch {
			case strings.HasPrefix(l, "+"):
				added++
			case strings.HasPrefix(l, "-"):
				removed++
			}
		}
	}
	return added, removed, true
}

// Settle marks the session as ended: a pending answer — the last turn ended
// on a tool call — becomes a plain implicit one, since no more text will
// arrive. The path is changed in place.
func Settle(path []Stop) {
	for i := range path {
		st := &path[i]
		if st.Kind != StopAnswer || !st.Pending {
			continue
		}
		st.Pending = false
		st.Detail = "ended on a tool call"
		if st.Label == "working …" {
			st.Label = "(no answer)"
		}
	}
}

// Collapse reduces text to its first non-empty line with runs of
// whitespace collapsed to one space, capped like Label.
func Collapse(text string) string {
	return Label(strings.Join(strings.Fields(Label(text)), " "))
}
