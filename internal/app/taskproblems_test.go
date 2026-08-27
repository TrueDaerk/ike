package app

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/matcher"
)

// snapshotSink collects TaskProblemsMsg behind a mutex (flush runs on a timer
// goroutine, #2176) and lets tests wait for the coalesced snapshot.
type snapshotSink struct {
	mu   sync.Mutex
	msgs []TaskProblemsMsg
}

func (s *snapshotSink) send(m tea.Msg) {
	s.mu.Lock()
	s.msgs = append(s.msgs, m.(TaskProblemsMsg))
	s.mu.Unlock()
}

func (s *snapshotSink) wait(t *testing.T, n int) []TaskProblemsMsg {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.msgs) >= n {
			out := append([]TaskProblemsMsg(nil), s.msgs...)
			s.mu.Unlock()
			return out
		}
		s.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Fatalf("timed out waiting for %d snapshots, have %d", n, len(s.msgs))
	return nil
}

// The collector (#1915) converts matcher problems into per-path diagnostics —
// relative paths resolved against the run directory, positions 0-based — and
// publishes coalesced full-set snapshots: matched chunks inside one quiet
// window fold into a single message (#2176) instead of one per chunk.
func TestTaskCollectorFeedPublishesCoalescedSnapshots(t *testing.T) {
	sink := &snapshotSink{}
	gom, _ := matcher.Builtin("go")
	c := &taskCollector{
		eng:    matcher.NewEngine([]matcher.Matcher{gom}),
		dir:    "/proj",
		source: "make: build",
		send:   sink.send,
		byPath: map[string][]ilsp.Diagnostic{},
	}
	c.feed([]byte("plain output\r\n./main.go:5:2: undefined: foo\r\n"))
	c.feed([]byte("no match here\r\n"))
	c.feed([]byte("sub/x.go:9: boom\r\n"))
	msgs := sink.wait(t, 1)
	if len(msgs) != 1 {
		t.Fatalf("want the window's matches coalesced into 1 snapshot, got %d", len(msgs))
	}
	last := msgs[0]
	if last.Source != "make: build" {
		t.Fatalf("source = %q", last.Source)
	}
	ds := last.ByPath["/proj/main.go"]
	if len(ds) != 1 || ds[0].Range.Start.Line != 4 || ds[0].Range.Start.Col != 1 || ds[0].Message != "undefined: foo" || ds[0].Source != "make: build" {
		t.Fatalf("main.go diags = %+v", ds)
	}
	if len(last.ByPath["/proj/sub/x.go"]) != 1 {
		t.Fatalf("sub/x.go missing: %v", last.ByPath)
	}

	// A match after the flush re-arms the window: the next snapshot again
	// carries the full accumulated set.
	c.feed([]byte("sub/y.go:3: pow\r\n"))
	msgs = sink.wait(t, 2)
	next := msgs[len(msgs)-1]
	if len(next.ByPath) != 3 {
		t.Fatalf("re-armed snapshot paths = %d, want the full set 3", len(next.ByPath))
	}
}

// resolveMatchers skips unknown names instead of failing the run.
func TestResolveMatchersBuiltinsAndUnknown(t *testing.T) {
	ms := resolveMatchers([]string{"go", "no-such-matcher", "tsc"})
	if len(ms) != 2 || ms[0].Name() != "go" || ms[1].Name() != "tsc" {
		names := make([]string, len(ms))
		for i, m := range ms {
			names[i] = m.Name()
		}
		t.Fatalf("resolved = %v, want [go tsc]", names)
	}
}
