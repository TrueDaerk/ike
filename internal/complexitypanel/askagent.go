package complexitypanel

// askagent.go is the pane's 'a' (#2964): ask a coding agent to simplify
// the selected function. The pane only names the function — the root model
// reads its source, builds the prompt and runs the agent.

import (
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
