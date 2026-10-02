package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/tracepanel"
)

// agenttrace_history.go is the app half of the session history (#2860):
// the trace keeps a compact record of every session it follows under
// .ike/agent-trace/<session-id>.json (agenttrace.Store), written from the
// live read at every turn boundary, on SessionEnd and when the trace moves
// on to another session (a /clear, a new transcript) — so a crash loses at
// most the running turn. agent.trace.import reads the transcripts Claude
// Code still has for this project into the same store (progress in the
// status bar). The pane's `s` (agent.trace.history) lists the records in a
// picker; a picked session is shown read-only in the pane — the ticks keep
// reading and saving the live one meanwhile, they just leave the pane
// alone — and esc / r return to the live session. agent.ask on a stored
// session resumes its id when the transcript still exists.

// AgentTraceHistoryMsg runs agent.trace.history: the session picker.
type AgentTraceHistoryMsg struct{}

// AgentTraceImportMsg runs agent.trace.import.
type AgentTraceImportMsg struct{}

// traceSaveState is what the last written record of the live session held;
// the next read writes again when any of it moved.
type traceSaveState struct {
	id      string
	turns   int
	rewinds int
	ended   bool
}

// traceSavedMsg reports a record write (the live read's, or the close of a
// session the trace left).
type traceSavedMsg struct{ err error }

// traceHistoryListMsg is the store listing the picker shows.
type traceHistoryListMsg struct {
	gen   int64
	items []tracepanel.HistoryItem
	err   error
}

// traceHistoryLoadedMsg is a stored session read for the pane.
type traceHistoryLoadedMsg struct {
	gen   int64
	id    string
	nodes []agenttrace.Node
	stops []agenttrace.Stop
	diffs []agenttrace.ChangeDiff
	info  tracepanel.Info
	err   error
}

// traceImportProgressMsg is one step of the import (status bar).
type traceImportProgressMsg struct{ done, total int }

// CoalesceKey makes the host's outbox keep only the latest progress.
func (traceImportProgressMsg) CoalesceKey() string { return "agenttrace.import" }

// traceImportDoneMsg is the finished import.
type traceImportDoneMsg struct {
	res agenttrace.ImportResult
	err error
}

// traceHistoryDir is the record directory: the project's .ike/agent-trace,
// IKE_CONFIG_DIR/agent-trace when the variable is set (the localHistoryDir
// convention).
func traceHistoryDir() string {
	base := ".ike"
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		base = d
	}
	return filepath.Join(base, "agent-trace")
}

// traceStore is the store with the configured cap.
func traceStore() agenttrace.Store {
	return agenttrace.Store{Dir: traceHistoryDir(), Max: config.Get().Agent.Trace.HistoryMaxSessions}
}

// traceRecordDue reports whether the live session needs a record: its turn
// count, rewinds or end moved since the last write (or it was never
// written). A session without an id cannot be filed.
func traceRecordDue(s *agenttrace.Session, ended bool, saved traceSaveState) bool {
	if s == nil || s.ID == "" || len(s.Events) == 0 {
		return false
	}
	if saved.id != s.ID {
		return true
	}
	return s.Turns() != saved.turns || len(s.Rewinds) != saved.rewinds || (ended && !saved.ended)
}

// traceCloseCmd files the session a reader followed as ended — the trace
// moves on to another transcript (a /clear started a new session). nil
// without a reader.
func (m *Model) traceCloseCmd(r *agenttrace.Reader) tea.Cmd {
	if r == nil {
		return nil
	}
	st := traceStore()
	return func() tea.Msg {
		var rec *agenttrace.Record
		r.Read(func(s *agenttrace.Session) {
			if s.ID != "" && len(s.Events) > 0 {
				rec = agenttrace.NewRecord(s, r.Path(), true)
			}
		})
		if rec == nil {
			return nil
		}
		return traceSavedMsg{err: st.Save(rec)}
	}
}

// handleTraceSaved reports a failed write once per distinct error — a full
// disk must not nag on every turn.
func (m Model) handleTraceSaved(msg traceSavedMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil {
		m.traceSaveErr = ""
		return m, nil
	}
	if text := msg.err.Error(); text != m.traceSaveErr {
		m.traceSaveErr = text
		m.host.Notify(host.Warn, "agent trace: history not written: "+text)
	}
	return m, nil
}

// openTraceHistory is agent.trace.history and the pane's 's': the pane
// opens if it was closed, and the store is listed off the loop for the
// picker.
func (m *Model) openTraceHistory() tea.Cmd {
	var open tea.Cmd
	if m.agentTracePanel() == nil {
		open = m.showPanel(pane.AgentTraceKey, m.openAgentTracePanel)
	}
	m.traceHistoryGen++
	gen := m.traceHistoryGen
	st := traceStore()
	live := m.traceLiveID()
	return tea.Batch(open, func() tea.Msg {
		all, err := st.List()
		items := make([]tracepanel.HistoryItem, 0, len(all))
		for _, s := range all {
			items = append(items, tracepanel.HistoryItem{
				ID: s.ID, StartedAt: s.StartedAt, EndedAt: s.EndedAt, Ended: s.Ended,
				Turns: s.Turns, Files: s.FilesChanged, Prompt: s.FirstPrompt, Live: s.ID == live && live != "",
			})
		}
		return traceHistoryListMsg{gen: gen, items: items, err: err}
	})
}

// traceLiveID is the id of the session the trace follows: the hook
// binding's, else the one the last live read reported.
func (m Model) traceLiveID() string {
	if m.traceSession.ID != "" {
		return m.traceSession.ID
	}
	return m.traceLiveInfo.ID
}

// handleTraceHistoryList opens the picker on the listing.
func (m Model) handleTraceHistoryList(msg traceHistoryListMsg) (tea.Model, tea.Cmd) {
	p := m.agentTracePanel()
	if msg.gen != m.traceHistoryGen || p == nil {
		return m, nil
	}
	if msg.err != nil {
		m.host.Notify(host.Error, "agent trace: history: "+msg.err.Error())
		return m, nil
	}
	p.OpenPicker(msg.items)
	return m, nil
}

// showTraceHistoryCmd loads a stored session off the loop and builds its
// tree and path.
func (m *Model) showTraceHistoryCmd(id string) tea.Cmd {
	m.traceHistoryGen++
	gen := m.traceHistoryGen
	st := traceStore()
	return func() tea.Msg {
		rec, err := st.Load(id)
		if err != nil {
			return traceHistoryLoadedMsg{gen: gen, id: id, err: err}
		}
		s := rec.Session()
		info := tracepanel.Info{
			ID: rec.ID, Transcript: rec.Transcript, CWD: rec.CWD, Ended: true, Turns: rec.Turns,
			History: tracepanel.HistoryLabel(rec.StartedAt),
		}
		return traceHistoryLoadedMsg{gen: gen, id: id, nodes: agenttrace.BuildTree(s), stops: agenttrace.BuildPath(s), diffs: s.KnownDiffs, info: info}
	}
}

// handleTraceHistoryLoaded shows the stored session in the pane.
func (m Model) handleTraceHistoryLoaded(msg traceHistoryLoadedMsg) (tea.Model, tea.Cmd) {
	p := m.agentTracePanel()
	if msg.gen != m.traceHistoryGen || p == nil {
		return m, nil
	}
	if msg.err != nil {
		m.host.Notify(host.Error, "agent trace: stored session "+shortID(msg.id)+": "+msg.err.Error())
		return m, nil
	}
	m.traceHistoryID = msg.id
	m.traceHistoryDiffs = msg.diffs
	p.SetStored(msg.nodes, msg.stops, msg.info)
	// The change-feed links are the live session's; a stored one has none.
	p.SetLinks(agenttrace.Links{})
	return m, nil
}

// backToLiveTrace leaves a stored session: the pane forgets it and the
// live session is located and read again.
func (m *Model) backToLiveTrace() tea.Cmd {
	m.traceHistoryID = ""
	m.traceHistoryDiffs = nil
	if p := m.agentTracePanel(); p != nil {
		p.ClosePicker()
		p.Reset()
	}
	return m.traceRelocateCmd()
}

// traceShowingHistory reports whether the pane shows a stored session, so
// a live read or a relocation must leave it alone.
func (m Model) traceShowingHistory() bool { return m.traceHistoryID != "" }

// importTraceHistory is agent.trace.import: the project's transcripts are
// read off the loop into the store; progress goes through the host's
// outbox into the status bar.
func (m *Model) importTraceHistory() tea.Cmd {
	if m.traceImporting {
		m.host.Notify(host.Info, "agent trace: an import is already running")
		return nil
	}
	m.traceImporting = true
	m.traceImportDone, m.traceImportTotal = 0, 0
	st := traceStore()
	projects := agenttrace.ProjectsDir()
	cwd := projectRoot()
	h := m.host
	return func() tea.Msg {
		res, err := agenttrace.Import(st, projects, cwd, func(done, total int) {
			h.Send(traceImportProgressMsg{done: done, total: total})
		})
		return traceImportDoneMsg{res: res, err: err}
	}
}

// handleTraceImportProgress updates the status bar segment.
func (m Model) handleTraceImportProgress(msg traceImportProgressMsg) (tea.Model, tea.Cmd) {
	m.traceImportDone, m.traceImportTotal = msg.done, msg.total
	return m, nil
}

// handleTraceImportDone reports the import and refreshes an open picker.
func (m Model) handleTraceImportDone(msg traceImportDoneMsg) (tea.Model, tea.Cmd) {
	m.traceImporting = false
	m.traceImportDone, m.traceImportTotal = 0, 0
	if msg.err != nil {
		m.host.Notify(host.Error, "agent trace: import failed: "+msg.err.Error())
		return m, nil
	}
	m.host.Notify(importSeverity(msg.res), traceImportSummary(msg.res))
	if p := m.agentTracePanel(); p != nil && p.PickerOpen() {
		return m, m.openTraceHistory()
	}
	return m, nil
}

func importSeverity(res agenttrace.ImportResult) host.Severity {
	if len(res.Failed) > 0 {
		return host.Warn
	}
	return host.Info
}

// traceImportSummary is the notice: counts, then the unreadable files.
func traceImportSummary(res agenttrace.ImportResult) string {
	parts := []string{strconv.Itoa(res.Imported) + " imported"}
	if res.Unchanged > 0 {
		parts = append(parts, strconv.Itoa(res.Unchanged)+" unchanged")
	}
	if res.Forks > 0 {
		parts = append(parts, strconv.Itoa(res.Forks)+" forks skipped")
	}
	text := "agent trace: " + strings.Join(parts, ", ")
	if len(res.Failed) > 0 {
		text += " · unreadable: " + strings.Join(res.Failed, "; ")
	}
	return text
}

// traceImportSegment is the status bar's import progress (#2860), "" when
// no import runs.
func (m Model) traceImportSegment() string {
	if !m.traceImporting {
		return ""
	}
	if m.traceImportTotal == 0 {
		return "⇣ agent sessions …"
	}
	return "⇣ agent sessions " + strconv.Itoa(m.traceImportDone) + "/" + strconv.Itoa(m.traceImportTotal)
}

// traceDirChanged reports whether a transcript appeared in (or vanished
// from) the project directory of cwd since the last check (#2860): without
// hooks that is how a /clear shows — the next relocation then follows the
// new session. The first check only records the stamp.
func (m *Model) traceDirChanged(cwd string) bool {
	stamp := agenttrace.DirStamp(agenttrace.ProjectsDir(), cwd)
	changed := !m.traceDirStamp.IsZero() && !stamp.Equal(m.traceDirStamp)
	m.traceDirStamp = stamp
	return changed
}

// traceAskStored gates agent.ask on a stored session (#2860): the fork
// needs the transcript, which Claude Code prunes eventually. It returns the
// session to fork and "" — or the explanation when it cannot.
func (m Model) traceAskStored(info tracepanel.Info) (agentSession, string) {
	if info.Transcript == "" {
		return agentSession{}, "agent ask: this stored session has no transcript path; only the record remains"
	}
	if _, err := os.Stat(info.Transcript); err != nil {
		return agentSession{}, "agent ask: the transcript of this stored session is gone (" + displayPath(info.Transcript) + "); Claude Code pruned it, only the record remains"
	}
	cwd := info.CWD
	if cwd == "" {
		cwd = projectRoot()
	}
	return agentSession{ID: info.ID, Transcript: info.Transcript, CWD: cwd, Ended: true, At: time.Now()}, ""
}
