package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agentask"
	"ike/internal/agenttrace"
	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/scratch"
	"ike/internal/telemetry"
)

// complexity_agent.go is the Complexity window's `a` (#2964, epic 0551):
// ask a coding agent to simplify the selected function. The prompt carries
// the function's source (the dirty buffer when the file is open and unsaved,
// else the disk), its metrics, its complexity sites and its best extraction
// candidate, then the fixed instruction (agentask.SimplifyPrompt). The run
// is a fresh tool-less `claude -p` session — no --resume — on the agent.ask
// options in the project root, off the Update loop (agentask.RunSimplify);
// an answer without a diff gets one follow-up in the same session.
//
// With complexity.agent_apply on (#3009, the default) the answer's unified
// diff is applied to the function's open buffer (opened when needed) as one
// undoable edit — dirty, unsaved, never through git — the editor jumps to
// the function, the scratch with the agent's explanation opens in a
// background tab, and a notification reports the CCN / cognitive before →
// after, re-measured from the buffer. A hunk that does not match applies
// nothing and opens the scratch in front. Off restores the scratch-only
// behaviour of #2964.
//
// While a run is in flight the status line shows "[agent: simplify <fn>
// m:ss]" (repainted by a demand-armed one-second tick that stops with the
// last run), the pane's row carries ⟳, and complexity.cancelAgent or esc on
// the row cancels it. One run per function at a time: `a` again while it
// runs only notifies. A missing `claude` reuses agent.ask's install message,
// a failed run notifies with the runner's error. Every notification after a
// run and the scratch header name the model and the session log. The
// sessions the run leaves behind are excluded from trace discovery like an
// ask fork.
//
// On the Duplicates tab (#3045, Roadmap 0560) `a` on a clone group whose
// occurrences all lie in one file runs the same path with the extract
// prompt (agentask.ExtractPrompt: every occurrence with its line range and
// the whole file) asking for one diff that introduces a shared helper and
// replaces every occurrence; the apply, the scratch fallback, cancel and
// the notifications are the simplify run's, minus the re-measure.
//
// A group spanning several files (#3046) carries every occurrence with its
// file and every file in the prompt (agentask.ExtractFilesInstruction) and
// its diff may touch only the group's files: a section for any other file
// refuses the apply. The apply is all-or-nothing — every file's hunks are
// checked before anything opens and every new text computed before any
// buffer changes — and leaves one undoable, unsaved edit per changed buffer,
// the request's own file in front.

// ComplexityAskAgentMsg runs complexity.askAgent (#2964), a in the pane.
type ComplexityAskAgentMsg struct{}

// ComplexityCancelAgentMsg runs complexity.cancelAgent (#3009).
type ComplexityCancelAgentMsg struct{}

// askInstallHint is what a missing `claude` says, shared with agent.ask.
const askInstallHint = "Install Claude Code and make sure `claude` is on PATH, then ask again."

// cxAgentRunner runs one claude argv; tests swap it for a fake.
var cxAgentRunner agentask.Runner = agentask.Run

// cxAgentTick is the repaint interval of the status segment's clock.
const cxAgentTick = time.Second

// complexityAgentState tracks the runs in flight, shared by pointer like
// cxDiff.
type complexityAgentState struct {
	running map[string]*cxAgentRun
	// seq numbers the runs, so the result of a cancelled run never frees
	// the slot of the fresh run started after it.
	seq int
	// tickArmed is set while the segment's repaint tick is pending.
	tickArmed bool
}

// cxAgentRun is one run in flight.
type cxAgentRun struct {
	id      int
	cancel  context.CancelFunc
	req     complexitypanel.AskAgentMsg
	model   string
	started time.Time
}

// cxAgentDoneMsg is a finished (or cancelled) run.
type cxAgentDoneMsg struct {
	key string
	id  int
	req complexitypanel.AskAgentMsg
	res agentask.SimplifyResult
	err error
	dir string // the run's working directory (the session log's project)
}

// cxAgentTickMsg repaints the status segment while runs are in flight.
type cxAgentTickMsg struct{ gen int64 }

// cxAgentAppliedMsg is the off-loop re-measure after an applied diff.
type cxAgentAppliedMsg struct {
	req   complexitypanel.AskAgentMsg
	after *complexity.Function // nil when the function is not found again
	model string
	log   string
	// files are the changed files, project-relative (#3046).
	files []string
}

// complexityAskAgent is complexity.askAgent from the palette: the pane's
// 'a' for the selected row.
func (m *Model) complexityAskAgent() tea.Cmd {
	if p := m.complexityPanel(); p != nil {
		if req, ok := p.AskAgent(); ok {
			return m.startComplexityAgent(req)
		}
		if why := p.DuplicateAgentRefusal(); why != "" {
			m.host.Notify(host.Info, "complexity: "+why) // #3045
			return nil
		}
	}
	m.host.Notify(host.Info, "complexity: select a function row to ask the agent about it")
	return nil
}

// startComplexityAgent builds the prompt and launches the run.
func (m *Model) startComplexityAgent(req complexitypanel.AskAgentMsg) tea.Cmd {
	if m.cxAgent == nil {
		m.cxAgent = &complexityAgentState{running: map[string]*cxAgentRun{}}
	}
	key := req.Key()
	if _, busy := m.cxAgent.running[key]; busy {
		m.host.Notify(host.Info, "complexity: the agent is still working on "+req.Name+" — "+cancelAgentHint)
		return nil
	}
	prompt, err := m.complexityAgentPrompt(req)
	if err != nil {
		m.host.Notify(host.Error, "complexity: "+err.Error())
		return nil
	}
	opts := askOptions()
	dir := projectRoot()
	ctx, cancel := context.WithCancel(context.Background())
	m.cxAgent.seq++
	id := m.cxAgent.seq
	m.cxAgent.running[key] = &cxAgentRun{id: id, cancel: cancel, req: req, model: opts.Model, started: m.clock()}
	m.syncComplexityAgentMarks()
	m.host.Notify(host.Info, "complexity: asking "+opts.Model+" to "+req.Task()+"…")
	run := cxAgentRunner
	return tea.Batch(func() tea.Msg {
		res, err := agentask.RunSimplify(ctx, run, dir, opts, prompt)
		return cxAgentDoneMsg{key: key, id: id, req: req, res: res, err: err, dir: dir}
	}, m.armComplexityAgentTick())
}

// cancelAgentHint names the ways to cancel a run.
const cancelAgentHint = "esc on its row or \"Complexity: Cancel Agent\" cancels it"

// complexityCancelAgent is complexity.cancelAgent: the selected row's run
// when it has one, else every run in flight.
func (m *Model) complexityCancelAgent() {
	if m.cxAgent == nil || len(m.cxAgent.running) == 0 {
		m.host.Notify(host.Info, "complexity: the agent is not running")
		return
	}
	if p := m.complexityPanel(); p != nil {
		if req, ok := p.AskAgent(); ok {
			if _, running := m.cxAgent.running[req.Key()]; running {
				m.cancelComplexityAgent(req.Key())
				return
			}
		}
	}
	for _, key := range m.complexityAgentKeys() {
		m.cancelComplexityAgent(key)
	}
}

// cancelComplexityAgent stops the run in slot key and frees the slot at
// once, so `a` on the row starts a fresh run; the cancelled run's result
// is dropped when it arrives.
func (m *Model) cancelComplexityAgent(key string) {
	if m.cxAgent == nil {
		return
	}
	run, ok := m.cxAgent.running[key]
	if !ok {
		return
	}
	run.cancel()
	delete(m.cxAgent.running, key)
	m.syncComplexityAgentMarks()
	m.recordComplexityAgent("cancelled", run, false)
	m.host.Notify(host.Info, "complexity: cancelled the agent for "+run.req.Name)
}

// complexityAgentKeys lists the running slots, oldest first.
func (m Model) complexityAgentKeys() []string {
	if m.cxAgent == nil {
		return nil
	}
	keys := make([]string, 0, len(m.cxAgent.running))
	for k := range m.cxAgent.running {
		keys = append(keys, k)
	}
	runs := m.cxAgent.running
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && runs[keys[j]].id < runs[keys[j-1]].id; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// syncComplexityAgentMarks hands the pane the running slots for its ⟳.
func (m *Model) syncComplexityAgentMarks() {
	p := m.complexityPanel()
	if p == nil {
		return
	}
	var keys map[string]bool
	if m.cxAgent != nil && len(m.cxAgent.running) > 0 {
		keys = make(map[string]bool, len(m.cxAgent.running))
		for k := range m.cxAgent.running {
			keys[k] = true
		}
	}
	p.SetAgentRunning(keys)
}

// armComplexityAgentTick arms the segment's repaint tick unless it is
// pending already (performance.md idle rules: demand-armed, re-armed only
// while a run is in flight, stamped with the model generation).
func (m *Model) armComplexityAgentTick() tea.Cmd {
	if m.cxAgent == nil || m.cxAgent.tickArmed {
		return nil
	}
	m.cxAgent.tickArmed = true
	gen := m.modelGen
	return tickCmd(cxAgentTick, func(time.Time) tea.Msg { return cxAgentTickMsg{gen: gen} })
}

// handleComplexityAgentTick repaints (the wake itself redraws the segment)
// and re-arms while a run is in flight; it also re-syncs the pane's marks,
// so a window opened during a run picks them up.
func (m *Model) handleComplexityAgentTick(msg cxAgentTickMsg) tea.Cmd {
	if !m.ownsTick(msg.gen) || m.cxAgent == nil {
		return nil // a departed model's tick (#2194)
	}
	m.cxAgent.tickArmed = false
	m.syncComplexityAgentMarks()
	if len(m.cxAgent.running) == 0 {
		return nil
	}
	return m.armComplexityAgentTick()
}

// complexityAgentSegment is the status line's run indicator:
// "[agent: simplify render 0:42]", the oldest run named and the others
// counted; "" while nothing runs.
func (m Model) complexityAgentSegment() string {
	keys := m.complexityAgentKeys()
	if len(keys) == 0 {
		return ""
	}
	run := m.cxAgent.running[keys[0]]
	s := "[agent: " + run.req.Task() + " " + agentElapsed(m.clock().Sub(run.started))
	if len(keys) > 1 {
		s += " +" + strconv.Itoa(len(keys)-1)
	}
	return s + "]"
}

// agentElapsed spells a run's duration as m:ss.
func agentElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d / time.Second)
	return strconv.Itoa(secs/60) + ":" + twoDigits(secs%60)
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
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
	text, err := m.complexitySourceText(req.Path)
	if err != nil {
		return "", err
	}
	return sourceLines(req.Path, strings.Split(text, "\n"), req.Line, req.EndLine)
}

// complexitySourceText is path's text for a prompt: the open buffer when
// the file is open, else the disk.
func (m Model) complexitySourceText(path string) (string, error) {
	if ed := m.editorForPath(path); ed != nil && ed.HasFile() {
		return ed.Text(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// complexityAgentPrompt builds the run's prompt: the simplify prompt of a
// function row, the extract prompt of a clone group (#3045) — every
// occurrence's lines and the whole file, each file read once; a group
// spanning several files (#3046) names each occurrence's file and carries
// every file.
func (m Model) complexityAgentPrompt(req complexitypanel.AskAgentMsg) (string, error) {
	if req.Dup == nil {
		source, err := m.complexityFunctionSource(req)
		if err != nil {
			return "", err
		}
		return agentask.SimplifyPrompt(complexitySimplify(req, displayPath(req.Path), source)), nil
	}
	paths := req.Paths()
	e := agentask.Extract{Path: displayPath(req.Path), Lang: req.Lang, Tokens: req.Tokens}
	lines := make(map[string][]string, len(paths))
	for _, p := range paths {
		text, err := m.complexitySourceText(p)
		if err != nil {
			return "", err
		}
		lines[p] = strings.Split(text, "\n")
		e.Files = append(e.Files, agentask.ExtractFile{Path: displayPath(p), Text: text})
	}
	for _, o := range req.Dup {
		src, err := sourceLines(o.Path, lines[o.Path], o.Line, o.EndLine)
		if err != nil {
			return "", err
		}
		e.Occurrences = append(e.Occurrences, agentask.ExtractOccurrence{Path: displayPath(o.Path), Line: o.Line, EndLine: o.EndLine, Source: src})
	}
	if len(e.Files) == 1 {
		e.File, e.Files = e.Files[0].Text, nil // the same-file prompt (#3045)
	}
	return agentask.ExtractPrompt(e), nil
}

// sourceLines joins lines from..to of path's text, clamping the end; a
// start past the text asks for a rescan.
func sourceLines(path string, lines []string, from, to int) (string, error) {
	if from < 0 || from >= len(lines) {
		return "", errors.New(filepath.Base(path) + " no longer holds line " + strconv.Itoa(from+1) + "; rescan the project")
	}
	if to < from {
		to = from
	}
	if to >= len(lines) {
		to = len(lines) - 1
	}
	return strings.Join(lines[from:to+1], "\n"), nil
}

// handleComplexityAgentDone shows the answer — applied, or as a scratch —
// or the failure of a run.
func (m Model) handleComplexityAgentDone(msg cxAgentDoneMsg) (tea.Model, tea.Cmd) {
	for _, id := range msg.res.Sessions {
		// The fresh session is not the traced one: discovery skips it.
		m.noteAskFork(id)
	}
	var run *cxAgentRun
	if m.cxAgent != nil {
		if r, ok := m.cxAgent.running[msg.key]; ok && r.id == msg.id {
			run = r
		}
	}
	if run == nil {
		// Cancelled: the cancel freed the slot and said so already.
		return m, nil
	}
	run.cancel()
	delete(m.cxAgent.running, msg.key)
	m.syncComplexityAgentMarks()
	log := cxSessionLog(msg.dir, msg.res.ForkID)
	if msg.err != nil {
		m.recordComplexityAgent("failed", run, msg.res.Retried)
		switch {
		case errors.Is(msg.err, agentask.ErrNotInstalled):
			m.host.Notify(host.Error, "complexity: "+msg.err.Error()+" "+askInstallHint)
		default:
			m.host.Notify(host.Error, "complexity: the agent failed for "+msg.req.Name+": "+msg.err.Error()+cxRunFooter(run.model, log))
		}
		return m, nil
	}
	sc := cxAgentScratch{req: msg.req, answer: msg.res.Answer, model: run.model, log: log}
	switch {
	case !msg.res.HasDiff:
		m.recordComplexityAgent("no_diff", run, msg.res.Retried)
		sc.note = "The agent answered without a unified diff"
		if msg.res.Retried {
			sc.note += ", also after the follow-up asking for one"
		}
		sc.note += "; nothing was applied."
		return m.openComplexityAgentScratch(sc, host.Warn, "the agent answered without a diff — opened as ")
	case !complexityAgentApply():
		m.recordComplexityAgent("scratch", run, msg.res.Retried)
		return m.openComplexityAgentScratch(sc, host.Info, "the agent's answer for "+msg.req.Name+" opened as ")
	}
	return m.applyComplexityAgentDiff(msg, run, sc)
}

// complexityAgentApply reads complexity.agent_apply (on without a config).
func complexityAgentApply() bool {
	c := config.Get()
	return c == nil || c.Complexity.AgentApply
}

// applyComplexityAgentDiff applies the answer's diff to the buffers of the
// files it changes — the function's file, or the clone group's files
// (#3046): every hunk's before lines must occur exactly once, each buffer's
// result is one undoable edit through the local-history restore path. The
// apply is all-or-nothing: every file is checked before anything opens and
// every new text computed before any buffer changes, so a miss in any file
// applies nothing and opens the scratch in front.
func (m Model) applyComplexityAgentDiff(msg cxAgentDoneMsg, run *cxAgentRun, sc cxAgentScratch) (tea.Model, tea.Cmd) {
	edits, why := cxAgentFileHunks(msg.req.Paths(), msg.res.Diff)
	if why != "" {
		m.recordComplexityAgent("not_applied", run, msg.res.Retried)
		sc.note = "The agent's diff " + why + "; nothing was applied."
		return m.openComplexityAgentScratch(sc, host.Warn, "the agent's diff "+why+" — opened as ")
	}
	multi := len(msg.req.Paths()) > 1
	// Check against the buffers (or the disk when a file is not open)
	// before anything opens: a miss leaves the editor as it is.
	for _, e := range edits {
		now, err := m.complexityFileText(e.path)
		if err == nil {
			_, err = agenttrace.Apply(e.hunks, now)
		}
		if err != nil {
			return m.complexityAgentNotApplied(sc, run, msg.res, cxMissFile(e.path, multi), err)
		}
	}
	// The explanation opens first and the changed files after it, the
	// request's own file last, so it is the tab in front and the scratch
	// waits behind them. The edits come last: a tab switch after one would
	// hand the dirty buffer to editor.auto_save = "focus", and the apply
	// never saves.
	var cmds []tea.Cmd
	sc.note = cxAppliedNote(edits)
	if scratchPath, err := scratch.CreateWithContent("md", []byte(sc.body())); err == nil {
		tm, cmd := m.openPathAt(scratchPath, 0, 0)
		m = asAppModel(tm, m)
		cmds = append(cmds, cmd)
	}
	for _, e := range cxOpenOrder(edits, msg.req.Path) {
		tm, cmd := m.openPathAt(e.path, cxOpenLine(msg.req, e.path), 0)
		m = asAppModel(tm, m)
		cmds = append(cmds, cmd)
	}
	texts := make([]string, len(edits))
	for i, e := range edits {
		var err error
		if ed := m.editorForPath(e.path); ed == nil {
			err = errors.New("could not open " + baseName(e.path))
		} else {
			texts[i], err = agenttrace.Apply(e.hunks, ed.Text())
		}
		if err != nil {
			tm, cmd := m.complexityAgentNotApplied(sc, run, msg.res, cxMissFile(e.path, multi), err)
			return tm, tea.Batch(append(cmds, cmd)...)
		}
	}
	m.recordComplexityAgent("applied", run, msg.res.Retried)
	files := make([]string, len(edits))
	for i, e := range edits {
		restoreCmd, _ := m.applyBufferRestore(e.path, texts[i])
		cmds = append(cmds, restoreCmd)
		files[i] = displayPath(e.path)
	}
	req, model, log := msg.req, run.model, sc.log
	path, now := edits[0].path, texts[0]
	cmds = append(cmds, func() tea.Msg {
		if req.Dup != nil {
			return cxAgentAppliedMsg{req: req, model: model, log: log, files: files} // no function to re-measure (#3045)
		}
		return cxAgentAppliedMsg{req: req, after: cxFindMeasured(cxMeasureText(path, now), req), model: model, log: log, files: files}
	})
	return m, tea.Batch(cmds...)
}

// cxMissFile names the file of a miss in a multi-file apply; "" otherwise.
func cxMissFile(path string, multi bool) string {
	if !multi {
		return ""
	}
	return displayPath(path)
}

// cxAppliedNote is the scratch's note on an applied diff.
func cxAppliedNote(edits []cxFileHunks) string {
	if len(edits) == 1 {
		return "Applied to the buffer of " + displayPath(edits[0].path) + " as one edit — u reverts it, save writes it."
	}
	names := make([]string, len(edits))
	for i, e := range edits {
		names[i] = displayPath(e.path)
	}
	return "Applied to the buffers of " + strings.Join(names, ", ") + " as one edit each — u in each buffer reverts it, save writes it."
}

// cxOpenOrder is the order the changed files open in: the request's own
// file last, so it ends in front; else the diff's first file.
func cxOpenOrder(edits []cxFileHunks, front string) []cxFileHunks {
	last := 0
	for i, e := range edits {
		if e.path == front {
			last = i
		}
	}
	out := make([]cxFileHunks, 0, len(edits))
	for i, e := range edits {
		if i != last {
			out = append(out, e)
		}
	}
	return append(out, edits[last])
}

// cxOpenLine is the line a changed file opens at: the request's line in
// its own file, a clone group's first occurrence in another one.
func cxOpenLine(req complexitypanel.AskAgentMsg, path string) int {
	if path == req.Path {
		return req.Line
	}
	for _, o := range req.Dup {
		if o.Path == path {
			return o.Line
		}
	}
	return 0
}

// complexityFileText is path's content now: its open buffer, else the disk.
func (m Model) complexityFileText(path string) (string, error) {
	if ed := m.editorForPath(path); ed != nil {
		return ed.Text(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// complexityAgentNotApplied reports a diff that did not apply — the hunk
// that missed, when one did, and in a multi-file apply (#3046) its file —
// and opens the scratch in front. A diff that leaves lines out (#3050,
// res.Diff.Elided, also after the follow-up asking for the complete one)
// says so in the scratch's note: that, not an edited file, is why it misses.
func (m Model) complexityAgentNotApplied(sc cxAgentScratch, run *cxAgentRun, res agentask.SimplifyResult, file string, err error) (tea.Model, tea.Cmd) {
	m.recordComplexityAgent("not_applied", run, res.Retried)
	what := err.Error()
	var he *agenttrace.HunkError
	if errors.As(err, &he) {
		what = "hunk " + strconv.Itoa(he.Index+1) + " of " + strconv.Itoa(he.Total)
	}
	buffer := "the buffer"
	if file != "" {
		what += " in " + file
		buffer = "the buffer of " + file
	}
	sc.note = "The agent's diff does not apply cleanly to " + buffer + " (" + err.Error() + "); nothing was applied."
	if res.Diff.Elided != "" {
		sc.note += " The diff leaves lines out — " + res.Diff.Elided
		if res.Retried {
			sc.note += ", also after the follow-up asking for the complete diff"
		}
		sc.note += " — so it cannot match the file."
		what += ", the diff leaves lines out"
	}
	sc.note += " Save it as a patch and try `git apply --3way`."
	return m.openComplexityAgentScratch(sc, host.Warn, "the agent's diff does not apply cleanly ("+what+") — opened as ")
}

// asAppModel unwraps an open's result, keeping fallback on a foreign type.
func asAppModel(tm tea.Model, fallback Model) Model {
	if mm, ok := tm.(Model); ok {
		return mm
	}
	return fallback
}

// handleComplexityAgentApplied reports an applied diff with the function's
// metrics before → after and the excess delta under its thresholds.
func (m *Model) handleComplexityAgentApplied(msg cxAgentAppliedMsg) {
	text := "complexity: applied the agent's diff to " + msg.req.Name
	revert := "; u reverts, save writes it"
	switch fn := msg.after; {
	case msg.req.Dup != nil && len(msg.files) > 1:
		// #3046: one edit in each changed file.
		text = "complexity: applied the agent's diff extracting the " + msg.req.Name + " into a shared helper — changed " + strings.Join(msg.files, ", ")
		revert = "; u in each buffer reverts, save writes it"
	case msg.req.Dup != nil:
		// #3045: a clone group has no function to re-measure.
		text = "complexity: applied the agent's diff extracting the " + msg.req.Name + " of " + displayPath(msg.req.Path) + " into a shared helper"
	case fn != nil:
		th := complexity.Thresholds{}
		if c := config.Get(); c != nil {
			th = m.complexityThresholdsFor(c.Complexity, msg.req.Path)
		}
		text += " (" + excessDelta(th.Excess(*fn)-th.Excess(msg.req.Fn)) + " excess) — CCN " +
			strconv.Itoa(msg.req.CCN) + " → " + strconv.Itoa(fn.CCN) + ", cognitive " +
			strconv.Itoa(msg.req.Cognitive) + " → " + strconv.Itoa(fn.Cognitive)
	default:
		text += " — the function is not found under its name any more, so nothing was re-measured"
	}
	text += revert + cxRunFooter(msg.model, msg.log)
	m.host.Notify(host.Info, text)
}

// excessDelta spells an excess change: "−3", "+2", "±0".
func excessDelta(d int) string {
	switch {
	case d < 0:
		return "−" + strconv.Itoa(-d)
	case d > 0:
		return "+" + strconv.Itoa(d)
	}
	return "±0"
}

// cxFindMeasured is the re-measured function: the same name, nearest to
// the old header line.
func cxFindMeasured(fns []complexity.Function, req complexitypanel.AskAgentMsg) *complexity.Function {
	var best *complexity.Function
	for i := range fns {
		f := &fns[i]
		if f.Name != req.Name {
			continue
		}
		if best == nil || abs(f.Line-req.Line) < abs(best.Line-req.Line) {
			best = f
		}
	}
	return best
}

// cxFileHunks is one file's share of an answer diff.
type cxFileHunks struct {
	path  string
	hunks []agenttrace.DiffHunk
}

// cxAgentFileHunks sorts the diff's hunks onto paths — the files the run
// may change: the function's file, or a clone group's files (#3046) — in
// the order the diff first names them. A section belongs to the path it
// names, or whose suffix it names; a section naming no file belongs to the
// only path. A section for any other file refuses the apply, as does one
// naming no file or matching several while there are several paths.
func cxAgentFileHunks(paths []string, d agentask.AnswerDiff) ([]cxFileHunks, string) {
	var out []cxFileHunks
	n := 0
	for _, f := range d.Files {
		if len(f.Hunks) == 0 {
			continue
		}
		p := strings.TrimPrefix(filepath.ToSlash(f.Path), "./")
		var match []string
		for _, path := range paths {
			slash := filepath.ToSlash(path)
			if p == "" || p == slash || strings.HasSuffix(slash, "/"+p) {
				match = append(match, path)
			}
		}
		switch {
		case len(match) == 0 && len(paths) > 1:
			return nil, "also changes " + f.Path + ", which holds no occurrence of the clone group"
		case len(match) == 0:
			return nil, "also changes " + f.Path
		case len(match) > 1 && p == "":
			return nil, "has a section naming no file"
		case len(match) > 1:
			return nil, "names " + f.Path + ", which matches several of the clone group's files"
		}
		n += len(f.Hunks)
		i := slices.IndexFunc(out, func(e cxFileHunks) bool { return e.path == match[0] })
		if i < 0 {
			out = append(out, cxFileHunks{path: match[0]})
			i = len(out) - 1
		}
		out[i].hunks = append(out[i].hunks, f.Hunks...)
	}
	if n == 0 {
		return nil, "holds no hunk"
	}
	return out, ""
}

// openComplexityAgentScratch writes the scratch, notifies prefix + its
// path (with the model and session log), and opens it in front.
func (m Model) openComplexityAgentScratch(sc cxAgentScratch, sev host.Severity, prefix string) (tea.Model, tea.Cmd) {
	path, err := scratch.CreateWithContent("md", []byte(sc.body()))
	if err != nil {
		m.host.Notify(host.Error, "complexity: could not write the answer: "+err.Error())
		return m, nil
	}
	m.host.Notify(sev, "complexity: "+prefix+displayPath(path)+cxRunFooter(sc.model, sc.log))
	return m.openPathAt(path, 0, 0)
}

// cxRunFooter is the notification's second line: the model and the
// session log the run left behind.
func cxRunFooter(model, log string) string {
	if log == "" {
		return "\nmodel " + model
	}
	return "\nmodel " + model + " · session log " + log
}

// cxSessionLog is the transcript of session id run in dir, home shortened
// to "~"; "" without an id.
func cxSessionLog(dir, id string) string {
	root := agenttrace.ProjectsDir()
	if id == "" || root == "" {
		return ""
	}
	p := filepath.Join(root, agenttrace.EncodeCWD(dir), id+".jsonl")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return filepath.Join("~", rel)
		}
	}
	return p
}

// recordComplexityAgent is the complexity.agent telemetry op of one run.
func (m Model) recordComplexityAgent(outcome string, run *cxAgentRun, retried bool) {
	if m.usage == nil || run == nil {
		return
	}
	phase := telemetry.OpPhaseOK
	switch outcome {
	case "failed":
		phase = telemetry.OpPhaseError
	case "cancelled":
		phase = telemetry.OpPhaseCanceled
	}
	m.usage.Op(telemetry.OpComplexityAgent, phase, map[string]string{
		"outcome": outcome,
		"ms":      strconv.FormatInt(m.clock().Sub(run.started).Milliseconds(), 10),
		"retried": strconv.FormatBool(retried),
	})
}

// cxAgentScratch is the scratch file of a run: a heading naming the
// function, its metrics, the model and session log, what happened to the
// diff, then the answer with its diff fenced.
type cxAgentScratch struct {
	req    complexitypanel.AskAgentMsg
	answer string
	model  string
	log    string
	note   string
}

func (s cxAgentScratch) body() string {
	var sb strings.Builder
	if s.req.Dup != nil {
		// #3045: the clone group's occurrences instead of a function's metrics.
		sb.WriteString("# Extract " + s.req.Name + "\n\n")
		multi := len(s.req.Paths()) > 1 // #3046: each location names its file
		locs := make([]string, 0, len(s.req.Dup))
		for _, o := range s.req.Dup {
			loc := strconv.Itoa(o.Line+1) + "–" + strconv.Itoa(o.EndLine+1)
			if multi {
				loc = displayPath(o.Path) + ":" + loc
			}
			locs = append(locs, loc)
		}
		if multi {
			sb.WriteString("Occurrences: " + strings.Join(locs, ", "))
		} else {
			sb.WriteString(displayPath(s.req.Path) + ": lines " + strings.Join(locs, ", "))
		}
		sb.WriteString(" — " + strconv.Itoa(s.req.Tokens) + " tokens each\n\n")
	} else {
		sb.WriteString("# Simplify " + s.req.Name + "\n\n")
		sb.WriteString(displayPath(s.req.Path) + ":" + strconv.Itoa(s.req.Line+1) + " — CCN " + strconv.Itoa(s.req.CCN) + ", cognitive " + strconv.Itoa(s.req.Cognitive) + "\n\n")
	}
	sb.WriteString("Model: " + s.model)
	if s.log != "" {
		sb.WriteString(" · session log: " + s.log)
	}
	sb.WriteString("\n\n")
	if s.note != "" {
		sb.WriteString("> " + s.note + "\n\n")
	}
	sb.WriteString(fenceDiff(s.answer))
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
