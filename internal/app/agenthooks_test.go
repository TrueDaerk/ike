package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/agenttrace"
	"ike/internal/deeplink"
)

// openWatcher opens a sleeping tool pane and returns the model and the
// tool's terminal session key.
func openWatcher(t *testing.T) (Model, string) {
	t.Helper()
	withTools(t, sleepTool("watcher"))
	m := sized(t, 100, 40)
	out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	inst := m.toolPane("watcher")
	if inst == nil {
		t.Fatal("tool pane did not open")
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	return m, inst.Terminal().SessionKey()
}

func agentEvent(event, cwd string) deeplink.Event {
	return deeplink.Event{
		SessionID: "sess-1", CWD: cwd, Event: event,
		TranscriptPath: "/tmp/p/sess-1.jsonl",
	}
}

func sendAgentEvent(m Model, ev deeplink.Event) Model {
	out, _ := m.Update(AgentEventMsg{Event: ev})
	return out.(Model)
}

// TestAgentEventBindsByPane: an event naming this instance's terminal via
// $IKE_SESSION binds there regardless of the cwd.
func TestAgentEventBindsByPane(t *testing.T) {
	m, key := openWatcher(t)
	ev := agentEvent("SessionStart", "/nowhere/else")
	ev.Pane, ev.PID = key, os.Getpid()
	m = sendAgentEvent(m, ev)
	s, ok := m.agentSessionFor(key)
	if !ok || s.ID != "sess-1" || !s.FromHook || s.Ended || s.Transcript != "/tmp/p/sess-1.jsonl" {
		t.Fatalf("binding = %+v, %v", s, ok)
	}

	// Another instance's pid never matches by pane key — and the cwd does
	// not match either, so nothing binds.
	other := agentEvent("SessionStart", "/nowhere/else")
	other.SessionID, other.Pane, other.PID = "sess-2", key, os.Getpid()+1
	m = sendAgentEvent(m, other)
	if s, _ := m.agentSessionFor(key); s.ID != "sess-1" {
		t.Errorf("foreign-pid event rebound the pane to %q", s.ID)
	}

	// SessionEnd keeps the binding but marks it ended.
	ev.Event = "SessionEnd"
	m = sendAgentEvent(m, ev)
	if s, _ := m.agentSessionFor(key); !s.Ended || s.ID != "sess-1" {
		t.Errorf("after SessionEnd = %+v", s)
	}
}

// TestAgentEventBindsByCwd: without a pane key the event binds to the tool
// pane whose working directory is the session's.
func TestAgentEventBindsByCwd(t *testing.T) {
	m, key := openWatcher(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	m = sendAgentEvent(m, agentEvent("UserPromptSubmit", cwd+string(filepath.Separator)))
	if s, ok := m.agentSessionFor(key); !ok || s.ID != "sess-1" {
		t.Fatalf("cwd binding = %+v, %v", s, ok)
	}
}

// TestAgentEventUnmatchedIsDropped: no terminal in the directory — nothing
// binds, discovery stays the pane's source.
func TestAgentEventUnmatchedIsDropped(t *testing.T) {
	m, key := openWatcher(t)
	m = sendAgentEvent(m, agentEvent("SessionStart", t.TempDir()))
	if _, ok := m.agentSessionFor(key); ok || len(m.agentSessions) != 0 {
		t.Errorf("unmatched event bound: %v", m.agentSessions)
	}
}

// TestLocateAgentSessionFallsBackToDiscovery: a live hook binding wins; an
// ended or missing one falls back to the transcript scan.
func TestLocateAgentSessionFallsBackToDiscovery(t *testing.T) {
	projects := t.TempDir()
	cwd := t.TempDir()
	dir := filepath.Join(projects, agenttrace.EncodeCWD(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"disc-1","cwd":"` + cwd + `","uuid":"u1",` +
		`"timestamp":"2026-09-30T10:00:00Z","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "disc-1.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	bound := agentSession{ID: "hook-1", Transcript: "/x/hook-1.jsonl", FromHook: true}
	if s, err := locateAgentSession(bound, true, projects, cwd); err != nil || s.ID != "hook-1" {
		t.Errorf("live binding = %+v, %v", s, err)
	}
	bound.Ended = true
	if s, err := locateAgentSession(bound, true, projects, cwd); err != nil || s.ID != "disc-1" || s.FromHook {
		t.Errorf("ended binding = %+v, %v; want discovery", s, err)
	}
	if s, err := locateAgentSession(agentSession{}, false, projects, cwd); err != nil || s.ID != "disc-1" {
		t.Errorf("no binding = %+v, %v; want discovery", s, err)
	}
	if _, err := locateAgentSession(agentSession{}, false, projects, t.TempDir()); err == nil {
		t.Error("empty directory found a session")
	}
}

// TestAgentHooksCommands runs agent.hooks.install / uninstall against a
// temporary Claude config dir.
func TestAgentHooksCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	exe := filepath.Join(dir, "ike")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	prev := agentHookExe
	agentHookExe = func() (string, error) { return exe, nil }
	t.Cleanup(func() { agentHookExe = prev })
	m := sized(t, 100, 40)

	_, cmd := m.Update(AgentHooksInstallMsg{})
	if cmd == nil {
		t.Fatal("install dispatched no work")
	}
	done, ok := cmd().(agentHooksDoneMsg)
	if !ok || done.err != nil || !done.changed || !done.install {
		t.Fatalf("install = %+v", done)
	}
	m.Update(done)
	path := filepath.Join(dir, "settings.json")
	if got, err := agenttrace.HooksInstalled(path); err != nil || len(got) != len(agenttrace.HookEvents) {
		t.Fatalf("installed = %v, %v", got, err)
	}

	_, cmd = m.Update(AgentHooksUninstallMsg{})
	done = cmd().(agentHooksDoneMsg)
	if done.err != nil || !done.changed || done.install {
		t.Fatalf("uninstall = %+v", done)
	}
	if got, _ := agenttrace.HooksInstalled(path); len(got) != 0 {
		t.Errorf("hooks left after uninstall: %v", got)
	}
}
