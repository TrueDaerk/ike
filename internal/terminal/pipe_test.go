package terminal

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// waitView polls the model's view until want appears — the pipe feed runs on
// the session's async feed loop.
func waitView(t *testing.T, m *Model, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(m.View(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("view never showed %q:\n%s", want, m.View())
}

// TestPipeFeedRendersText (#1370): a pipe session renders fed text like a PTY
// terminal, with bare newlines normalized so lines start at column zero.
func TestPipeFeedRendersText(t *testing.T) {
	m := NewPipe("pipe-test", 40, 6, nil)
	t.Cleanup(m.Close)
	if !m.IsPipe() {
		t.Fatal("NewPipe must build a pipe session")
	}
	if !m.Running() {
		t.Fatal("a fresh pipe session runs until FinishPipe/Close")
	}
	m.FeedText("hello\nworld\n")
	waitView(t, &m, "hello")
	waitView(t, &m, "world")
	// The \n → \r\n normalization keeps the second line at column zero.
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "world") && !strings.HasPrefix(line, "world") {
			t.Fatalf("line not at column zero: %q", line)
		}
	}
}

// TestPipeFinishShowsExitAndStaysFeedable (#1370): FinishPipe renders the
// dead view with the exit code, but the session stays open so output the
// adapter flushes past `terminated` still lands (#637).
func TestPipeFinishShowsExitAndStaysFeedable(t *testing.T) {
	m := NewPipe("pipe-finish", 40, 6, nil)
	t.Cleanup(m.Close)
	m.FeedText("payload\n")
	waitView(t, &m, "payload")
	m.FinishPipe(3, true)
	waitView(t, &m, "[process exited with code 3]")
	if !strings.Contains(m.View(), "payload") {
		t.Fatal("finished pipe must keep its output reviewable")
	}
	m.FeedText("late flush\n")
	waitView(t, &m, "late flush")
}

// TestPipeFinishWithoutCode renders the plain exited marker.
func TestPipeFinishWithoutCode(t *testing.T) {
	m := NewPipe("pipe-nocode", 40, 6, nil)
	t.Cleanup(m.Close)
	m.FinishPipe(0, false)
	waitView(t, &m, "[process exited]")
}

// TestPipeIgnoredOnPTYSessions: FeedText/FinishPipe are pipe-only seams.
func TestPipeIgnoredOnPTYSessions(t *testing.T) {
	m := NewCommand("pipe-vs-pty", []string{"/bin/sh", "-c", "sleep 5"}, t.TempDir(), 40, 6, nil, nil)
	t.Cleanup(m.Close)
	if m.IsPipe() {
		t.Fatal("a PTY command session is not a pipe")
	}
	m.FeedText("must not corrupt the grid\n")
	m.FinishPipe(9, true)
	if strings.Contains(m.View(), "must not corrupt") || strings.Contains(m.View(), "code 9") {
		t.Fatalf("pipe seams must be no-ops on PTY sessions:\n%s", m.View())
	}
}

// TestPipeFinishDoesNotBlockOnSend (#1375): FinishPipe runs on the bubbletea
// Update goroutine (stopDebugSession), and Program.Send blocks until that very
// loop receives — a synchronous repaint send deadlocked the whole UI. The send
// must be async: FinishPipe returns even when nobody is receiving yet, and the
// repaint message still arrives.
func TestPipeFinishDoesNotBlockOnSend(t *testing.T) {
	got := make(chan tea.Msg) // unbuffered, like Program.Send from the Update loop
	m := NewPipe("pipe-freeze", 40, 6, func(msg tea.Msg) { got <- msg })
	t.Cleanup(m.Close)

	done := make(chan struct{})
	go func() {
		m.FinishPipe(3, true)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("FinishPipe blocked on its repaint send (#1375)")
	}
	// The repaint still arrives once the loop is free to receive.
	select {
	case msg := <-got:
		if _, ok := msg.(OutputMsg); !ok {
			t.Fatalf("expected OutputMsg repaint, got %T", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FinishPipe never sent the repaint message")
	}
}

// TestParkedSessionSuppressesOutputMsgs guards #1522: a parked session keeps
// ingesting into its grid but sends no OutputMsg — every send is a full
// program Update pass for a grid nobody renders — and un-parking delivers
// exactly the owed repaint.
func TestParkedSessionSuppressesOutputMsgs(t *testing.T) {
	msgs := make(chan tea.Msg, 64)
	m := NewPipe("parked-test", 40, 6, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(m.Close)
	m.SetParked(true)
	m.FeedText("quiet ingest\n")
	waitView(t, &m, "quiet ingest") // the grid still ingests while parked
	select {
	case msg := <-msgs:
		t.Fatalf("parked session sent %T", msg)
	case <-time.After(60 * time.Millisecond):
	}
	m.SetParked(false)
	select {
	case msg := <-msgs:
		if _, ok := msg.(OutputMsg); !ok {
			t.Fatalf("owed repaint = %T, want OutputMsg", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("un-parking must deliver the owed repaint")
	}
}

// TestParkedWithoutOutputOwesNoRepaint: un-parking a session that stayed
// silent must not synthesize an OutputMsg.
func TestParkedWithoutOutputOwesNoRepaint(t *testing.T) {
	msgs := make(chan tea.Msg, 8)
	m := NewPipe("parked-idle", 40, 6, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(m.Close)
	m.SetParked(true)
	m.SetParked(false)
	select {
	case msg := <-msgs:
		t.Fatalf("idle un-park sent %T", msg)
	case <-time.After(60 * time.Millisecond):
	}
}
