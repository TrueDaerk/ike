package settings

// agent_trace_test.go covers the Settings-UI half of agent.trace.view
// (#2858): the key is a real Enum entry on the Agent Trace page whose two
// values persist and render — never config-file-only.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

func TestAgentTraceViewEntryShape(t *testing.T) {
	e := agentAskEntry(t, "agent.trace.view")
	if e.Type != Enum || e.Scope != config.UserScope || strings.Join(e.Options, ",") != "graph,tree" {
		t.Fatalf("entry = %+v, want a user-scoped Enum of graph/tree", e)
	}
	for _, want := range []string{"graph", "tree", "t key"} {
		if !strings.Contains(e.Description, want) {
			t.Errorf("the description must mention %q: %q", want, e.Description)
		}
	}
}

func TestAgentTraceViewPicksAndPersists(t *testing.T) {
	m := agentAskPanel(t, "agent.trace.view")
	ed, ok := m.editor.(*enumEditor)
	if !ok {
		t.Fatalf("editor = %T, want *enumEditor", m.editor)
	}
	if got := ed.matches(); len(got) != 2 {
		t.Fatalf("option list = %v, want the two views", got)
	}
	m.Update(key("down"))
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Agent.Trace.View; got != "tree" {
		t.Fatalf("view = %q, want tree", got)
	}
	if v := m.View(); !strings.Contains(v, "tree") {
		t.Fatalf("the list must render the new value:\n%s", v)
	}
	m.Update(key("enter"))
	m.Update(key("up"))
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Agent.Trace.View; got != "graph" {
		t.Fatalf("view = %q, want graph", got)
	}
}
