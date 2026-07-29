package terminal

import (
	"strings"
	"testing"
	"time"
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
