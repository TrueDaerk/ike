package app

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/agentask"
	"ike/internal/agenttrace"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/ui"
)

// agentask.go is the app half of agent.ask (#2845, epic 0540): from the
// selected node of the Agent Trace window, ask a *fork* of the same session
// — full context, a cheaper model, no tools — why it did what it did. The
// question is typed into a ui.Field prompt in the floating shell; the run
// happens off the Update loop (agentask.Run in the session's cwd, so the
// fork lands in the same Claude project directory); the answer replaces the
// prompt in the same shell as glamour-rendered markdown, prefixed by the
// injected context when agent.ask.show_context is on. A missing `claude`
// or a refused resume shows as a dialog in the shell's error accent.
//
// The original session's transcript is never written to: the only process
// started is `claude -p --resume <id> --fork-session …`, whose answer goes
// into the fork's own file. The fork ids are remembered (askForks) and
// passed to discovery as exclusions, so the trace keeps following the
// original even while the fork is the newest file in the directory.
//
// Follow-ups (#2844): `f` on an answer asks again on the same fork —
// `claude -p --resume <fork-id>` without --fork-session, so the fork's
// conversation continues and the original is never named. The fork id is
// kept per traced session and node (askNodeForks) for the IKE session, so
// asking about the same node later continues that conversation too (ctrl+n
// in the prompt starts a fresh fork instead).

// AgentAskMsg runs agent.ask.
type AgentAskMsg struct{}

// askPhase is where the ask stands: typing the question, waiting for the
// fork, showing its answer, or showing why there is none.
type askPhase int

const (
	askPrompting askPhase = iota
	askRunning
	askAnswered
	askFailed
)

// agentAskState is one ask from prompt to answer.
type agentAskState struct {
	phase askPhase
	// gen retires the done and spinner messages of a superseded ask.
	gen int64
	// input is the question line while prompting.
	input   ui.Field
	problem string
	// node is a copy (children dropped) of the selected trace row, nil for a
	// question about the session as a whole; hunk is its change-feed diff.
	node *agenttrace.Node
	hunk string
	// session is the traced session the fork resumes; transcript its file,
	// parsed at run time for the node's context.
	session    agentSession
	transcript string
	question   string
	model      string
	// cancel stops the running fork; frame drives the spinner.
	cancel context.CancelFunc
	frame  int
	// ctx, result and err are the finished run.
	ctx    agentask.Context
	result agentask.Result
	err    error
	// startedAt stamps the run for the elapsed time in the running view.
	startedAt time.Time
	// forkKey names the node's slot in askNodeForks; followUp is the fork
	// the next question resumes, "" to fork the original afresh.
	forkKey  string
	followUp string
	// history holds the earlier exchanges of this overlay, oldest first.
	history []askExchange
	// historyCache is the rendered history at historyW for historyN
	// exchanges.
	historyCache       string
	historyW, historyN int
}

// askExchange is one answered question of a follow-up chain.
type askExchange struct {
	question string
	answer   string
}

// askForkKey is the askNodeForks key of a node of a traced session; a nil
// node stands for the session as a whole.
func askForkKey(sessionID string, node *agenttrace.Node) string {
	if node == nil {
		return sessionID
	}
	return sessionID + "\x00" + node.Key
}

// askDoneMsg is the finished run.
type askDoneMsg struct {
	gen int64
	ctx agentask.Context
	res agentask.Result
	err error
	// followUp is the fork the run resumed, "" for a fresh fork.
	followUp string
}

// askSpinMsg advances the running view's spinner.
type askSpinMsg struct{ gen int64 }

// askSpinInterval is the spinner frame rate — the app-wide braille cycle.
var askSpinInterval = 200 * time.Millisecond

// askNoSession is the notice when there is no traced session to fork.
const askNoSession = "agent ask: open the Agent Trace on a running session first (cmd+alt+shift+a)"

// agentAskOpen reports whether the shell shows the ask prompt or answer.
func (m Model) agentAskOpen() bool { return m.agentAsk != nil && m.shell.IsOpen() }

// openAgentAsk starts an ask about the trace's selected node: the prompt
// opens in the shell. Without a traced session there is nothing to fork,
// and the notice says what to open.
func (m *Model) openAgentAsk() tea.Cmd {
	p := m.agentTracePanel()
	if p == nil || !p.HasSession() {
		m.host.Notify(host.Info, askNoSession)
		return nil
	}
	info := p.Info()
	sess := m.traceSession
	if sess.ID == "" {
		sess.ID = info.ID
	}
	if sess.CWD == "" {
		sess.CWD = info.CWD
	}
	if sess.Transcript == "" {
		sess.Transcript = info.Transcript
	}
	if sess.ID == "" {
		m.host.Notify(host.Info, askNoSession)
		return nil
	}
	if sess.CWD == "" {
		sess.CWD = projectRoot()
	}
	var node *agenttrace.Node
	if cur := p.Current(); cur != nil {
		cp := *cur
		cp.Children = nil
		node = &cp
	}
	m.askGen++
	key := askForkKey(sess.ID, node)
	m.agentAsk = &agentAskState{
		phase:      askPrompting,
		gen:        m.askGen,
		input:      ui.NewField(""),
		node:       node,
		hunk:       m.traceAskHunk(node),
		session:    sess,
		transcript: sess.Transcript,
		model:      askOptions().Model,
		forkKey:    key,
		followUp:   m.askNodeForks[key],
	}
	m.shell.SetAccent(nil)
	m.renderAgentAsk()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return nil
}

// askOptions reads the agent.ask settings.
func askOptions() agentask.Options {
	ask := config.Get().Agent.Ask
	opts := agentask.Options{Model: ask.Model, MaxTurns: ask.MaxTurns, ShowContext: ask.ShowContext}
	if opts.Model == "" {
		opts.Model = agentask.Defaults.Model
	}
	if opts.MaxTurns < 1 {
		opts.MaxTurns = agentask.Defaults.MaxTurns
	}
	return opts
}

// traceAskHunk is the unified diff of the node's linked change-feed entry
// (#2838), "" when the node links to nothing or the entry has no diff.
func (m Model) traceAskHunk(node *agenttrace.Node) string {
	if node == nil {
		return ""
	}
	path := m.traceLinks.Node(node.Key)
	if path == "" {
		return ""
	}
	e, ok := m.feed.Get(path)
	if !ok || !e.HasBefore() {
		return ""
	}
	after, problem := m.changeFeedAfter(e)
	if problem != "" {
		return ""
	}
	return agentask.Hunk(e.Before, after)
}

// closeAgentAsk dismisses the prompt or answer, cancelling a running fork.
func (m *Model) closeAgentAsk() {
	if s := m.agentAsk; s != nil && s.cancel != nil {
		s.cancel()
	}
	m.agentAsk = nil
	m.shell.SetAccent(nil)
	m.shell.Close()
}

// startAgentAsk runs the question off the loop: the transcript is parsed
// for the node's context, the fork command assembled and run in the
// session's working directory.
func (m *Model) startAgentAsk(question string) tea.Cmd {
	s := m.agentAsk
	s.question = question
	s.phase = askRunning
	s.frame = 0
	s.startedAt = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	opts := askOptions()
	s.model = opts.Model
	gen, sess, transcript, node, hunk, fork := s.gen, s.session, s.transcript, s.node, s.hunk, s.followUp
	run := func() tea.Msg {
		if fork != "" {
			// A follow-up: the fork already holds the context.
			argv := agentask.FollowUp(fork, opts, agentask.Prompt(agentask.Context{}, question))
			res, err := agentask.Run(ctx, sess.CWD, argv)
			return askDoneMsg{gen: gen, res: res, err: err, followUp: fork}
		}
		c := agentask.Context{}
		if node != nil {
			var parsed *agenttrace.Session
			if f, err := os.Open(transcript); err == nil {
				parsed, _ = agenttrace.Parse(f)
				f.Close()
			}
			c = agentask.NodeContext(parsed, node)
			c.Hunk = hunk
		}
		argv := agentask.Command(sess.ID, opts, agentask.Prompt(c, question))
		res, err := agentask.Run(ctx, sess.CWD, argv)
		return askDoneMsg{gen: gen, ctx: c, res: res, err: err}
	}
	m.renderAgentAsk()
	return tea.Batch(run, m.armAskSpin(gen))
}

// armAskSpin schedules the next spinner frame.
func (m *Model) armAskSpin(gen int64) tea.Cmd {
	return tea.Tick(askSpinInterval, func(time.Time) tea.Msg { return askSpinMsg{gen: gen} })
}

// handleAskSpin advances the spinner while the fork still runs.
func (m Model) handleAskSpin(msg askSpinMsg) (tea.Model, tea.Cmd) {
	s := m.agentAsk
	if s == nil || msg.gen != s.gen || s.phase != askRunning || !m.shell.IsOpen() {
		return m, nil
	}
	s.frame++
	m.renderAgentAsk()
	return m, m.armAskSpin(msg.gen)
}

// handleAskDone shows the answer — or the failure — of a still-current ask.
// A cancelled run (esc while running) reports nothing.
func (m Model) handleAskDone(msg askDoneMsg) (tea.Model, tea.Cmd) {
	s := m.agentAsk
	if s == nil || msg.gen != s.gen || s.phase != askRunning {
		return m, nil
	}
	s.cancel = nil
	if msg.followUp == "" {
		// A follow-up keeps the context of the ask that forked.
		s.ctx = msg.ctx
	}
	s.result = msg.res
	if msg.res.ForkID != "" {
		m.noteAskFork(msg.res.ForkID)
	}
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		var re *agentask.ResumeError
		if msg.followUp != "" && errors.As(msg.err, &re) {
			// The fork is gone: the next ask about the node forks afresh.
			delete(m.askNodeForks, s.forkKey)
		}
		s.err = msg.err
		s.phase = askFailed
		m.shell.SetAccent(m.pal().Error)
		m.renderAgentAsk()
		return m, nil
	}
	// The fork this answer lives in is what the node's next question resumes.
	if fork := msg.res.ForkID; fork != "" || msg.followUp != "" {
		if fork == "" {
			fork = msg.followUp
			s.result.ForkID = fork
		}
		if m.askNodeForks == nil {
			m.askNodeForks = map[string]string{}
		}
		m.askNodeForks[s.forkKey] = fork
	}
	s.phase = askAnswered
	m.shell.SetAccent(nil)
	m.renderAgentAsk()
	return m, nil
}

// followUpAgentAsk turns the answer overlay back into the prompt, the next
// question resuming the answer's fork; the answer moves into the history.
func (m *Model) followUpAgentAsk() {
	s := m.agentAsk
	if s.result.ForkID == "" {
		return
	}
	s.history = append(s.history, askExchange{question: s.question, answer: s.result.Answer})
	s.followUp = s.result.ForkID
	s.phase = askPrompting
	s.input.Clear()
	s.problem = ""
	s.question = ""
	s.result = agentask.Result{}
	m.renderAgentAsk()
}

// noteAskFork remembers a fork id so discovery skips it.
func (m *Model) noteAskFork(id string) {
	for _, f := range m.askForks {
		if f == id {
			return
		}
	}
	m.askForks = append(m.askForks, id)
}

// updateAgentAsk consumes every key while the ask is up. Prompting: typing
// edits the question, enter asks, esc cancels. Running: esc cancels the
// fork. Answered / failed: esc or q closes, f (answered) follows up on the
// fork, the rest scrolls the shell. ctrl+n in a follow-up prompt drops the
// fork for a fresh one.
func (m Model) updateAgentAsk(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.agentAsk
	switch s.phase {
	case askPrompting:
		switch {
		case msg.Code == tea.KeyEscape:
			m.closeAgentAsk()
			return m, nil
		case msg.Code == tea.KeyEnter:
			q := strings.TrimSpace(s.input.Text)
			if q == "" {
				s.problem = "type a question first"
				m.renderAgentAsk()
				return m, nil
			}
			return m, m.startAgentAsk(q)
		case msg.Code == 'u' && msg.Mod == tea.ModCtrl:
			s.input.Clear()
			s.problem = ""
		case msg.Code == 'n' && msg.Mod == tea.ModCtrl && s.followUp != "":
			// Start over on a fresh fork of the original.
			s.followUp = ""
			s.history = nil
		default:
			s.input.Key(msg)
			s.problem = ""
		}
		m.renderAgentAsk()
		return m, nil
	case askRunning:
		if msg.Code == tea.KeyEscape {
			m.closeAgentAsk()
		}
		return m, nil
	default:
		switch msg.String() {
		case "esc", "q":
			m.closeAgentAsk()
		case "f":
			if s.phase == askAnswered {
				m.followUpAgentAsk()
			}
		default:
			m.shell.Update(msg)
		}
		return m, nil
	}
}

// pasteAgentAskPrompt pastes into the question line.
func (m *Model) pasteAgentAskPrompt(text string) bool {
	s := m.agentAsk
	if s == nil || s.phase != askPrompting {
		return false
	}
	if !s.input.Paste(text) {
		return false
	}
	s.problem = ""
	m.renderAgentAsk()
	return true
}

// renderAgentAsk (re)fills the shell for the current phase.
func (m *Model) renderAgentAsk() {
	s := m.agentAsk
	if s == nil {
		return
	}
	m.shell.SetContent(&askContent{m: m, s: s})
}

// askContent renders the ask at the shell's width budget: the prompt while
// typing, the spinner while running, the markdown answer once it arrived.
type askContent struct {
	m *Model
	s *agentAskState
	// cache holds the last rendered answer by width — glamour is not free.
	cacheW int
	cache  string
}

// Title implements ui.Content.
func (c *askContent) Title() string {
	switch c.s.phase {
	case askPrompting:
		return "Ask the agent"
	case askRunning:
		return "Asking " + c.s.model + "…"
	case askFailed:
		return "Ask failed"
	}
	return "Agent (" + c.s.model + ")"
}

// Render implements ui.Content.
func (c *askContent) Render(width int) string {
	s := c.s
	pal := c.m.pal()
	faint := lipgloss.NewStyle().Faint(true)
	width = max(20, width)
	var sb strings.Builder
	switch s.phase {
	case askPrompting:
		about := agentask.NodeContext(nil, s.node).DisplayLines(displayPath)
		if len(about) == 0 {
			about = []string{"the whole session"}
		}
		sb.WriteString(c.historyView(width))
		sb.WriteString(faint.Render("about: "+strings.Join(about, " · ")) + "\n")
		if s.followUp != "" {
			sb.WriteString(faint.Render("follow-up on fork "+shortID(s.followUp)) + "\n")
		}
		sb.WriteString("> " + s.input.View() + "\n")
		if s.problem != "" {
			sb.WriteString(lipgloss.NewStyle().Foreground(pal.Warning).Render(s.problem) + "\n")
		}
		hint := "enter ask on " + s.model + " (a fork; the session is not touched) · esc cancel"
		if s.followUp != "" {
			hint = "enter continue the fork on " + s.model + " · ctrl+n new fork · esc cancel"
		}
		sb.WriteString("\n" + faint.Render(hint))
		return lipgloss.NewStyle().Width(width).Render(sb.String())
	case askRunning:
		frame := playSpinFrames[s.frame%len(playSpinFrames)]
		elapsed := time.Since(s.startedAt).Round(time.Second)
		doing := "forking the session"
		if s.followUp != "" {
			doing = "asking fork " + shortID(s.followUp)
		}
		sb.WriteString(frame + " " + doing + " on " + s.model + "… " + faint.Render(elapsed.String()) + "\n")
		sb.WriteString(faint.Render("Q: "+s.question) + "\n\n")
		sb.WriteString(faint.Render("esc cancels"))
		return lipgloss.NewStyle().Width(width).Render(sb.String())
	case askFailed:
		sb.WriteString(lipgloss.NewStyle().Foreground(pal.Error).Render(s.err.Error()) + "\n\n")
		switch {
		case errors.Is(s.err, agentask.ErrNotInstalled):
			sb.WriteString("Install Claude Code and make sure `claude` is on PATH, then ask again.\n")
		default:
			var re *agentask.ResumeError
			if errors.As(s.err, &re) {
				sb.WriteString("The fork ran in " + displayPath(s.session.CWD) + ". Claude Code resumes a session only from the directory it was started in; the traced session may also have been deleted.\n")
			}
		}
		sb.WriteString("\n" + faint.Render("esc close"))
		return lipgloss.NewStyle().Width(width).Render(sb.String())
	}
	// Answered.
	if c.cacheW != width || c.cache == "" {
		var body strings.Builder
		body.WriteString(c.historyView(width))
		body.WriteString(lipgloss.NewStyle().Bold(true).Render("Q: "+s.question) + "\n")
		if askOptions().ShowContext {
			if lines := s.ctx.DisplayLines(displayPath); len(lines) > 0 {
				body.WriteString(faint.Render("context:") + "\n")
				for _, l := range lines {
					for _, ll := range strings.Split(l, "\n") {
						body.WriteString(faint.Render("  "+ll) + "\n")
					}
				}
			}
		}
		body.WriteString("\n" + agentask.RenderMarkdown(s.result.Answer, width, pal) + "\n")
		meta := []string{}
		if s.result.Duration > 0 {
			meta = append(meta, s.result.Duration.Round(100*time.Millisecond).String())
		}
		if s.result.CostUSD > 0 {
			meta = append(meta, "$"+trimFloat(s.result.CostUSD))
		}
		if s.result.ForkID != "" {
			meta = append(meta, "fork "+shortID(s.result.ForkID))
		}
		keys := "esc close"
		if s.result.ForkID != "" {
			keys = "f follow up · " + keys
		}
		body.WriteString("\n" + faint.Render(strings.Join(append(meta, keys), " · ")))
		c.cache = lipgloss.NewStyle().Width(width).Render(body.String())
		c.cacheW = width
	}
	return c.cache
}

// historyView renders the earlier exchanges of a follow-up chain, each
// question over its markdown answer, "" without any. The render is cached
// on the state per width and history length: the prompt re-renders on every
// key.
func (c *askContent) historyView(width int) string {
	s := c.s
	if len(s.history) == 0 {
		return ""
	}
	if s.historyW == width && s.historyN == len(s.history) {
		return s.historyCache
	}
	var sb strings.Builder
	for _, ex := range s.history {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Q: "+ex.question) + "\n\n")
		sb.WriteString(agentask.RenderMarkdown(ex.answer, width, c.m.pal()) + "\n\n")
	}
	s.historyCache, s.historyW, s.historyN = sb.String(), width, len(s.history)
	return s.historyCache
}

// shortID trims a session id to its first block.
func shortID(id string) string {
	if i := strings.IndexByte(id, '-'); i > 0 {
		return id[:i]
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// trimFloat prints a cost with up to four decimals, no trailing zeros.
func trimFloat(f float64) string {
	s := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', 4, 64), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}
