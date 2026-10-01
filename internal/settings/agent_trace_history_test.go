package settings

// agent_trace_history_test.go covers the Settings-UI half of the trace
// history cap (#2860): agent.trace.history_max_sessions is a bounded Int
// entry on the Agent Trace page that validates, clamps, persists and
// renders — never config-file-only.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

func TestAgentTraceHistoryMaxSessionsEntryShape(t *testing.T) {
	e := agentAskEntry(t, "agent.trace.history_max_sessions")
	if e.Type != Int || e.Min != 1 || e.Max != 500 || e.Scope != config.UserScope {
		t.Fatalf("entry = %+v, want a user-scoped Int bounded 1–500", e)
	}
	for _, want := range []string{"agent-trace", "import", "prune"} {
		if !strings.Contains(e.Description, want) {
			t.Errorf("the description must mention %q: %q", want, e.Description)
		}
	}
}

func TestAgentTraceHistoryMaxSessionsClampsAndPersists(t *testing.T) {
	m := agentAskPanel(t, "agent.trace.history_max_sessions")
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	ed.tf.Set("lots")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("a non-numeric cap must not write (err=%q)", ed.err)
	}
	ed.tf.Set("9000")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Agent.Trace.HistoryMaxSessions; got != 500 {
		t.Fatalf("history_max_sessions = %d, want clamped 500", got)
	}
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("0")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Agent.Trace.HistoryMaxSessions; got != 1 {
		t.Fatalf("history_max_sessions = %d, want clamped 1", got)
	}
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("25")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Agent.Trace.HistoryMaxSessions; got != 25 {
		t.Fatalf("history_max_sessions = %d, want 25", got)
	}
	if view := m.View(); !strings.Contains(view, "25") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}
