package deeplink

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// event.go is the second message the socket accepts (#2843): "event {json}",
// a coding-agent session lifecycle push from `ike agent-hook`. Claude Code
// runs the hook on SessionStart / SessionEnd / UserPromptSubmit; the CLI
// forwards the session id, cwd and transcript path so a tool pane learns its
// session without scanning ~/.claude/projects.
//
// Same trust rules as "open": one line, the shared length cap, strict
// validation before anything is delivered. The receiver only ever records
// the ids and paths — nothing in an event runs, opens or reads anything.

// AgentEvents are the hook event names an event message may carry: the ones
// `agent.hooks.install` registers.
var AgentEvents = []string{"SessionStart", "SessionEnd", "UserPromptSubmit"}

// Event is one agent lifecycle push. The JSON names are Claude Code's hook
// input field names, so the CLI forwards what it read verbatim.
type Event struct {
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Event          string `json:"event"`
	// Pane is the IKE terminal session the agent runs in ($IKE_SESSION,
	// injected into every terminal spawn) and PID the IKE process that owns
	// it ($IKE_PID); both empty/0 when the agent runs outside IKE.
	Pane string `json:"ike_session,omitempty"`
	PID  int    `json:"ike_pid,omitempty"`
}

// maxFieldLen bounds a single id field; paths are bounded by maxLinkLen.
const maxFieldLen = 256

// Validate rejects anything that is not a plausible lifecycle push: a known
// event name, a session id of id characters only, absolute paths without
// control bytes, a transcript that is a .jsonl file.
func (e Event) Validate() error {
	known := false
	for _, name := range AgentEvents {
		known = known || e.Event == name
	}
	if !known {
		return fmt.Errorf("unknown event %q", e.Event)
	}
	if !isID(e.SessionID) {
		return fmt.Errorf("invalid session id")
	}
	if !cleanAbs(e.CWD) {
		return fmt.Errorf("cwd must be an absolute path")
	}
	if e.TranscriptPath != "" && (!cleanAbs(e.TranscriptPath) || !strings.HasSuffix(e.TranscriptPath, ".jsonl")) {
		return fmt.Errorf("transcript_path must be an absolute .jsonl path")
	}
	if e.Pane != "" && (len(e.Pane) > maxFieldLen || strings.ContainsFunc(e.Pane, isControl)) {
		return fmt.Errorf("invalid ike_session")
	}
	if e.PID < 0 {
		return fmt.Errorf("invalid ike_pid")
	}
	return nil
}

// isID reports whether s is a non-empty id of [A-Za-z0-9_-] — Claude Code's
// session ids are UUIDs.
func isID(s string) bool {
	if s == "" || len(s) > maxFieldLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// cleanAbs reports whether p is an absolute path without control bytes.
func cleanAbs(p string) bool {
	return p != "" && filepath.IsAbs(p) && !strings.ContainsFunc(p, isControl)
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// ParseEvent decodes and validates the payload of an "event " message.
// Unknown fields are refused, like an unknown message form.
func ParseEvent(payload string) (Event, error) {
	dec := json.NewDecoder(strings.NewReader(payload))
	dec.DisallowUnknownFields()
	var e Event
	if err := dec.Decode(&e); err != nil {
		return Event{}, fmt.Errorf("malformed event: %v", err)
	}
	if dec.More() {
		return Event{}, fmt.Errorf("malformed event: trailing data")
	}
	return e, e.Validate()
}

// encodeEvent renders the one-line wire form of e.
func encodeEvent(e Event) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return "", err
	}
	line := "event " + strings.TrimSpace(buf.String())
	if len(line)+1 > maxLinkLen {
		return "", fmt.Errorf("event too long")
	}
	return line, nil
}

// SendEvent delivers e to a running instance: the instance named by e.PID
// first (the agent runs in one of its terminals), then every other one in
// focus order like Send. ErrNoInstance when nobody acknowledged.
func SendEvent(dir string, e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	line, err := encodeEvent(e)
	if err != nil {
		return err
	}
	socks := sockets(dir)
	if e.PID > 0 {
		owner := filepath.Join(dir, fmt.Sprintf("ike-%d.sock", e.PID))
		for i, s := range socks {
			if s == owner {
				socks = append(append([]string{owner}, socks[:i]...), socks[i+1:]...)
				break
			}
		}
	}
	return deliverLine(socks, line)
}

// sendLine is one delivery attempt of a raw message line; true when the
// instance acknowledged it.
func sendLine(sock, line string) bool {
	conn, err := net.DialTimeout("unix", sock, ipcTimeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(ipcTimeout))
	if _, err := fmt.Fprintf(conn, "%s\n", line); err != nil {
		return false
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	return err == nil && strings.TrimSpace(reply) == "ok"
}

// removeDead cleans up the files of an instance that did not answer.
func removeDead(sock string) {
	_ = os.Remove(sock)
	_ = os.Remove(strings.TrimSuffix(sock, ".sock") + ".focus")
}
