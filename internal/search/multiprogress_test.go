package search

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestMultiScanReportsProgressPerRoot: every root reports exactly once when it
// is through, in scan order and counted against the root total (#2413) — the
// numbers the status-line progress segment shows. A root without matches emits
// no batch, so the progress messages are the only source for the counter.
func TestMultiScanReportsProgressPerRoot(t *testing.T) {
	roots := multiFixture(t, 3)
	var mu sync.Mutex
	var prog []MultiProgressMsg
	done := make(chan MultiDoneMsg, 1)
	s := NewMulti(func(msg tea.Msg) {
		switch m := msg.(type) {
		case MultiProgressMsg:
			mu.Lock()
			prog = append(prog, m)
			mu.Unlock()
		case MultiDoneMsg:
			done <- m
		}
	})
	s.forceGo = true
	gen := s.ScanMulti(MultiQuery{Query: Query{Pattern: "needle"}, Roots: roots})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("multi scan did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prog) != len(roots) {
		t.Fatalf("got %d progress messages, want %d", len(prog), len(roots))
	}
	for i, p := range prog {
		if p.Gen != gen || p.Root != roots[i] || p.Done != i+1 || p.Total != len(roots) {
			t.Fatalf("progress[%d] = %+v, want gen %d root %s done %d/%d",
				i, p, gen, roots[i], i+1, len(roots))
		}
	}
}

// TestMultiScanReportsProgressForFailingRoot: a root that cannot be scanned
// still counts towards the progress — otherwise the counter would stall on a
// stale project in the history.
func TestMultiScanReportsProgressForFailingRoot(t *testing.T) {
	roots := append(multiFixture(t, 1), "/definitely/not/a/directory")
	var mu sync.Mutex
	var prog []MultiProgressMsg
	done := make(chan MultiDoneMsg, 1)
	s := NewMulti(func(msg tea.Msg) {
		switch m := msg.(type) {
		case MultiProgressMsg:
			mu.Lock()
			prog = append(prog, m)
			mu.Unlock()
		case MultiDoneMsg:
			done <- m
		}
	})
	s.forceGo = true
	s.ScanMulti(MultiQuery{Query: Query{Pattern: "needle"}, Roots: roots})
	select {
	case d := <-done:
		if len(d.Errs) != 1 {
			t.Fatalf("errs = %v, want the missing root to fail alone", d.Errs)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("multi scan did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prog) != 2 || prog[1].Done != 2 {
		t.Fatalf("progress = %+v, want both roots counted", prog)
	}
}
