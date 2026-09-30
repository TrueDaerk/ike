package deeplink

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validEvent() Event {
	return Event{
		SessionID:      "0b6f7a52-1c1e-4c1f-9d7e-2f4f0c3f6a11",
		CWD:            "/Users/me/src/ike",
		TranscriptPath: "/Users/me/.claude/projects/-Users-me-src-ike/0b6f7a52-1c1e-4c1f-9d7e-2f4f0c3f6a11.jsonl",
		Event:          "SessionStart",
		Pane:           "tool:claude#7",
		PID:            4242,
	}
}

func TestParseEvent(t *testing.T) {
	line, err := encodeEvent(validEvent())
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := strings.CutPrefix(line, "event ")
	if !ok {
		t.Fatalf("wire form %q lacks the event prefix", line)
	}
	got, err := ParseEvent(payload)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if got != validEvent() {
		t.Errorf("round trip = %+v, want %+v", got, validEvent())
	}
}

func TestParseEventRejects(t *testing.T) {
	cases := map[string]string{
		"not json":           `nope`,
		"array":              `[1,2]`,
		"unknown field":      `{"session_id":"a","cwd":"/x","event":"SessionStart","command":"rm -rf /"}`,
		"trailing data":      `{"session_id":"a","cwd":"/x","event":"SessionStart"} {}`,
		"unknown event":      `{"session_id":"a","cwd":"/x","event":"PreToolUse"}`,
		"empty session":      `{"session_id":"","cwd":"/x","event":"SessionStart"}`,
		"session with slash": `{"session_id":"../../etc","cwd":"/x","event":"SessionStart"}`,
		"relative cwd":       `{"session_id":"a","cwd":"x/y","event":"SessionStart"}`,
		"control in cwd":     `{"session_id":"a","cwd":"/x\ny","event":"SessionStart"}`,
		"transcript not jsonl": `{"session_id":"a","cwd":"/x","event":"SessionStart",` +
			`"transcript_path":"/etc/passwd"}`,
		"relative transcript": `{"session_id":"a","cwd":"/x","event":"SessionStart","transcript_path":"a.jsonl"}`,
		"negative pid":        `{"session_id":"a","cwd":"/x","event":"SessionStart","ike_pid":-1}`,
		"long pane":           `{"session_id":"a","cwd":"/x","event":"SessionStart","ike_session":"` + strings.Repeat("p", 300) + `"}`,
	}
	for name, payload := range cases {
		if _, err := ParseEvent(payload); err == nil {
			t.Errorf("%s: %q accepted", name, payload)
		}
	}
	// Every registered event name is accepted.
	for _, name := range AgentEvents {
		if _, err := ParseEvent(`{"session_id":"a","cwd":"/x","event":"` + name + `"}`); err != nil {
			t.Errorf("event %s refused: %v", name, err)
		}
	}
}

func TestEncodeEventLengthCap(t *testing.T) {
	e := validEvent()
	e.CWD = "/" + strings.Repeat("d", maxLinkLen)
	if _, err := encodeEvent(e); err == nil {
		t.Error("oversized event encoded")
	}
	if err := SendEvent(sockDir(t), e); err == nil || err == ErrNoInstance {
		t.Errorf("SendEvent oversized = %v, want a length error", err)
	}
}

func TestServeEventAndSend(t *testing.T) {
	dir := sockDir(t)
	got := make(chan Event, 1)
	links := make(chan string, 1)
	s, err := ServeHandlers(dir, Handlers{
		Open:  func(url string) { links <- url },
		Event: func(e Event) { got <- e },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := SendEvent(dir, validEvent()); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	select {
	case e := <-got:
		if e != validEvent() {
			t.Errorf("delivered %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event never delivered")
	}
	// The open form still works on the same endpoint.
	if err := Send(dir, "ike://open?project=ike"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case <-links:
	case <-time.After(2 * time.Second):
		t.Fatal("link never delivered")
	}
}

// TestServerRejectsInvalidEvent sends raw lines and checks the reply: an
// invalid event is answered with an error and never delivered, and an
// endpoint without an Event handler refuses the form altogether.
func TestServerRejectsInvalidEvent(t *testing.T) {
	dir := sockDir(t)
	delivered := make(chan Event, 1)
	s, err := ServeHandlers(dir, Handlers{Event: func(e Event) { delivered <- e }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sock := filepath.Join(dir, fmt.Sprintf("ike-%d.sock", os.Getpid()))
	for _, msg := range []string{
		`event {"session_id":"a","cwd":"rel","event":"SessionStart"}`,
		`event {"session_id":"a","cwd":"/x","event":"Bogus"}`,
		`open ike://open?project=x`, // no Open handler on this endpoint
		strings.Repeat("x", maxLinkLen+10),
	} {
		if reply := rawSend(t, sock, msg); strings.HasPrefix(reply, "ok") {
			t.Errorf("%.40q answered %q", msg, reply)
		}
	}
	select {
	case e := <-delivered:
		t.Errorf("invalid event delivered: %+v", e)
	case <-time.After(200 * time.Millisecond):
	}

	// Serve (open only) refuses events.
	dir2 := sockDir(t)
	s2, err := Serve(dir2, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err := SendEvent(dir2, validEvent()); err != ErrNoInstance {
		t.Errorf("event to an open-only endpoint = %v, want ErrNoInstance", err)
	}
}

// TestSendEventPrefersOwner: the instance named by the event's pid is tried
// before a more recently focused one.
func TestSendEventPrefersOwner(t *testing.T) {
	dir := sockDir(t)
	got := make(chan Event, 1)
	s, err := ServeHandlers(dir, Handlers{Event: func(e Event) { got <- e }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A foreign live listener with a fresher focus stamp that would accept
	// anything: the owner must still win.
	other := filepath.Join(dir, "ike-1.sock")
	ln, err := net.Listen("unix", other)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	stolen := make(chan bool, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		fmt.Fprintln(c, "ok")
		c.Close()
		stolen <- true
	}()
	focus := filepath.Join(dir, "ike-1.focus")
	_ = os.WriteFile(focus, nil, 0o600)
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(focus, future, future)

	e := validEvent()
	e.PID = os.Getpid()
	if err := SendEvent(dir, e); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	select {
	case <-got:
	case <-stolen:
		t.Fatal("the focused foreign instance received the owner's event")
	case <-time.After(2 * time.Second):
		t.Fatal("event never delivered")
	}
}

func rawSend(t *testing.T, sock, line string) string {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn, "%s\n", line)
	reply, _ := bufio.NewReader(conn).ReadString('\n')
	return reply
}
