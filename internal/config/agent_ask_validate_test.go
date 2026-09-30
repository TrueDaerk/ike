package config

import "testing"

// The agent.ask settings (#2845) default to a one-turn sonnet fork that
// shows its context; the model must be one word and the turn budget 1–5.
func TestValidateAgentAsk(t *testing.T) {
	c := defaults()
	if c.Agent.Ask.Model != "sonnet" || c.Agent.Ask.MaxTurns != 1 || !c.Agent.Ask.ShowContext {
		t.Fatalf("defaults = %+v", c.Agent.Ask)
	}
	for _, bad := range []string{"", "   ", "two words", "a\tb"} {
		c := defaults()
		c.Agent.Ask.Model = bad
		diags := validate(c)
		if c.Agent.Ask.Model != "sonnet" || len(diagsFor(diags, "agent.ask.model")) != 1 {
			t.Errorf("model %q validated to %q with %v", bad, c.Agent.Ask.Model, diags)
		}
	}
	for _, good := range []string{"sonnet", "opus", "claude-sonnet-5-5", " opus "} {
		c := defaults()
		c.Agent.Ask.Model = good
		if diags := validate(c); len(diagsFor(diags, "agent.ask.model")) != 0 {
			t.Errorf("model %q is valid: %v", good, diags)
		}
	}
	c = defaults()
	c.Agent.Ask.Model = " opus "
	validate(c)
	if c.Agent.Ask.Model != "opus" {
		t.Errorf("model must be trimmed, got %q", c.Agent.Ask.Model)
	}
	for _, bad := range []int{0, -1, 6, 100} {
		c := defaults()
		c.Agent.Ask.MaxTurns = bad
		diags := validate(c)
		if c.Agent.Ask.MaxTurns != 1 || len(diagsFor(diags, "agent.ask.max_turns")) != 1 {
			t.Errorf("max_turns %d validated to %d with %v", bad, c.Agent.Ask.MaxTurns, diags)
		}
	}
	for _, good := range []int{1, 3, 5} {
		c := defaults()
		c.Agent.Ask.MaxTurns = good
		if diags := validate(c); len(diagsFor(diags, "agent.ask.max_turns")) != 0 || c.Agent.Ask.MaxTurns != good {
			t.Errorf("max_turns %d is valid: %d, %v", good, c.Agent.Ask.MaxTurns, diags)
		}
	}
	flat := defaults().Flat()
	if flat["agent.ask.model"] != "sonnet" || flat["agent.ask.max_turns"] != "1" || flat["agent.ask.show_context"] != "true" {
		t.Errorf("flat = %q %q %q", flat["agent.ask.model"], flat["agent.ask.max_turns"], flat["agent.ask.show_context"])
	}
}
