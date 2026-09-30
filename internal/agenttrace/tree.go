package agenttrace

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// tree.go groups a Session's flat timeline into the trace tree the tool
// window shows (#2840): turn → assistant decision → tool call → file. It is
// a pure function of the Session, so the host rebuilds it after every
// incremental read; node keys are derived from event indices, which the
// append-only Events slice keeps stable, so a tree host can carry expansion
// and selection across rebuilds by key.

// NodeKind classifies a trace tree node.
type NodeKind int

const (
	// NodeTurn is one user prompt with everything the agent did in reply.
	NodeTurn NodeKind = iota
	// NodeDecision is one assistant text or thinking block; the tool calls
	// that followed it nest below.
	NodeDecision
	// NodeTool is one tool call.
	NodeTool
	// NodeFile is one file a tool call touched.
	NodeFile
	// NodeSeparator is a compaction or summary boundary inside a turn.
	NodeSeparator
)

// String returns the lowercase name used in tests.
func (k NodeKind) String() string {
	switch k {
	case NodeTurn:
		return "turn"
	case NodeDecision:
		return "decision"
	case NodeTool:
		return "tool"
	case NodeFile:
		return "file"
	case NodeSeparator:
		return "separator"
	}
	return "unknown"
}

// Node is one row of the trace tree.
type Node struct {
	Kind NodeKind
	// Key identifies the node across rebuilds: "t<turn>" for a turn,
	// "e<index>" for the event behind a decision or tool call, "e<index>/f<n>"
	// for a file, "e<index>/x" for the implicit decision that groups tool
	// calls issued before any assistant text of the turn.
	Key string
	// Label is the row text; Detail the faint suffix (time, status, op).
	Label  string
	Detail string
	// Turn is the turn the node belongs to; Event indexes Session.Events
	// (-1 for a turn).
	Turn  int
	Event int
	// Ref is set when the node opens a file: every file node, and a tool
	// node whose call touched exactly one file.
	Ref *FileRef
	// Path is the location shown beside the row: the file of a file node or
	// of a single-file tool call. "" for the other nodes.
	Path string
	// Error marks a tool call whose result was an error; Pending one whose
	// result has not arrived.
	Error   bool
	Pending bool
	// At and Until bound a tool call (and its file nodes) in time: when the
	// call was issued and when its result arrived (zero while pending).
	// Change-feed linking (link.go) matches external writes against them.
	At    time.Time
	Until time.Time

	Children []Node
}

// MaxLabel caps a node label in runes; longer text is cut with an ellipsis.
const MaxLabel = 80

// BuildTree groups the session's events into turns. Events before the first
// prompt (a resumed session's compaction, for instance) form a leading
// "session start" turn.
func BuildTree(s *Session) []Node {
	if s == nil {
		return nil
	}
	// Turns are built behind pointers: the decision a tool call nests under
	// must stay valid while the turn's children grow.
	var turns []*Node
	var cur *Node      // the turn being filled
	var decision *Node // the decision tool calls nest under
	turnFor := func(ev Event, i int) *Node {
		if cur != nil && cur.Turn == ev.Turn {
			return cur
		}
		cur = &Node{Kind: NodeTurn, Key: "t" + strconv.Itoa(ev.Turn), Turn: ev.Turn, Event: -1}
		turns = append(turns, cur)
		decision = nil
		if ev.Kind == KindUser {
			cur.Label = "#" + strconv.Itoa(ev.Turn) + " " + Label(ev.Text)
			cur.Event = i
		} else {
			cur.Label = "session start"
		}
		if !ev.At.IsZero() {
			cur.Detail = ev.At.Local().Format("15:04")
		}
		return cur
	}
	for i := range s.Events {
		ev := s.Events[i]
		t := turnFor(ev, i)
		switch ev.Kind {
		case KindUser:
			// The prompt is the turn row itself.
		case KindAssistant:
			n := Node{Kind: NodeDecision, Key: "e" + strconv.Itoa(i), Turn: ev.Turn, Event: i, Label: Label(ev.Text)}
			if ev.Reasoning {
				n.Detail = "thinking"
			}
			t.Children = append(t.Children, n)
			decision = &t.Children[len(t.Children)-1]
		case KindSeparator:
			t.Children = append(t.Children, Node{Kind: NodeSeparator, Key: "e" + strconv.Itoa(i), Turn: ev.Turn, Event: i, Label: "— " + Label(ev.Text) + " —"})
			decision = nil
		case KindTool:
			if ev.Tool == nil {
				continue
			}
			if decision == nil {
				t.Children = append(t.Children, Node{Kind: NodeDecision, Key: "e" + strconv.Itoa(i) + "/x", Turn: ev.Turn, Event: i, Label: "tool calls"})
				decision = &t.Children[len(t.Children)-1]
			}
			decision.Children = append(decision.Children, toolNode(ev, i))
		}
	}
	out := make([]Node, len(turns))
	for i, t := range turns {
		out[i] = *t
	}
	return out
}

// toolNode builds the row of one tool call with its files below.
func toolNode(ev Event, i int) Node {
	tool := ev.Tool
	n := Node{Kind: NodeTool, Key: "e" + strconv.Itoa(i), Turn: ev.Turn, Event: i, Label: tool.Name, Error: tool.IsError, Pending: !tool.Done, At: ev.At, Until: tool.DoneAt}
	switch {
	case len(tool.Paths) == 1:
		ref := tool.Paths[0]
		n.Ref = &ref
		n.Path = ref.Path
	case len(tool.Paths) > 1:
		n.Detail = strconv.Itoa(len(tool.Paths)) + " files"
	default:
		n.Detail = Label(tool.Title)
	}
	switch {
	case tool.IsError:
		n.Detail = strings.TrimSpace(n.Detail + " ✗ error")
	case !tool.Done:
		n.Detail = strings.TrimSpace(n.Detail + " …")
	}
	for j := range tool.Paths {
		ref := tool.Paths[j]
		n.Children = append(n.Children, Node{
			Kind: NodeFile, Key: n.Key + "/f" + strconv.Itoa(j), Turn: ev.Turn, Event: i,
			Label: ref.Op.String(), Ref: &ref, Path: ref.Path, At: ev.At, Until: tool.DoneAt,
		})
	}
	return n
}

// Label reduces text to its first non-empty line, capped at MaxLabel runes.
func Label(text string) string {
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	if utf8.RuneCountInString(line) <= MaxLabel {
		return line
	}
	rs := []rune(line)
	return strings.TrimSpace(string(rs[:MaxLabel-1])) + "…"
}

// Walk visits every node depth-first, parents before children.
func Walk(nodes []Node, visit func(n *Node)) {
	for i := range nodes {
		visit(&nodes[i])
		Walk(nodes[i].Children, visit)
	}
}
