package settings

// agent_ask_test.go covers the Settings-UI half of agent.ask (#2845): the
// three agent.ask.* keys are real entries on the Agent Trace page that
// validate, persist and render — never config-file-only.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// agentAskPage returns the Agent Trace page.
func agentAskPage(t *testing.T) Page {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		if p.Title == "Agent Trace" {
			return p
		}
	}
	t.Fatal("the Agent Trace settings page is missing")
	return Page{}
}

// agentAskEntry finds one entry of the page.
func agentAskEntry(t *testing.T, key string) Entry {
	t.Helper()
	for _, e := range agentAskPage(t).Entries {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("%s must be configurable in the settings UI", key)
	return Entry{}
}

func TestAgentAskEntriesShape(t *testing.T) {
	model := agentAskEntry(t, "agent.ask.model")
	if model.Type != String || model.ValidateString == nil || model.Scope != config.UserScope {
		t.Fatalf("model entry = %+v, want a validated user-scoped String", model)
	}
	for _, want := range []string{"sonnet", "opus", "full model id"} {
		if !strings.Contains(model.Description, want) {
			t.Errorf("the model description must name %q: %q", want, model.Description)
		}
	}
	turns := agentAskEntry(t, "agent.ask.max_turns")
	if turns.Type != Int || turns.Min != 1 || turns.Max != 5 {
		t.Fatalf("max_turns entry = %+v, want an Int bounded 1–5", turns)
	}
	if show := agentAskEntry(t, "agent.ask.show_context"); show.Type != Bool {
		t.Fatalf("show_context entry = %+v, want a Bool", show)
	}
	// The form validator follows the config rule.
	if model.ValidateString("") == "" || model.ValidateString("two words") == "" {
		t.Error("an empty or multi-word model must be refused")
	}
	for _, ok := range []string{"sonnet", "opus", "claude-sonnet-5-5"} {
		if msg := model.ValidateString(ok); msg != "" {
			t.Errorf("%q refused: %s", ok, msg)
		}
	}
}

// agentAskPanel opens a panel on the real page with the given entry's
// editor open.
func agentAskPanel(t *testing.T, k string) *Model {
	t.Helper()
	restoreConfig(t)
	m := New([]Page{agentAskPage(t)}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.focus = formColumn
	for i, r := range m.rows() {
		if r.kind == rowEntry && r.entry.Key == k {
			m.sel = i
		}
	}
	m.Update(key("enter"))
	return m
}

func TestAgentAskModelValidatesAndPersists(t *testing.T) {
	m := agentAskPanel(t, "agent.ask.model")
	ed, ok := m.editor.(*textEditor)
	if !ok {
		t.Fatalf("editor = %T, want *textEditor", m.editor)
	}
	// Two words are refused inline, without a write.
	ed.tf.Set("sonnet please")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid model must not write (err=%q)", ed.err)
	}
	if got := config.Get().Agent.Ask.Model; got != "sonnet" {
		t.Fatalf("refused value leaked: %q", got)
	}
	// A full model id persists and shows up in the list rendering.
	ed.tf.Set("claude-sonnet-5-5")
	m.Update(key("enter"))
	if ed.err != "" {
		t.Fatalf("a valid model was refused: %s", ed.err)
	}
	commit(t, m)
	if got := config.Get().Agent.Ask.Model; got != "claude-sonnet-5-5" {
		t.Fatalf("model = %q", got)
	}
	if view := m.View(); !strings.Contains(view, "claude-sonnet-5-5") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}

func TestAgentAskMaxTurnsClampsAndPersists(t *testing.T) {
	m := agentAskPanel(t, "agent.ask.max_turns")
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	ed.tf.Set("many")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("non-numeric turns must not write (err=%q)", ed.err)
	}
	ed.tf.Set("9")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Agent.Ask.MaxTurns; got != 5 {
		t.Fatalf("max_turns = %d, want clamped 5", got)
	}
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("0")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Agent.Ask.MaxTurns; got != 1 {
		t.Fatalf("max_turns = %d, want clamped 1", got)
	}
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("3")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Agent.Ask.MaxTurns; got != 3 {
		t.Fatalf("max_turns = %d, want 3", got)
	}
	if view := m.View(); !strings.Contains(view, "3") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}

func TestAgentAskShowContextToggles(t *testing.T) {
	// Enter on a Bool row toggles it in one step and stages the write.
	m := agentAskPanel(t, "agent.ask.show_context")
	if _, ok := m.editor.(*boolEditor); !ok {
		t.Fatalf("editor = %T, want *boolEditor", m.editor)
	}
	commit(t, m)
	if config.Get().Agent.Ask.ShowContext {
		t.Fatal("enter on the toggle row must write the other value")
	}
	if view := m.View(); !strings.Contains(view, "● off") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
	m.Update(key("enter"))
	commit(t, m)
	if !config.Get().Agent.Ask.ShowContext {
		t.Fatal("a second enter must toggle back")
	}
}
