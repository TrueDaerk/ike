package app

import (
	"sync"
	"testing"
	"time"
)

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
