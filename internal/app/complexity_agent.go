package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agentask"
	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/host"
	"ike/internal/scratch"
)

// complexity_agent.go is the Complexity window's `a` (#2964, epic 0551):
// ask a coding agent to simplify the selected function. The prompt carries
// the function's source (the dirty buffer when the file is open and unsaved,
// else the disk), its metrics, its complexity sites and its best extraction
// candidate, then the fixed instruction (agentask.SimplifyPrompt). The run
// is a fresh `claude -p` session — no --resume — on the agent.ask options
// in the project root, off the Update loop (agentask.Run); the answer opens
// as a markdown scratch with the diff fenced. One run per function at a
// time: `a` again while it runs only notifies. A missing `claude` reuses
// agent.ask's install message, a failed run notifies with the runner's
// error (the first stderr line). The session the run leaves behind is
// excluded from trace discovery like an ask fork.

// ComplexityAskAgentMsg runs complexity.askAgent (#2964), a in the pane.
type ComplexityAskAgentMsg struct{}

// askInstallHint is what a missing `claude` says, shared with agent.ask.
const askInstallHint = "Install Claude Code and make sure `claude` is on PATH, then ask again."

// complexityAgentState tracks the runs in flight, shared by pointer like
// cxDiff.
type complexityAgentState struct {
	running map[string]context.CancelFunc
}

// cxAgentKey names one function's slot: path, name and header line.
func cxAgentKey(req complexitypanel.AskAgentMsg) string {
	return req.Path + "\x00" + req.Name + "#" + strconv.Itoa(req.Line)
}

// cxAgentDoneMsg is a finished run.
type cxAgentDoneMsg struct {
	key  string
	req  complexitypanel.AskAgentMsg
	res  agentask.Result
	err  error
	argv []string // the command run (tests)
}

// complexityAskAgent is complexity.askAgent from the palette: the pane's
// 'a' for the selected row.
func (m *Model) complexityAskAgent() tea.Cmd {
	if p := m.complexityPanel(); p != nil {
		if req, ok := p.AskAgent(); ok {
			return m.startComplexityAgent(req)
		}
	}
	m.host.Notify(host.Info, "complexity: select a function row to ask the agent about it")
	return nil
}

// startComplexityAgent builds the prompt and launches the run.
func (m *Model) startComplexityAgent(req complexitypanel.AskAgentMsg) tea.Cmd {
	if m.cxAgent == nil {
		m.cxAgent = &complexityAgentState{running: map[string]context.CancelFunc{}}
	}
	key := cxAgentKey(req)
	if _, busy := m.cxAgent.running[key]; busy {
		m.host.Notify(host.Info, "complexity: the agent is still working on "+req.Name)
		return nil
	}
	source, err := m.complexityFunctionSource(req)
	if err != nil {
		m.host.Notify(host.Error, "complexity: "+err.Error())
		return nil
	}
	prompt := agentask.SimplifyPrompt(complexitySimplify(req, displayPath(req.Path), source))
	opts := askOptions()
	argv := agentask.FreshCommand(opts, prompt)
	dir := projectRoot()
	ctx, cancel := context.WithCancel(context.Background())
	m.cxAgent.running[key] = cancel
	m.host.Notify(host.Info, "complexity: asking "+opts.Model+" to simplify "+req.Name+"…")
	return func() tea.Msg {
		res, err := agentask.Run(ctx, dir, argv)
		return cxAgentDoneMsg{key: key, req: req, res: res, err: err, argv: argv}
	}
}

// complexitySimplify converts a pane request into the runner's request.
func complexitySimplify(req complexitypanel.AskAgentMsg, path, source string) agentask.Simplify {
	s := agentask.Simplify{
		Path: path, Lang: req.Lang, Name: req.Name, Line: req.Line, EndLine: req.EndLine,
		CCN: req.CCN, Cognitive: req.Cognitive, Source: source,
	}
	for _, site := range req.Fn.Sites {
		s.Sites = append(s.Sites, agentask.SimplifySite{Line: site.Line, Kind: site.Kind, CCN: site.CCN, Cognitive: site.Cognitive, Nesting: site.Nesting})
	}
	if len(req.Fn.Candidates) > 0 {
		c := bestCandidate(req.Fn.Candidates)
		s.Candidate = &agentask.SimplifyCandidate{
			Line: c.Line, EndLine: c.EndLine, Kind: c.Kind, CogSaving: c.CogSaving,
			NewCCN: c.NewCCN, NewCog: c.NewCog, RestCCN: c.RestCCN, RestCog: c.RestCog,
		}
	}
	return s
}

// bestCandidate is the candidate saving the most cognitive complexity —
// the engine lists them best first, but the saving decides regardless.
func bestCandidate(cands []complexity.Candidate) complexity.Candidate {
	best := cands[0]
	for _, c := range cands[1:] {
		if c.CogSaving > best.CogSaving {
			best = c
		}
	}
	return best
}

// complexityFunctionSource is the function's text: the lines Line..EndLine
// of the open buffer when the file is open, else of the file on disk.
func (m Model) complexityFunctionSource(req complexitypanel.AskAgentMsg) (string, error) {
	var text string
	if ed := m.editorForPath(req.Path); ed != nil && ed.HasFile() {
		text = ed.Text()
	} else {
		data, err := os.ReadFile(req.Path)
		if err != nil {
			return "", err
		}
		text = string(data)
	}
	lines := strings.Split(text, "\n")
	from, to := req.Line, req.EndLine
	if from < 0 || from >= len(lines) {
		return "", errors.New(filepath.Base(req.Path) + " no longer holds line " + strconv.Itoa(req.Line+1) + "; rescan the project")
	}
	if to < from {
		to = from
	}
	if to >= len(lines) {
		to = len(lines) - 1
	}
	return strings.Join(lines[from:to+1], "\n"), nil
}

// handleComplexityAgentDone shows the answer — or the failure — of a run.
func (m Model) handleComplexityAgentDone(msg cxAgentDoneMsg) (tea.Model, tea.Cmd) {
	if m.cxAgent != nil {
		if cancel, ok := m.cxAgent.running[msg.key]; ok {
			cancel()
			delete(m.cxAgent.running, msg.key)
		}
	}
	if msg.res.ForkID != "" {
		// The fresh session is not the traced one: discovery skips it.
		m.noteAskFork(msg.res.ForkID)
	}
	if msg.err != nil {
		switch {
		case errors.Is(msg.err, context.Canceled):
			return m, nil
		case errors.Is(msg.err, agentask.ErrNotInstalled):
			m.host.Notify(host.Error, "complexity: "+msg.err.Error()+" "+askInstallHint)
		default:
			m.host.Notify(host.Error, "complexity: "+msg.err.Error())
		}
		return m, nil
	}
	body := complexityAgentScratch(msg.req, msg.res.Answer)
	path, err := scratch.CreateWithContent("md", []byte(body))
	if err != nil {
		m.host.Notify(host.Error, "complexity: could not write the answer: "+err.Error())
		return m, nil
	}
	m.host.Notify(host.Info, "complexity: the agent's answer for "+msg.req.Name+" opened as "+displayPath(path))
	return m.openPathAt(path, 0, 0)
}

// complexityAgentScratch is the scratch file: a heading naming the
// function, the answer with its diff fenced.
func complexityAgentScratch(req complexitypanel.AskAgentMsg, answer string) string {
	var sb strings.Builder
	sb.WriteString("# Simplify " + req.Name + "\n\n")
	sb.WriteString(displayPath(req.Path) + ":" + strconv.Itoa(req.Line+1) + " — CCN " + strconv.Itoa(req.CCN) + ", cognitive " + strconv.Itoa(req.Cognitive) + "\n\n")
	sb.WriteString(fenceDiff(answer))
	sb.WriteString("\n")
	return sb.String()
}

// fenceDiff wraps a bare unified diff in the answer into a ```diff fence
// so the scratch renders it; an answer that already carries fences is
// left alone. The diff starts at the first "--- " / "diff " line and runs
// while the lines look like diff lines.
func fenceDiff(answer string) string {
	answer = strings.TrimSpace(answer)
	if strings.Contains(answer, "```") {
		return answer
	}
	lines := strings.Split(answer, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "diff ") {
			start = i
			break
		}
	}
	if start < 0 {
		return answer
	}
	end := start
	for end < len(lines) && isDiffLine(lines[end]) {
		end++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end-- // blank lines after the diff stay outside the fence
	}
	var out []string
	out = append(out, lines[:start]...)
	if start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		out = append(out, "")
	}
	out = append(out, "```diff")
	out = append(out, lines[start:end]...)
	out = append(out, "```")
	if end < len(lines) && strings.TrimSpace(lines[end]) != "" {
		out = append(out, "")
	}
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// isDiffLine reports a line that belongs to a unified diff.
func isDiffLine(l string) bool {
	if l == "" {
		return true
	}
	switch l[0] {
	case ' ', '+', '-', '@', '\\':
		return true
	}
	return strings.HasPrefix(l, "diff ") || strings.HasPrefix(l, "index ")
}
