package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/deeplink"
)

const hookJSON = `{"session_id":"abc-123","transcript_path":"/home/me/.claude/projects/-home-me-x/abc-123.jsonl",
"cwd":"/home/me/x","hook_event_name":"UserPromptSubmit","prompt":"fix <the> bug","permission_mode":"default"}`

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestBuildHookEvent(t *testing.T) {
	ev, err := buildHookEvent(strings.NewReader(hookJSON), "UserPromptSubmit",
		env(map[string]string{"IKE_SESSION": "tool:claude#3", "IKE_PID": "77"}))
	if err != nil {
		t.Fatal(err)
	}
	want := deeplink.Event{
		SessionID:      "abc-123",
		CWD:            "/home/me/x",
		TranscriptPath: "/home/me/.claude/projects/-home-me-x/abc-123.jsonl",
		Event:          "UserPromptSubmit",
		Pane:           "tool:claude#3",
		PID:            77,
	}
	if ev != want {
		t.Errorf("event = %+v, want %+v", ev, want)
	}
	// Outside IKE: no pane, no pid; a junk pid is ignored.
	ev, err = buildHookEvent(strings.NewReader(hookJSON), "SessionEnd", env(map[string]string{"IKE_PID": "x"}))
	if err != nil || ev.Pane != "" || ev.PID != 0 || ev.Event != "SessionEnd" {
		t.Errorf("outside IKE = %+v, %v", ev, err)
	}
	for _, in := range []string{"", "nope", `{"session_id":"a"}`, `{"session_id":"a b","cwd":"/x"}`} {
		if _, err := buildHookEvent(strings.NewReader(in), "SessionStart", env(nil)); err == nil {
			t.Errorf("input %q accepted", in)
		}
	}
	if _, err := buildHookEvent(strings.NewReader(hookJSON), "PreToolUse", env(nil)); err == nil {
		t.Error("unknown event accepted")
	}
}

func TestRunAgentHookDelivers(t *testing.T) {
	base, err := os.MkdirTemp("", "ah")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	dir := filepath.Join(base, "dl")
	// Nobody running is not an error: the hook must stay silent.
	if err := runAgentHook(strings.NewReader(hookJSON), "SessionStart", env(nil), dir); err != nil {
		t.Errorf("no instance = %v, want nil", err)
	}
	got := make(chan deeplink.Event, 1)
	s, err := deeplink.ServeHandlers(dir, deeplink.Handlers{Event: func(e deeplink.Event) { got <- e }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := runAgentHook(strings.NewReader(hookJSON), "SessionStart", env(nil), dir); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-got:
		if e.SessionID != "abc-123" || e.Event != "SessionStart" {
			t.Errorf("delivered %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event never delivered")
	}
}
