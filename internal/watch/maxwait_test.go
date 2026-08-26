package watch

import (
	"fmt"
	"testing"
	"time"
)

// TestSustainedChurnStillFlushes (#2163): a writer producing events faster
// than the debounce window used to reset the timer forever — no flush until
// the writer paused, with the pending map growing the whole time. The
// maxWait ceiling forces a flush even while the churn continues.
func TestSustainedChurnStillFlushes(t *testing.T) {
	s, c := service()
	s.maxWait = 30 * time.Millisecond // s.debounce is 10ms
	deadline := time.Now().Add(2 * time.Second)
	i := 0
	for time.Now().Before(deadline) {
		// Distinct paths, arriving faster than the window slides shut.
		s.note(fmt.Sprintf("/p/churn-%d.go", i), FileChanged)
		i++
		if c.count() > 0 {
			return // flushed mid-churn — the ceiling works
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no flush during 2s of sustained churn — the debounce deferred forever")
}
