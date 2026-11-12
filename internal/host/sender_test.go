package host

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestSetSenderIsSafeWhileWorkersSend guards #3135: a worker started before
// the sender is (re)wired may already be inside Send, so wiring it must not
// race the read (the race detector is the assertion), and a message sent
// after the wiring arrives.
func TestSetSenderIsSafeWhileWorkersSend(t *testing.T) {
	h := New(MapConfig{})
	got := make(chan tea.Msg, 1024)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					h.Send("early")
				}
			}
		}()
	}
	h.SetSender(func(msg tea.Msg) {
		select {
		case got <- msg:
		default:
		}
	})
	close(stop)
	wg.Wait()
	// Resent until it lands: the outbox is bounded, and the workers' backlog
	// may still fill it when the first attempt is made. The deadline covers
	// one hop through the dispatcher goroutine.
	deadline := time.After(2 * time.Second)
	for {
		h.Send("late")
		select {
		case msg := <-got:
			if msg == "late" {
				return
			}
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			t.Fatal("a message sent after SetSender never arrived")
		}
	}
}
