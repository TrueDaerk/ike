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

// TestSendEventSkipsDeadSockets (#2857): a directory of crashed instances'
// leftovers — sockets of vanished pids, and one of a live pid nobody listens
// on — all focused more recently than the live instance, plus a live
// instance that refuses the message. The event still reaches the live one
// well inside the hook's 5 s timeout, the leftovers are removed and the
// refusing instance keeps its socket.
func TestSendEventSkipsDeadSockets(t *testing.T) {
	dir := sockDir(t)
	got := make(chan Event, 1)
	s, err := ServeHandlers(dir, Handlers{Event: func(e Event) { got <- e }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	stamp := func(sock string, at time.Time) {
		focus := strings.TrimSuffix(sock, ".sock") + ".focus"
		_ = os.WriteFile(focus, nil, 0o600)
		_ = os.Chtimes(focus, at, at)
	}
	// leftover binds a real socket file and closes it without unlinking:
	// what a killed instance leaves behind.
	leftover := func(name string) string {
		sock := filepath.Join(dir, name)
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		ln.(*net.UnixListener).SetUnlinkOnClose(false)
		ln.Close()
		return sock
	}
	future := time.Now().Add(time.Hour)
	var dead []string
	for i, pid := range []int{99999991, 99999992, 99999993, 99999994, 99999995, 99999996} {
		sock := leftover(fmt.Sprintf("ike-%d.sock", pid))
		stamp(sock, future.Add(time.Duration(i)*time.Minute))
		dead = append(dead, sock)
	}
	// pid 1 is always alive, but nothing listens on its leftover: the
	// refused dial marks it dead.
	refusedDial := leftover("ike-1.sock")
	stamp(refusedDial, future)
	dead = append(dead, refusedDial)

	// A live instance that answers with an error (an older build without
	// event support): skipped, never deleted.
	refusing := filepath.Join(dir, fmt.Sprintf("ike-%d.sock", os.Getppid()))
	ln, err := net.Listen("unix", refusing)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			bufio.NewReader(c).ReadString('\n')
			fmt.Fprintln(c, "err unsupported message")
			c.Close()
		}
	}()
	stamp(refusing, future.Add(-time.Minute))

	e := validEvent()
	e.PID = 0 // no owner preference: every leftover is tried first
	start := time.Now()
	if err := SendEvent(dir, e); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("delivery took %v", took)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("event never delivered")
	}
	for _, sock := range dead {
		if _, err := os.Stat(sock); !os.IsNotExist(err) {
			t.Errorf("dead socket %s not removed: %v", filepath.Base(sock), err)
		}
		if _, err := os.Stat(strings.TrimSuffix(sock, ".sock") + ".focus"); !os.IsNotExist(err) {
			t.Errorf("focus stamp of %s not removed", filepath.Base(sock))
		}
	}
	if _, err := os.Stat(refusing); err != nil {
		t.Errorf("the refusing live instance lost its socket: %v", err)
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
