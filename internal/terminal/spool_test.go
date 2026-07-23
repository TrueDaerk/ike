package terminal

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

// TestSpoolNoByteLossWithStalledConsumer is the #734 regression: a paused
// consumer (the emulator feed during a lock/sleep/resume window) must not
// cause any produced byte to be dropped or reordered — everything buffers and
// replays.
func TestSpoolNoByteLossWithStalledConsumer(t *testing.T) {
	sp := newSpool()
	var want bytes.Buffer
	for i := 0; i < 4096; i++ {
		want.WriteByte(byte(i))
		want.WriteByte(byte(i >> 8))
	}
	produced := want.Bytes()

	var got bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Stall like a suspended render loop, then drain everything.
		time.Sleep(50 * time.Millisecond)
		for {
			chunk, ok := sp.take()
			if !ok {
				return
			}
			got.Write(chunk)
		}
	}()
	for off := 0; off < len(produced); off += 100 {
		end := min(off+100, len(produced))
		sp.put(produced[off:end])
	}
	sp.close()
	wg.Wait()
	if !bytes.Equal(got.Bytes(), produced) {
		t.Fatalf("spool lost or reordered bytes: got %d want %d", got.Len(), len(produced))
	}
}

// TestSpoolDrainsAfterClose: chunks put before close are still delivered;
// take reports done only once drained.
func TestSpoolDrainsAfterClose(t *testing.T) {
	sp := newSpool()
	sp.put([]byte("abc"))
	sp.put([]byte("def"))
	sp.close()
	var got []byte
	for {
		chunk, ok := sp.take()
		if !ok {
			break
		}
		got = append(got, chunk...)
	}
	if string(got) != "abcdef" {
		t.Fatalf("drain after close = %q", got)
	}
}

// TestSpoolPutCopiesChunk: the reader reuses its buffer between reads, so put
// must copy.
func TestSpoolPutCopiesChunk(t *testing.T) {
	sp := newSpool()
	buf := []byte("first")
	sp.put(buf)
	copy(buf, "XXXXX")
	chunk, ok := sp.take()
	if !ok || string(chunk) != "first" {
		t.Fatalf("put must copy, got %q ok=%v", chunk, ok)
	}
}

// TestSpoolCapBackpressure: at the cap, put blocks instead of growing without
// bound, and unblocks as the consumer drains.
func TestSpoolCapBackpressure(t *testing.T) {
	sp := newSpool()
	big := make([]byte, spoolMax)
	sp.put(big) // fills to the cap
	unblocked := make(chan struct{})
	go func() {
		sp.put([]byte("x")) // must block until room frees up
		close(unblocked)
	}()
	select {
	case <-unblocked:
		t.Fatal("put above the cap must block")
	case <-time.After(20 * time.Millisecond):
	}
	if _, ok := sp.take(); !ok {
		t.Fatal("take must return the buffered chunk")
	}
	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("put must unblock once the consumer drains")
	}
	sp.close()
}

// TestSpoolDiscardDropsBacklog (#989): discard empties the buffered backlog
// and reports the dropped byte count; the spool keeps working afterwards.
func TestSpoolDiscardDropsBacklog(t *testing.T) {
	sp := newSpool()
	sp.put([]byte("stale-1"))
	sp.put([]byte("stale-2"))
	if got := sp.discard(); got != len("stale-1")+len("stale-2") {
		t.Fatalf("discard dropped %d bytes, want %d", got, len("stale-1")+len("stale-2"))
	}
	sp.put([]byte("fresh"))
	chunk, ok := sp.take()
	if !ok || string(chunk) != "fresh" {
		t.Fatalf("take after discard = %q ok=%v, want fresh", chunk, ok)
	}
	if got := sp.discard(); got != 0 {
		t.Fatalf("discard on empty spool = %d, want 0", got)
	}
	sp.close()
}

// TestSpoolDiscardUnblocksBlockedPut (#989): dropping the backlog frees the
// cap, so a producer blocked on a full spool resumes and its chunk lands.
func TestSpoolDiscardUnblocksBlockedPut(t *testing.T) {
	sp := newSpool()
	sp.put(make([]byte, spoolMax))
	released := make(chan struct{})
	go func() {
		sp.put([]byte("after"))
		close(released)
	}()
	time.Sleep(10 * time.Millisecond)
	sp.discard()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("discard must unblock a blocked put")
	}
	chunk, ok := sp.take()
	if !ok || string(chunk) != "after" {
		t.Fatalf("post-discard chunk = %q ok=%v, want after", chunk, ok)
	}
	sp.close()
}

// TestSpoolCloseUnblocksBlockedPut: teardown must release a reader stuck on a
// full spool.
func TestSpoolCloseUnblocksBlockedPut(t *testing.T) {
	sp := newSpool()
	sp.put(make([]byte, spoolMax))
	released := make(chan struct{})
	go func() {
		sp.put([]byte("x"))
		close(released)
	}()
	time.Sleep(10 * time.Millisecond)
	sp.close()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("close must unblock a blocked put")
	}
}
