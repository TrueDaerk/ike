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
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/registry"
)

// stubAdapter answers every DAP request with success over an in-memory pipe
// and records the commands it saw.
type stubAdapter struct {
	in  *io.PipeReader
	out *io.PipeWriter
	mu  sync.Mutex
	cmd []string
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
		if json.Unmarshal(data, &req) != nil || req.Type != "request" {
			continue
		}
		s.mu.Lock()
		s.cmd = append(s.cmd, req.Command)
		seq++
		resp, _ := json.Marshal(map[string]any{
			"seq": seq, "type": "response", "request_seq": req.Seq,
			"command": req.Command, "success": true, "body": map[string]any{},
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
