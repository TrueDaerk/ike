package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"ike/internal/deeplink"
)

// agenthook.go is `ike agent-hook <event>` (#2843), the command Claude Code
// runs for the hooks agent.hooks.install writes. It reads the hook input JSON
// on stdin, keeps the session id, cwd and transcript path, adds the IKE
// terminal the agent runs in ($IKE_SESSION / $IKE_PID, injected into every
// terminal spawn) and forwards it all as a deeplink "event" message.
//
// The hook must never get in the agent's way: whatever happens — no IKE
// running, garbage on stdin — the process exits 0 (a non-zero exit from a
// UserPromptSubmit hook would block the prompt), and the only trace of a
// failure is one line on stderr, which Claude Code shows in its debug log.

// maxHookInput bounds what is read from stdin: the fields IKE needs sit in
// a small object, but Claude Code includes the whole prompt for
// UserPromptSubmit.
const maxHookInput = 1 << 20

// hookInput is the subset of Claude Code's hook input IKE forwards.
type hookInput struct {
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path"`
	HookEventName  string `json:"hook_event_name"`
}

// buildHookEvent turns the hook input on r into the socket message for
// event. The command-line event name wins over the input's
// hook_event_name — it is what the installed entry said.
func buildHookEvent(r io.Reader, event string, getenv func(string) string) (deeplink.Event, error) {
	var in hookInput
	if err := json.NewDecoder(io.LimitReader(r, maxHookInput)).Decode(&in); err != nil {
		return deeplink.Event{}, fmt.Errorf("hook input: %v", err)
	}
	ev := deeplink.Event{
		SessionID:      in.SessionID,
		CWD:            in.CWD,
		TranscriptPath: in.TranscriptPath,
		Event:          event,
		Pane:           getenv("IKE_SESSION"),
	}
	if pid, err := strconv.Atoi(getenv("IKE_PID")); err == nil && pid > 0 {
		ev.PID = pid
	}
	return ev, ev.Validate()
}

// runAgentHook is the whole subcommand; the returned error is only printed.
func runAgentHook(r io.Reader, event string, getenv func(string) string, dir string) error {
	ev, err := buildHookEvent(r, event, getenv)
	if err != nil {
		return err
	}
	if err := deeplink.SendEvent(dir, ev); err != nil && err != deeplink.ErrNoInstance {
		return err
	}
	return nil
}
