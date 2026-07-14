package lsp

import (
	"testing"
	"time"

	"ike/internal/host"
)

// pendingCount reports how many paths have a queued change (test-only peek).
func (b *bridge) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pendingChange)
}

func (b *bridge) pendingText(path string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ev, ok := b.pendingChange[path]
	return ev.Text, ok
}

// TestScheduleChangeCoalesces verifies a burst of edits to one path collapses to
// a single pending change holding the latest text (#595) — the whole point of
// the debounce is that N keystrokes do not become N syncs.
func TestScheduleChangeCoalesces(t *testing.T) {
	b := &bridge{}
	for i, text := range []string{"a", "ab", "abc"} {
		b.scheduleChange(host.EditorEvent{Path: "f.go", Text: text})
		if got := b.pendingCount(); got != 1 {
			t.Fatalf("after edit %d: pendingCount = %d, want 1", i, got)
		}
	}
	if txt, ok := b.pendingText("f.go"); !ok || txt != "abc" {
		t.Fatalf("pending text = %q,%v, want \"abc\",true (latest wins)", txt, ok)
	}
}

// TestFlushChangeDrains verifies an explicit flush clears the pending change
// (with no manager the sync + follow-ups are nil-guarded no-ops, so only the
// bookkeeping is observable).
func TestFlushChangeDrains(t *testing.T) {
	b := &bridge{}
	b.scheduleChange(host.EditorEvent{Path: "f.go", Text: "x"})
	b.flushChange("f.go")
	if got := b.pendingCount(); got != 0 {
		t.Fatalf("after flush: pendingCount = %d, want 0", got)
	}
	// A second flush with nothing pending is a safe no-op.
	b.flushChange("f.go")
}

// TestCancelChangeDropsWithoutSync verifies a close-time cancel clears the queue
// so a debounced sync never lands after didClose.
func TestCancelChangeDropsWithoutSync(t *testing.T) {
	b := &bridge{}
	b.scheduleChange(host.EditorEvent{Path: "f.go", Text: "x"})
	b.cancelChange("f.go")
	if got := b.pendingCount(); got != 0 {
		t.Fatalf("after cancel: pendingCount = %d, want 0", got)
	}
}

// TestDebounceTimerFlushes verifies the armed timer drains the pending change on
// its own after the debounce window, without an explicit flush.
func TestDebounceTimerFlushes(t *testing.T) {
	b := &bridge{}
	b.scheduleChange(host.EditorEvent{Path: "f.go", Text: "x"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.pendingCount() == 0 {
			return // the timer fired and drained it
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("debounce timer never flushed the pending change")
}

// TestCurFlushesPendingChange verifies the request choke point (cur) drains a
// queued change, so a request keyed off the cursor never reads stale text.
func TestCurFlushesPendingChange(t *testing.T) {
	b := &bridge{}
	b.setCur("f.go", 1, 2)
	b.scheduleChange(host.EditorEvent{Path: "f.go", Text: "x"})
	if _, _, _ = b.cur(); b.pendingCount() != 0 {
		t.Fatalf("cur() did not flush the pending change: pendingCount = %d", b.pendingCount())
	}
}
