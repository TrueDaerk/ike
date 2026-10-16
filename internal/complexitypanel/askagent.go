package complexitypanel

// askagent.go is the pane's 'a' (#2964): ask a coding agent to simplify
// the selected function. The pane only names the function — the root model
// reads its source, builds the prompt and runs the agent. While a run is in
// flight (#3009) the root model hands the pane its slot (SetAgentRunning):
// the function's row carries agentMark, and esc on it cancels the run.

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// AskAgentMsg asks the root model to run the simplify agent on one
// function row. Line / EndLine are the function's 0-based range, Fn the
// whole measured unit (its sites and extraction candidates feed the
// prompt), Lang the file's language id.
type AskAgentMsg struct {
	Path      string
	Line      int
	EndLine   int
	Name      string
	CCN       int
	Cognitive int
	Lang      string
	Fn        complexity.Function
}

// AskAgent reports the request for the selected row: a function row of
// any tab (a site or candidate row stands for its parent); false on a group
// or file row, a removed function, a Trend row or no selection.
func (m *Model) AskAgent() (AskAgentMsg, bool) {
	r, ok := m.Selected()
	if ok && r.child() && r.Parent >= 0 && r.Parent < len(m.rows) {
		r = m.rows[r.Parent]
	}
	if !ok || r.aggregate() || r.Trend != nil || r.Gone || r.Change == complexity.Removed || r.Path == "" || r.Fn.Name == "" {
		return AskAgentMsg{}, false
	}
	return AskAgentMsg{
		Path: r.Path, Line: r.Fn.Line, EndLine: r.Fn.EndLine, Name: r.Fn.Name,
		CCN: r.Fn.CCN, Cognitive: r.Fn.Cognitive, Lang: r.Lang, Fn: r.Fn,
	}, true
}

// askAgent is the pane's 'a'; a row without a function says so in the
// note line.
func (m *Model) askAgent() tea.Cmd {
	msg, ok := m.AskAgent()
	if !ok {
		m.note = "a asks the agent about a function row"
		return nil
	}
	return func() tea.Msg { return msg }
}

// agentMark leads the function cell of a row the agent is working on.
const agentMark = "⟳ "

// AgentKey names one function's agent slot: path, name and header line.
func AgentKey(path, name string, line int) string {
	return path + "\x00" + name + "#" + strconv.Itoa(line)
}

// Key is the request's AgentKey.
func (a AskAgentMsg) Key() string { return AgentKey(a.Path, a.Name, a.Line) }

// CancelAgentMsg asks the root model to cancel the run of one function:
// esc on its running row (#3009).
type CancelAgentMsg struct {
	AskAgentMsg
}

// SetAgentRunning hands the pane the slots of the runs in flight (nil or
// empty: none). The pane keeps its own copy.
func (m *Model) SetAgentRunning(keys map[string]bool) {
	m.agentRunning = nil
	for k, on := range keys {
		if !on {
			continue
		}
		if m.agentRunning == nil {
			m.agentRunning = map[string]bool{}
		}
		m.agentRunning[k] = true
	}
}

// agentRunningOn reports a function row the agent is working on.
func (m *Model) agentRunningOn(r Row) bool {
	if len(m.agentRunning) == 0 || r.Path == "" || r.Fn.Name == "" || r.aggregate() || r.Trend != nil {
		return false
	}
	return m.agentRunning[AgentKey(r.Path, r.Fn.Name, r.Fn.Line)]
}

// selectedAgentRunning reports whether the selected row's function (a
// site or candidate row standing for its parent) has a run in flight.
func (m *Model) selectedAgentRunning() bool {
	req, ok := m.AskAgent()
	return ok && m.agentRunning[req.Key()]
}

// cancelAgent is the pane's esc: cancel the selected function's run; on
// any other row esc does nothing here.
func (m *Model) cancelAgent() tea.Cmd {
	if !m.selectedAgentRunning() {
		return nil
	}
	req, _ := m.AskAgent()
	return func() tea.Msg { return CancelAgentMsg{AskAgentMsg: req} }
}
