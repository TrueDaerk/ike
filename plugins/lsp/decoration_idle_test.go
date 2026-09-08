package lsp

import (
	"testing"
	"time"

	"ike/internal/host"
)

// decoration_idle_test.go covers the typing-burst rules of #2541 on the
// bridge: the decoration refreshes after a didChange wait for typing to
// pause, cursor moves inside the burst leave the occurrence request to that
// timer, empty replies are not re-delivered, and the completion debounce
// reads lsp.completion_delay_ms.

func (b *bridge) decorationArmed(path string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.decoTimer[path] != nil
}

// TestFlushChangeArmsDecorationIdle: draining a change arms the per-path
// decoration timer instead of firing the follow-ups; a second flush inside
// the window re-arms the same timer.
func TestFlushChangeArmsDecorationIdle(t *testing.T) {
	b := &bridge{}
	b.scheduleChange(host.EditorEvent{Path: "f.go", Text: "x"})
	b.flushChange("f.go")
	if !b.decorationArmed("f.go") {
		t.Fatal("a flushed change must arm the decoration idle timer")
	}
	if !b.typingBurst("f.go") {
		t.Fatal("an armed decoration timer is a typing burst")
	}
	b.scheduleChange(host.EditorEvent{Path: "f.go", Text: "xy"})
	if !b.typingBurst("f.go") {
		t.Fatal("a pending change is a typing burst")
	}
	b.flushChange("f.go")
	b.mu.Lock()
	n := len(b.decoTimer)
	b.mu.Unlock()
	if n != 1 {
		t.Fatalf("decoration timers = %d, want the one re-armed timer", n)
	}
	// The timer fires on its own once typing pauses and clears itself (the
	// requests are nil-guarded no-ops without a manager).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && b.decorationArmed("f.go") {
		time.Sleep(5 * time.Millisecond)
	}
	if b.decorationArmed("f.go") || b.typingBurst("f.go") {
		t.Fatal("the decoration timer never fired")
	}
}

// TestCancelDecorationsAtClose: a close drops the armed timer and the
// empty-reply memory, so a reopen starts fresh.
func TestCancelDecorationsAtClose(t *testing.T) {
	b := &bridge{}
	b.scheduleDecorations("f.go")
	if b.dropEmptyRepeat("semantic", "f.go", true) {
		t.Fatal("the first empty reply must go out")
	}
	b.cancelDecorations("f.go")
	if b.decorationArmed("f.go") {
		t.Fatal("close must drop the decoration timer")
	}
	if b.dropEmptyRepeat("semantic", "f.go", true) {
		t.Fatal("close must forget the empty reply: the reopened editor holds nothing")
	}
}

// TestDropEmptyRepeat: an empty set after an empty set is dropped; a
// non-empty reply always goes out and re-opens the door for one empty.
func TestDropEmptyRepeat(t *testing.T) {
	b := &bridge{}
	if b.dropEmptyRepeat("lenses", "f.go", true) {
		t.Fatal("first empty reply must be delivered (the editor may hold anything)")
	}
	if !b.dropEmptyRepeat("lenses", "f.go", true) {
		t.Fatal("an empty reply after an empty reply changes nothing and must be dropped")
	}
	if b.dropEmptyRepeat("lenses", "f.go", false) {
		t.Fatal("a non-empty reply always goes out")
	}
	if b.dropEmptyRepeat("lenses", "f.go", true) {
		t.Fatal("the empty reply clearing a non-empty set must go out")
	}
	if b.dropEmptyRepeat("lenses", "g.go", true) {
		t.Fatal("the memory is per path")
	}
	if b.dropEmptyRepeat("folds", "f.go", true) {
		t.Fatal("the memory is per kind")
	}
}

// TestCompletionDelayFromConfig: lsp.completion_delay_ms drives the
// identifier-rune debounce; unset or malformed means the default, 0 means
// immediate.
func TestCompletionDelayFromConfig(t *testing.T) {
	mk := func(cfg host.MapConfig) *bridge {
		return &bridge{h: host.New(cfg)}
	}
	if got := mk(host.MapConfig{}).completionDelay(); got != defaultCompletionDelay {
		t.Fatalf("unset delay = %v, want %v", got, defaultCompletionDelay)
	}
	if got := mk(host.MapConfig{"lsp.completion_delay_ms": "250"}).completionDelay(); got != 250*time.Millisecond {
		t.Fatalf("delay = %v, want 250ms", got)
	}
	if got := mk(host.MapConfig{"lsp.completion_delay_ms": "0"}).completionDelay(); got != 0 {
		t.Fatalf("delay 0 = %v, want immediate", got)
	}
	if got := mk(host.MapConfig{"lsp.completion_delay_ms": "later"}).completionDelay(); got != defaultCompletionDelay {
		t.Fatalf("malformed delay = %v, want the default", got)
	}
	if got := (&bridge{}).completionDelay(); got != defaultCompletionDelay {
		t.Fatalf("no host = %v, want the default", got)
	}
}
