package terminal

import (
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// earlytap_test.go covers #3000's tap replay: a command session retains the
// output replayed before its first SetTap and hands it to that tap, so the
// Run tool's problem-matcher tee and the notebook watch (installed right
// after the spawn) see a print-and-exit child's whole stream even when the
// child was gone before they arrived.

// tapRecorder collects tap chunks under a lock.
type tapRecorder struct {
	mu  sync.Mutex
	got []byte
}

func (r *tapRecorder) tap(b []byte) {
	r.mu.Lock()
	r.got = append(r.got, b...)
	r.mu.Unlock()
}

func (r *tapRecorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.got)
}

// TestSetTapReplaysOutputBeforeTheTap: the tap installed after the child
// exited still receives everything the child printed.
func TestSetTapReplaysOutputBeforeTheTap(t *testing.T) {
	c := &collector{}
	s, err := StartCommandSession("terminal", []string{"/bin/sh", "-c", "echo early-mark; echo early-two"}, t.TempDir(), 80, 24, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	waitFor(t, "exit msg", func() bool {
		return c.has(func(m tea.Msg) bool { _, ok := m.(ExitedMsg); return ok })
	})
	waitFor(t, "run output on the grid", func() bool {
		return strings.Contains(plainView(s), "early-two")
	})
	rec := &tapRecorder{}
	s.SetTap(rec.tap)
	if got := rec.text(); !strings.Contains(got, "early-mark") || !strings.Contains(got, "early-two") {
		t.Fatalf("the tap must receive the output replayed before it was installed, got %q", got)
	}
	// The retention is spent: a second tap starts from the live stream only.
	second := &tapRecorder{}
	s.SetTap(second.tap)
	if got := second.text(); got != "" {
		t.Fatalf("a later tap must not replay the early output again, got %q", got)
	}
}

// TestSetTapRetentionIsBounded: what a run printed past earlyLimit before
// the tap is dropped, the head is kept.
func TestSetTapRetentionIsBounded(t *testing.T) {
	s := &Session{} // a bare session retains like a command session does
	big := strings.Repeat("x", earlyLimit)
	s.tee([]byte("head-mark "), []byte(big), []byte(" tail-mark"))
	rec := &tapRecorder{}
	s.SetTap(rec.tap)
	got := rec.text()
	if !strings.HasPrefix(got, "head-mark ") || len(got) != earlyLimit {
		t.Fatalf("retained %d bytes starting %q, want the head capped at %d", len(got), got[:min(len(got), 10)], earlyLimit)
	}
	if strings.Contains(got, "tail-mark") {
		t.Fatal("output past the cap must be dropped, not the head")
	}
}

// TestShellSessionRetainsNothingForATap: a plain shell is never tapped, so
// its prompt bytes are not kept around.
func TestShellSessionRetainsNothingForATap(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	waitPrompt(t, s)
	rec := &tapRecorder{}
	s.SetTap(rec.tap)
	if got := rec.text(); got != "" {
		t.Fatalf("a shell session must not replay early output, got %q", got)
	}
}
