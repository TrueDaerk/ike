package config

import "testing"

// The agent.trace.view setting (#2858) defaults to the graph view and
// accepts only "graph" or "tree"; anything else falls back with a
// diagnostic. The key is part of the flat view the settings UI reads.
func TestValidateAgentTraceView(t *testing.T) {
	c := defaults()
	if c.Agent.Trace.View != "graph" {
		t.Fatalf("default view = %q", c.Agent.Trace.View)
	}
	for _, bad := range []string{"", "snake", "GRAPH"} {
		c := defaults()
		c.Agent.Trace.View = bad
		diags := validate(c)
		if c.Agent.Trace.View != "graph" || len(diagsFor(diags, "agent.trace.view")) != 1 {
			t.Errorf("view %q validated to %q with %v", bad, c.Agent.Trace.View, diags)
		}
	}
	for _, good := range []string{"graph", "tree"} {
		c := defaults()
		c.Agent.Trace.View = good
		if diags := validate(c); len(diagsFor(diags, "agent.trace.view")) != 0 || c.Agent.Trace.View != good {
			t.Errorf("view %q is valid: %v", good, diags)
		}
	}
	if flat := defaults().Flat(); flat["agent.trace.view"] != "graph" {
		t.Errorf("flat = %q", flat["agent.trace.view"])
	}
}

// The agent.trace.history_max_sessions setting (#2860) defaults to 50
// stored sessions and stays inside 1–500; the key is in the flat view.
func TestValidateAgentTraceHistoryMaxSessions(t *testing.T) {
	c := defaults()
	if c.Agent.Trace.HistoryMaxSessions != 50 {
		t.Fatalf("default history_max_sessions = %d", c.Agent.Trace.HistoryMaxSessions)
	}
	for _, bad := range []int{0, -3, 501, 10000} {
		c := defaults()
		c.Agent.Trace.HistoryMaxSessions = bad
		diags := validate(c)
		if c.Agent.Trace.HistoryMaxSessions != 50 || len(diagsFor(diags, "agent.trace.history_max_sessions")) != 1 {
			t.Errorf("history_max_sessions %d validated to %d with %v", bad, c.Agent.Trace.HistoryMaxSessions, diags)
		}
	}
	for _, good := range []int{1, 50, 500} {
		c := defaults()
		c.Agent.Trace.HistoryMaxSessions = good
		if diags := validate(c); len(diagsFor(diags, "agent.trace.history_max_sessions")) != 0 || c.Agent.Trace.HistoryMaxSessions != good {
			t.Errorf("history_max_sessions %d is valid: %v", good, diags)
		}
	}
	if flat := defaults().Flat(); flat["agent.trace.history_max_sessions"] != "50" {
		t.Errorf("flat = %q", flat["agent.trace.history_max_sessions"])
	}
}
