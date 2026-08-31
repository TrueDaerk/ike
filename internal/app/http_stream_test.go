package app

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestHTTPStreamPumpDrainsProducer pins the #2348 freeze-audit invariant: the
// update loop's event pump reads exactly one next event per stream message it
// processes, all the way to the finalizing HTTPResponseMsg — so a producer
// pushing far more chunks than the channel buffers is never left blocked on a
// send (the wedge the audit looked for). A break in the chain (a handler that
// stops returning nextHTTPEvent) fails this test by leaving the producer
// goroutine stuck.
func TestHTTPStreamPumpDrainsProducer(t *testing.T) {
	m := httpApp(t)
	events := make(chan tea.Msg, 4) // far smaller than the chunk count: backpressure is real
	done := make(chan struct{})
	go func() { // shaped like dispatchHTTP's producer goroutine
		defer close(done)
		events <- HTTPStreamStartMsg{Source: "a.http", Request: "k",
			Status: "200 OK", Proto: "HTTP/1.1", events: events}
		for i := 0; i < 64; i++ {
			events <- HTTPStreamChunkMsg{Source: "a.http", Request: "k",
				Chunk: []byte("data\n"), events: events}
		}
		events <- HTTPResponseMsg{Source: "a.http", Request: "k", Resp: sampleResponse("k")}
		close(events)
	}()

	msg := <-events // what the dispatch command's own first read returns
	for {
		tm, cmd := m.updateMsg(msg)
		m = tm.(Model)
		if _, final := msg.(HTTPResponseMsg); final {
			break // the chain ends here by design; the channel is closed
		}
		if cmd == nil {
			t.Fatalf("pump chain broke: no follow-up read after %T", msg)
		}
		msg = cmd() // nextHTTPEvent: the one read this message re-armed
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("producer goroutine left blocked — the pump did not drain the channel")
	}
}

// TestHTTPChunkCoalescerBatchesPerWindow (#2176): chunks inside one quiet
// window fold into a single emitted buffer instead of one message per chunk.
func TestHTTPChunkCoalescerBatchesPerWindow(t *testing.T) {
	var mu sync.Mutex
	var got [][]byte
	c := &httpChunkCoalescer{emit: func(b []byte) { mu.Lock(); got = append(got, b); mu.Unlock() }}
	c.add([]byte("data: 1\n"))
	c.add([]byte("data: 2\n"))
	c.add([]byte("data: 3\n"))
	mu.Lock()
	if len(got) != 0 {
		mu.Unlock()
		t.Fatalf("chunks emitted before the window expired: %d", len(got))
	}
	mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("window = %d emissions, want 1 coalesced buffer", len(got))
	}
	if string(got[0]) != "data: 1\ndata: 2\ndata: 3\n" {
		t.Fatalf("coalesced buffer = %q", got[0])
	}
}

// TestHTTPChunkCoalescerFinishFlushesTailAndStops (#2176): finish delivers
// the buffered tail synchronously — ahead of the final HTTPResponseMsg the
// dispatch goroutine sends next — and a late add or timer emits nothing, so
// no send can land after the events channel closes.
func TestHTTPChunkCoalescerFinishFlushesTailAndStops(t *testing.T) {
	var mu sync.Mutex
	var got [][]byte
	c := &httpChunkCoalescer{emit: func(b []byte) { mu.Lock(); got = append(got, b); mu.Unlock() }}
	c.add([]byte("tail"))
	c.finish()
	mu.Lock()
	if len(got) != 1 || string(got[0]) != "tail" {
		mu.Unlock()
		t.Fatalf("finish did not flush the tail synchronously: %v", got)
	}
	mu.Unlock()
	c.add([]byte("late"))
	c.flush() // the armed timer firing after finish
	time.Sleep(3 * httpStreamQuiet)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("emissions after finish: %d, want none", len(got)-1)
	}
}
