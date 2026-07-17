package app

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
	"ike/internal/debugpanel"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/pane"
	"ike/internal/registry"
)

// stubAdapter answers every DAP request with success over an in-memory pipe
// and records the commands it saw plus the client's responses to reverse
// requests (#638).
type stubAdapter struct {
	in   *io.PipeReader
	out  *io.PipeWriter
	mu   sync.Mutex
	cmd  []string
	resp []reverseResp
}

// reverseResp is one client response to an adapter-initiated request.
type reverseResp struct {
	RequestSeq int             `json:"request_seq"`
	Command    string          `json:"command"`
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
	Body       json.RawMessage `json:"body"`
}

type stubPipe struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p stubPipe) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p stubPipe) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p stubPipe) Close() error                { p.w.Close(); return p.r.Close() }

func startStub(t *testing.T) (stubPipe, *stubAdapter) {
	t.Helper()
	cr, aw := io.Pipe()
	ar, cw := io.Pipe()
	sa := &stubAdapter{in: ar, out: aw}
	go sa.serve()
	t.Cleanup(func() { aw.Close(); ar.Close() })
	return stubPipe{r: cr, w: cw}, sa
}

func (s *stubAdapter) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cmd...)
}

func (s *stubAdapter) serve() {
	r := bufio.NewReader(s.in)
	seq := 1000
	for {
		data, err := jsonrpc.ReadFrame(r)
		if err != nil {
			return
		}
		var req struct {
			Seq     int    `json:"seq"`
			Type    string `json:"type"`
			Command string `json:"command"`
		}
		if json.Unmarshal(data, &req) != nil {
			continue
		}
		if req.Type == "response" {
			var rr reverseResp
			if json.Unmarshal(data, &rr) == nil {
				s.mu.Lock()
				s.resp = append(s.resp, rr)
				s.mu.Unlock()
			}
			continue
		}
		if req.Type != "request" {
			continue
		}
		s.mu.Lock()
		s.cmd = append(s.cmd, req.Command)
		seq++
		body := map[string]any{}
		if req.Command == "initialize" {
			// Advertise setVariable so the app-level edit gating is exercisable.
			body["supportsSetVariable"] = true
		}
		resp, _ := json.Marshal(map[string]any{
			"seq": seq, "type": "response", "request_seq": req.Seq,
			"command": req.Command, "success": true, "body": body,
		})
		_ = jsonrpc.WriteFrame(s.out, resp)
		s.mu.Unlock()
	}
}

// debugModel builds a sized model with an open file and a live stub session.
func debugModel(t *testing.T) (Model, *stubAdapter, string) {
	t.Helper()
	if testStoreRoot != "" {
		os.Setenv("IKE_CONFIG_DIR", filepath.Join(testStoreRoot, "debug-"+t.Name()))
	}
	path := filepath.Join(t.TempDir(), "prog.rfake")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	pipe, sa := startStub(t)
	sess := dap.NewSession(dap.NewConn(pipe, nil))
	// Run the capability handshake so the session carries the stub's
	// supportsSetVariable, like a real post-launch session would (#640).
	if err := sess.Initialize(); err != nil {
		t.Fatal(err)
	}
	m.dbg = &debugState{sess: sess, cfgName: "prog.rfake", root: projectRoot()}
	return m, sa, path
}

func waitForCommand(t *testing.T, sa *stubAdapter, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, c := range sa.commands() {
			if c == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("adapter never saw %q (saw %v)", want, sa.commands())
}

// waitForReverseResp blocks until the stub saw the client's response to the
// reverse request with request_seq seq (#638).
func waitForReverseResp(t *testing.T, sa *stubAdapter, seq int) reverseResp {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sa.mu.Lock()
		for _, r := range sa.resp {
			if r.RequestSeq == seq {
				sa.mu.Unlock()
				return r
			}
		}
		sa.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no response for reverse request %d", seq)
	return reverseResp{}
}

// TestDebugStopJumpsAndMarks verifies a stopped message records the frames,
// navigates to the top frame and sets the paused marker.
func TestDebugStopJumpsAndMarks(t *testing.T) {
	m, _, path := debugModel(t)
	frames := []dap.StackFrame{
		{ID: 1, Name: "inner", Source: dap.Source{Path: path}, Line: 3, Column: 1},
		{ID: 2, Name: "<module>", Source: dap.Source{Path: path}, Line: 4, Column: 1},
	}
	tm, _ := m.Update(debugStoppedMsg{threadID: 7, frames: frames})
	m = tm.(Model)
	if m.dbg == nil || !m.dbg.paused || m.dbg.threadID != 7 || len(m.dbg.frames) != 2 {
		t.Fatalf("stop state wrong: %+v", m.dbg)
	}
	ed := m.editorForPath(canonicalPath(path))
	if ed == nil {
		t.Fatal("the paused file must be open")
	}
	if line, ok := ed.PausedLine(); !ok || line != 2 {
		t.Fatalf("paused marker = %d/%v, want line 2 (0-based)", line, ok)
	}
	if line, _ := ed.CursorPos(); line != 2 {
		t.Fatalf("cursor line = %d, want 2", line)
	}
}

// TestDebugStepSendsRequestAndClearsPause verifies F8 semantics: only while
// paused, the marker clears and the adapter sees the step request.
func TestDebugStepSendsRequestAndClearsPause(t *testing.T) {
	m, sa, path := debugModel(t)
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	tm, _ = m.Update(DebugStepOverMsg{})
	m = tm.(Model)
	if m.dbg.paused {
		t.Fatal("stepping must leave the paused state")
	}
	ed := m.editorForPath(canonicalPath(path))
	if _, ok := ed.PausedLine(); ok {
		t.Fatal("stepping must clear the paused marker")
	}
	waitForCommand(t, sa, "next")
	// Not paused anymore: further steps are refused (no new request kinds).
	tm, _ = m.Update(DebugStepIntoMsg{})
	m = tm.(Model)
	for _, c := range sa.commands() {
		if c == "stepIn" {
			t.Fatal("a step while running must not reach the adapter")
		}
	}
}

// TestDebugStepWithoutSession is a friendly no-op.
func TestDebugStepWithoutSession(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	m.dbg = nil
	if tm, _ = m.Update(DebugStepOverMsg{}); tm.(Model).dbg != nil {
		t.Fatal("stepping without a session must stay a no-op")
	}
}

// TestDebugEndedCleansUp verifies termination clears the session and marker.
func TestDebugEndedCleansUp(t *testing.T) {
	m, _, path := debugModel(t)
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	tm, _ = m.Update(debugEndedMsg{exitCode: 0, hasCode: true})
	m = tm.(Model)
	if m.dbg != nil {
		t.Fatal("a terminated session must clear the state")
	}
	ed := m.editorForPath(canonicalPath(path))
	if _, ok := ed.PausedLine(); ok {
		t.Fatal("termination must clear the paused marker")
	}
}

// TestDebugStopCommand verifies debug.stop disconnects and clears state.
func TestDebugStopCommand(t *testing.T) {
	m, sa, _ := debugModel(t)
	tm, _ := m.Update(DebugStopMsg{})
	m = tm.(Model)
	if m.dbg != nil {
		t.Fatal("debug.stop must clear the session state")
	}
	waitForCommand(t, sa, "disconnect")
}

// TestDebugPanelOpensAndFrameSelection verifies the tool window (#580): a
// stop opens the bottom panel fed with the frames, and activating an outer
// frame re-scopes variables (adapter sees scopes) and navigates the editor.
func TestDebugPanelOpensAndFrameSelection(t *testing.T) {
	m, sa, path := debugModel(t)
	frames := []dap.StackFrame{
		{ID: 1, Name: "inner", Source: dap.Source{Path: path}, Line: 3, Column: 1},
		{ID: 2, Name: "outer", Source: dap.Source{Path: path}, Line: 4, Column: 1},
	}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if !m.panes.Has(pane.DebugKey) {
		t.Fatal("a stop must open the debug panel")
	}
	if m.panes.Get(pane.DebugKey).Kind() != pane.KindDebug {
		t.Fatal("the panel leaf must be the debug kind")
	}
	waitForCommand(t, sa, "scopes") // top frame scopes fetched eagerly
	// Selecting the outer frame re-scopes and navigates to its line.
	tm, cmd := m.Update(debugpanel.SelectFrameMsg{Frame: frames[1]})
	m = tm.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			tm, _ = m.Update(msg)
			m = tm.(Model)
		}
	}
	ed := m.editorForPath(canonicalPath(path))
	if line, _ := ed.CursorPos(); line != 3 {
		t.Fatalf("cursor line = %d, want 3 (outer frame)", line)
	}
	// Session end closes the panel again.
	tm, _ = m.Update(debugEndedMsg{})
	m = tm.(Model)
	if m.panes.Has(pane.DebugKey) {
		t.Fatal("session end must close the debug panel")
	}
}

// TestRunInTerminalRefusedWithoutSession verifies the dbg==nil bail-out still
// answers the reverse request — a silent return would hang the adapter (#638).
func TestRunInTerminalRefusedWithoutSession(t *testing.T) {
	m, sa, _ := debugModel(t)
	sess := m.dbg.sess
	m.dbg = nil
	tm, _ := m.Update(debugRunInTerminalMsg{seq: 42, sess: sess, args: dap.RunInTerminalArgs{Args: []string{"/bin/sh"}}})
	m = tm.(Model)
	resp := waitForReverseResp(t, sa, 42)
	if resp.Success || resp.Command != "runInTerminal" || resp.Message == "" {
		t.Fatalf("response = %+v, want a refusal with a reason", resp)
	}
	if m.dbgTermKey != "" {
		t.Fatal("no terminal must be tracked after a refusal")
	}
}

// TestRunInTerminalRefusedWithoutCommand verifies the empty-argv bail-out
// answers with an error instead of hanging the adapter (#638).
func TestRunInTerminalRefusedWithoutCommand(t *testing.T) {
	m, sa, _ := debugModel(t)
	tm, _ := m.Update(debugRunInTerminalMsg{seq: 43, sess: m.dbg.sess})
	_ = tm
	resp := waitForReverseResp(t, sa, 43)
	if resp.Success || resp.Message != "no command" {
		t.Fatalf("response = %+v, want the no-command refusal", resp)
	}
}

// TestRunInTerminalSpawnFailureClosesPane verifies a failed debuggee spawn
// refuses the request AND removes the just-split terminal pane again, so no
// dead pane lingers or gets persisted (#638).
func TestRunInTerminalSpawnFailureClosesPane(t *testing.T) {
	m, sa, _ := debugModel(t)
	before := len(layout.Leaves(m.tree))
	tm, _ := m.Update(debugRunInTerminalMsg{seq: 44, sess: m.dbg.sess,
		args: dap.RunInTerminalArgs{Args: []string{"/nonexistent-ike-binary-638"}}})
	m = tm.(Model)
	resp := waitForReverseResp(t, sa, 44)
	if resp.Success || resp.Message != "debuggee failed to start" {
		t.Fatalf("response = %+v, want the spawn-failure refusal", resp)
	}
	if got := len(layout.Leaves(m.tree)); got != before {
		t.Fatalf("leaves = %d, want %d — the dead pane must be closed again", got, before)
	}
	if m.dbgTermKey != "" {
		t.Fatal("a failed spawn must not track a terminal key")
	}
}

// TestRunInTerminalReplacesStaleTerminal verifies the debuggee terminal stays
// open after its process ends (output review) and the next runInTerminal
// closes it before splitting a fresh one; a user-closed pane clears the
// tracked key (#638).
func TestRunInTerminalReplacesStaleTerminal(t *testing.T) {
	m, sa, _ := debugModel(t)
	argv := []string{"/bin/sh", "-c", "exit 0"}
	tm, _ := m.Update(debugRunInTerminalMsg{seq: 45, sess: m.dbg.sess, args: dap.RunInTerminalArgs{Args: argv}})
	m = tm.(Model)
	if resp := waitForReverseResp(t, sa, 45); !resp.Success {
		t.Fatalf("first spawn refused: %+v", resp)
	}
	old := m.dbgTermKey
	if old == "" || !m.panes.Has(old) {
		t.Fatalf("first spawn must track its terminal pane, got %q", old)
	}
	// Wait for the short-lived debuggee to exit; the pane must survive it.
	deadline := time.Now().Add(3 * time.Second)
	for m.panes.Get(old).Terminal().Running() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if m.panes.Get(old).Terminal().Running() {
		t.Fatal("debuggee process never exited")
	}
	// A new session's runInTerminal replaces the stale pane.
	tm, _ = m.Update(debugRunInTerminalMsg{seq: 46, sess: m.dbg.sess, args: dap.RunInTerminalArgs{Args: argv}})
	m = tm.(Model)
	if resp := waitForReverseResp(t, sa, 46); !resp.Success {
		t.Fatalf("second spawn refused: %+v", resp)
	}
	if m.panes.Has(old) {
		t.Fatal("the stale debuggee terminal must be closed on relaunch")
	}
	if m.dbgTermKey == "" || m.dbgTermKey == old || !m.panes.Has(m.dbgTermKey) {
		t.Fatalf("dbgTermKey = %q, want the fresh terminal pane", m.dbgTermKey)
	}
	// A user close clears the tracked key, so nothing chases it later.
	if !m.closeKey(m.dbgTermKey) {
		t.Fatal("closing the debuggee terminal pane must succeed")
	}
	if m.dbgTermKey != "" {
		t.Fatal("closing the pane must clear dbgTermKey")
	}
}

// TestEnvMapToSliceSkipsNulls verifies null env values (unset per DAP) are
// tolerated and skipped (#638).
func TestEnvMapToSliceSkipsNulls(t *testing.T) {
	v := "1"
	got := envMapToSlice(map[string]*string{"A": &v, "B": nil})
	if len(got) != 1 || got[0] != "A=1" {
		t.Fatalf("envMapToSlice = %v, want [A=1]", got)
	}
	if envMapToSlice(nil) != nil {
		t.Fatal("empty map must yield nil")
	}
}
