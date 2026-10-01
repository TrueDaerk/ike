package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
	"ike/internal/deeplink"
	"ike/internal/tracepanel"
)

// agenttrace_live_test.go is the end-to-end half of #2857: the Agent Trace
// pane open on a transcript a fake agent keeps appending to, driven by the
// real tick → read → re-arm chain (no key presses), with the editor or the
// tool terminal holding the keyboard, and a hook push that must read at
// once even when it carries another IKE process's pid.

// traceLoop is a minimal runtime for the trace's message chain: every
// command runs on its own goroutine like under tea.Program (ticks really
// wait), batches fan out, and the trace's messages come back on one channel
// for the test to feed into Update.
type traceLoop struct {
	ch    chan tea.Msg
	ticks int // tick messages fed into Update so far
}

func newTraceLoop() *traceLoop { return &traceLoop{ch: make(chan tea.Msg, 256)} }

func (l *traceLoop) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() { l.deliver(cmd()) }()
}

func (l *traceLoop) deliver(msg tea.Msg) {
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			l.run(c)
		}
	case traceTickMsg, traceReadMsg, traceLocatedMsg:
		l.ch <- msg
	}
}

// send feeds msg into m and runs what it returns.
func (l *traceLoop) send(m Model, msg tea.Msg) Model {
	if _, ok := msg.(traceTickMsg); ok {
		l.ticks++
	}
	out, cmd := m.Update(msg)
	l.run(cmd)
	return out.(Model)
}

// pump feeds the loop's messages into m until until holds, failing after d.
// step, when set, runs every 100 ms — the fake agent writing.
func (l *traceLoop) pump(t *testing.T, m Model, d time.Duration, step func(), until func(Model) bool) Model {
	t.Helper()
	deadline := time.After(d)
	writer := time.NewTicker(100 * time.Millisecond)
	defer writer.Stop()
	for !until(m) {
		select {
		case msg := <-l.ch:
			m = l.send(m, msg)
		case <-writer.C:
			if step != nil {
				step()
			}
		case <-deadline:
			p := m.agentTracePanel()
			t.Fatalf("timed out after %v; rows = %v\n%s", d, p.Rows(), p.View())
		}
	}
	return m
}

// liveTrace opens a tool pane (the agent's terminal) and a transcript of
// one turn for its cwd, then opens the trace pane and pumps until it shows
// that turn. It returns the model, the loop and the transcript.
func liveTrace(t *testing.T) (Model, *traceLoop, string) {
	t.Helper()
	m, _ := openWatcher(t)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	cwd := m.toolPane("watcher").Terminal().Cwd()
	dir := filepath.Join(cfg, "projects", agenttrace.EncodeCWD(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sess-live.jsonl")
	appendTraceTurn(t, path, cwd, 1)

	l := newTraceLoop()
	m = l.send(m, AgentTraceToggleMsg{})
	m = l.pump(t, m, 5*time.Second, nil, func(m Model) bool { return m.agentTracePanel().Info().Turns == 1 })
	return m, l, path
}

func appendTraceTurn(t *testing.T, path, cwd string, turn int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Error(err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(transcriptLines("sess-live", cwd, "/tmp/x.go", turn)); err != nil {
		t.Error(err)
	}
}

// fakeAgent returns a writer step appending turn from, from+1, … up to last, one
// per call.
func fakeAgent(t *testing.T, path, cwd string, from, last int) func() {
	next := from
	return func() {
		if next <= last {
			appendTraceTurn(t, path, cwd, next)
			next++
		}
	}
}

// TestAgentTraceFollowsGrowingTranscriptWithEditorFocused: with the editor
// holding the keyboard and the agent appending a turn every 100 ms, the
// tree grows on the ticks alone, within two of them; the cursor rides on
// the newest row and that row is on screen — the pane used to grow below
// the fold with the cursor parked on the oldest turn, which looked frozen.
func TestAgentTraceFollowsGrowingTranscriptWithEditorFocused(t *testing.T) {
	m, l, path := liveTrace(t)
	cwd := m.toolPane("watcher").Terminal().Cwd()
	m.setFocus(m.activeEditorKey())
	if m.activeWS().Panes.FocusedInstance().ActiveTerminal() != nil {
		t.Fatal("the editor must hold the keyboard")
	}

	agent := fakeAgent(t, path, cwd, 2, 9)
	start := l.ticks
	m = l.pump(t, m, 5*time.Second, agent, func(m Model) bool {
		return m.agentTracePanel().Info().Turns >= 2
	})
	if n := l.ticks - start; n > 2 {
		t.Fatalf("the first appended turn took %d ticks", n)
	}
	m = l.pump(t, m, 5*time.Second, agent, func(m Model) bool {
		return m.agentTracePanel().Info().Turns == 9
	})
	m = l.pump(t, m, 3*time.Second, nil, func(m Model) bool {
		return strings.Contains(m.agentTracePanel().View(), "#9 Turn 9 prompt")
	})
	p := m.agentTracePanel()
	rows := p.Rows()
	if cur := p.Current(); cur == nil || cur.Key != strings.TrimSpace(rows[len(rows)-1]) || !p.Following() {
		t.Fatalf("cursor %+v not on the newest row %q", cur, rows[len(rows)-1])
	}
	// The header says the pane reads, and whom it follows (widened: the
	// test layout's pane clips it).
	p.SetSize(200, 20)
	view := p.View()
	if !strings.Contains(view, "· read ") || !strings.Contains(view, "⇢ watcher (last focused)") {
		t.Fatalf("header diagnostics missing:\n%s", view)
	}
}

// TestAgentTraceGrowsKeepingSelectionWithTerminalFocused: the user moved
// the cursor onto an older row and folded a turn, the tool terminal holds
// the keyboard; appended turns still arrive without a key press and leave
// the selection and the fold alone.
func TestAgentTraceGrowsKeepingSelectionWithTerminalFocused(t *testing.T) {
	m, l, path := liveTrace(t)
	cwd := m.toolPane("watcher").Terminal().Cwd()
	appendTraceTurn(t, path, cwd, 2)
	m = l.pump(t, m, 5*time.Second, nil, func(m Model) bool { return m.agentTracePanel().Info().Turns == 2 })
	p := m.agentTracePanel()
	traceToTop(p)
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // e1, the decision
	p.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // fold it
	if cur := p.Current(); cur == nil || cur.Key != "e1" || p.Following() {
		t.Fatalf("cursor on %+v, following=%v", cur, p.Following())
	}
	m.setFocus(m.toolPane("watcher").Key())

	m = l.pump(t, m, 5*time.Second, fakeAgent(t, path, cwd, 3, 3), func(m Model) bool {
		return m.agentTracePanel().Info().Turns == 3
	})
	p = m.agentTracePanel()
	if cur := p.Current(); cur == nil || cur.Key != "e1" {
		t.Fatalf("selection lost: %+v", cur)
	}
	rows := strings.Join(p.Rows(), ",")
	if !strings.HasPrefix(rows, "t1, e1,t2,") {
		t.Fatalf("the fold of e1 was not kept: %v", rows)
	}
	if !strings.Contains(rows, "t3, e7,  e8,   e8/f0") {
		t.Fatalf("the new turn did not arrive expanded: %v", rows)
	}
	p.SetSize(200, 20)
	if view := p.View(); !strings.Contains(view, "⇢ watcher (focused)") {
		t.Fatalf("follow reason not shown:\n%s", view)
	}
}

// TestAgentTraceHookPushReadsAtOnce: a UserPromptSubmit push for the
// followed session triggers a read right away — the tree shows the new turn
// before the next tick. The push carries a stale ike_pid (the terminal was
// started under an earlier IKE process), so it binds by cwd.
func TestAgentTraceHookPushReadsAtOnce(t *testing.T) {
	m, l, path := liveTrace(t)
	cwd := m.toolPane("watcher").Terminal().Cwd()
	key := m.toolPane("watcher").Terminal().SessionKey()
	appendTraceTurn(t, path, cwd, 2)

	ev := deeplink.Event{
		SessionID: "sess-live", CWD: cwd, TranscriptPath: path, Event: "UserPromptSubmit",
		Pane: key, PID: os.Getpid() + 100000,
	}
	start := l.ticks
	m = l.send(m, AgentEventMsg{Event: ev})
	if s, ok := m.agentSessions[key]; !ok || s.ID != "sess-live" || !s.FromHook {
		t.Fatalf("stale-pid push did not bind by cwd: %+v", m.agentSessions)
	}
	m = l.pump(t, m, 3*time.Second, nil, func(m Model) bool {
		return m.agentTracePanel().Info().Turns == 2
	})
	if n := l.ticks - start; n > 0 {
		t.Fatalf("the push waited for %d tick(s) instead of reading at once", n)
	}
	if info := m.agentTracePanel().Info(); !info.FromHook || info.Transcript != path {
		t.Fatalf("info = %+v", info)
	}
}

// TestAgentTraceReadSurvivesRelocation: a read in flight while a relocation
// (hook push, 'r') starts is applied when it lands — its events are consumed
// either way — and a read asked for meanwhile runs right after it.
func TestAgentTraceReadSurvivesRelocation(t *testing.T) {
	m, _, path := liveTrace(t)
	cwd := m.toolPane("watcher").Terminal().Cwd()
	appendTraceTurn(t, path, cwd, 2)

	m.traceBusy = false
	inFlight := m.traceReadCmd()
	if inFlight == nil {
		t.Fatal("no read started")
	}
	out, _ := m.Update(tracepanel.RefreshMsg{})
	m = out.(Model)
	if m.traceReadCmd() != nil || !m.tracePending {
		t.Fatal("a second read must wait for the one in flight and be remembered")
	}
	out, cmd := m.Update(inFlight())
	m = out.(Model)
	if got := m.agentTracePanel().Info().Turns; got != 2 {
		t.Fatalf("the in-flight read was dropped: turns = %d", got)
	}
	if cmd == nil || m.tracePending {
		t.Fatal("the remembered read must run once the first one reported")
	}
}

// TestAgentTraceRevivesLostChainAndRead: a poll chain whose tick got lost
// is restarted by the next relocation, and a read that never reported
// stops blocking after agentTraceReadLost.
func TestAgentTraceRevivesLostChainAndRead(t *testing.T) {
	m, _, _ := liveTrace(t)
	gen := m.traceTickGen
	out, _ := m.Update(tracepanel.RefreshMsg{})
	m = out.(Model)
	if m.traceTickGen != gen {
		t.Fatal("a live chain must not be restarted")
	}
	m.traceTickAt = time.Now().Add(-agentTraceTickLost - time.Second)
	out, _ = m.Update(tracepanel.RefreshMsg{})
	m = out.(Model)
	if m.traceTickGen != gen+1 {
		t.Fatal("a lost chain must be restarted by the relocation")
	}

	m.traceBusy, m.traceBusyAt = true, time.Now()
	if m.traceReadCmd() != nil {
		t.Fatal("a fresh in-flight read must block the next one")
	}
	m.traceBusyAt = time.Now().Add(-agentTraceReadLost - time.Second)
	if m.traceReadCmd() == nil {
		t.Fatal("a read lost for good must stop blocking")
	}
}
