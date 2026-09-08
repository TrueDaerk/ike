package complete

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// delay_test.go covers the two pass-count rules of #2541: an identifier-rune
// trigger waits Delay for the next keystroke, and one dispatch's batches
// travel as a single CompletionBatchMsg.

// rawEngine records every message the engine sends, unpacked or not.
func rawEngine() (*Engine, func() []tea.Msg) {
	var mu sync.Mutex
	var msgs []tea.Msg
	e := NewEngine(func(msg tea.Msg) {
		mu.Lock()
		msgs = append(msgs, msg)
		mu.Unlock()
	})
	return e, func() []tea.Msg {
		mu.Lock()
		defer mu.Unlock()
		return append([]tea.Msg(nil), msgs...)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never met")
}

// TestDispatchBatchesSourcesIntoOneMessage: two instant sources land as one
// CompletionBatchMsg carrying both tagged batches — one Update+View pass for
// the popup, not one per source.
func TestDispatchBatchesSourcesIntoOneMessage(t *testing.T) {
	e, msgs := rawEngine()
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	e.Register(fakeSource{name: "symbols", prio: ilsp.PrioritySymbols})
	e.Emit(trigger("a"))
	waitFor(t, func() bool { return len(msgs()) >= 1 })
	got := msgs()
	if len(got) != 1 {
		t.Fatalf("messages = %d, want one batch message: %+v", len(got), got)
	}
	batch, ok := got[0].(ilsp.CompletionBatchMsg)
	if !ok || len(batch.Batches) != 2 {
		t.Fatalf("message = %#v, want a CompletionBatchMsg with two batches", got[0])
	}
	seen := map[string]bool{}
	for _, b := range batch.Batches {
		seen[b.Source] = true
		if b.Line != 1 || b.Col != 2 || b.Path != "/f.go" {
			t.Fatalf("batch position = %+v", b)
		}
	}
	if !seen["words"] || !seen["symbols"] {
		t.Fatalf("batch sources = %v", seen)
	}
}

// TestSlowSourceSendsOnItsOwn: a source past the gather window does not hold
// the fast ones back and still arrives, as its own message.
func TestSlowSourceSendsOnItsOwn(t *testing.T) {
	e, msgs := rawEngine()
	e.Register(fakeSource{name: "fast", prio: 1})
	e.Register(fakeSource{name: "slow", prio: 2, delay: 120 * time.Millisecond})
	e.Emit(trigger("a"))
	waitFor(t, func() bool { return len(msgs()) >= 2 })
	got := msgs()
	if _, ok := got[0].(ilsp.CompletionBatchMsg); !ok {
		t.Fatalf("first message = %#v, want the fast batch", got[0])
	}
	if m, ok := got[1].(ilsp.CompletionMsg); !ok || m.Source != "slow" {
		t.Fatalf("second message = %#v, want the slow source alone", got[1])
	}
}

// TestIdentifierDelayCollapsesBurst: with Delay set, a burst of identifier
// runes dispatches once, at the last position; the request never fires
// before the delay.
func TestIdentifierDelayCollapsesBurst(t *testing.T) {
	e, msgs := rawEngine()
	e.Delay = func() time.Duration { return 60 * time.Millisecond }
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	for i, ch := range []string{"a", "b", "c"} {
		e.Emit(host.EditorEvent{Kind: host.EditorCompletionTrigger, Path: "/f.go", Line: 1, Col: 2 + i, Char: ch})
		time.Sleep(10 * time.Millisecond)
	}
	if got := msgs(); len(got) != 0 {
		t.Fatalf("a burst inside the delay must not dispatch, got %+v", got)
	}
	waitFor(t, func() bool { return len(msgs()) >= 1 })
	time.Sleep(80 * time.Millisecond) // nothing else may trail in
	got := msgs()
	if len(got) != 1 {
		t.Fatalf("messages = %d, want exactly one dispatch for the burst", len(got))
	}
	batch := got[0].(ilsp.CompletionBatchMsg)
	if batch.Batches[0].Col != 4 {
		t.Fatalf("dispatch must be at the resting position, got %+v", batch.Batches[0])
	}
}

// TestManualTriggerIgnoresDelay: ctrl+space (empty Char) fires at once and
// cancels an armed identifier-rune wait — the user asked now.
func TestManualTriggerIgnoresDelay(t *testing.T) {
	e, msgs := rawEngine()
	e.Delay = func() time.Duration { return 200 * time.Millisecond }
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	e.Emit(trigger("a"))
	e.Emit(trigger(""))
	waitFor(t, func() bool { return len(msgs()) >= 1 })
	time.Sleep(250 * time.Millisecond)
	if got := msgs(); len(got) != 1 {
		t.Fatalf("messages = %d, want the manual dispatch alone (the armed wait cancelled)", len(got))
	}
}

// TestZeroDelayDispatchesImmediately: Delay nil or 0 keeps the pre-#2541
// behaviour — the request goes out on the keystroke.
func TestZeroDelayDispatchesImmediately(t *testing.T) {
	e, msgs := rawEngine()
	e.Delay = func() time.Duration { return 0 }
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	e.Emit(trigger("a"))
	waitFor(t, func() bool { return len(msgs()) >= 1 })
}
