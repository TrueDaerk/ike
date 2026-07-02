package transport

import "sync"

// ringBuffer is a bounded, concurrency-safe sink for a server's stderr: it keeps
// only the last `size` bytes so a chatty server cannot grow memory without bound,
// while still surfacing the tail (where crash messages land) on demand.
type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newRingBuffer(size int) *ringBuffer { return &ringBuffer{size: size} }

// Write implements io.Writer, retaining only the trailing `size` bytes.
func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.size {
		r.buf = r.buf[len(r.buf)-r.size:]
	}
	return len(p), nil
}

// String returns the retained tail.
func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
