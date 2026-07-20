package terminal

import "sync"

// spoolMax caps the spool's buffered bytes (soft backpressure threshold): a
// wedged consumer eventually blocks the PTY reader again instead of growing
// without bound. Generous enough to absorb minutes of typical output around a
// lock/sleep window (#734).
const spoolMax = 16 << 20

// spool is an unbounded-within-cap FIFO of byte chunks decoupling the PTY read
// loop from the emulator feed (#734): PTY output is copied in immediately so
// the kernel TTY queue stays drained even while the emulator or render loop
// stalls (app suspend/resume, UI contention). Bytes buffered here are replayed
// into the emulator in order once the consumer runs again — nothing is dropped.
type spool struct {
	mu       sync.Mutex
	nonEmpty sync.Cond // signalled when chunks arrive or the spool closes
	hasRoom  sync.Cond // signalled when buffered bytes drop below spoolMax
	chunks   [][]byte
	size     int
	closed   bool
}

func newSpool() *spool {
	sp := &spool{}
	sp.nonEmpty.L = &sp.mu
	sp.hasRoom.L = &sp.mu
	return sp
}

// put copies p into the spool, blocking only while the cap is exceeded. A
// closed spool discards the write.
func (sp *spool) put(p []byte) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	for sp.size >= spoolMax && !sp.closed {
		sp.hasRoom.Wait()
	}
	if sp.closed {
		return
	}
	c := make([]byte, len(p))
	copy(c, p)
	sp.chunks = append(sp.chunks, c)
	sp.size += len(c)
	sp.nonEmpty.Signal()
}

// take blocks until a chunk is available and returns it; ok is false once the
// spool is closed and fully drained — buffered chunks are always delivered
// before the close is observed.
func (sp *spool) take() (chunk []byte, ok bool) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	for len(sp.chunks) == 0 && !sp.closed {
		sp.nonEmpty.Wait()
	}
	if len(sp.chunks) == 0 {
		return nil, false
	}
	chunk = sp.chunks[0]
	sp.chunks = sp.chunks[1:]
	sp.size -= len(chunk)
	sp.hasRoom.Signal()
	return chunk, true
}

// close wakes both sides; pending chunks still drain through take.
func (sp *spool) close() {
	sp.mu.Lock()
	sp.closed = true
	sp.mu.Unlock()
	sp.nonEmpty.Broadcast()
	sp.hasRoom.Broadcast()
}
