package watch

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// collector gathers sent messages behind a mutex (flush runs on a timer
// goroutine) and lets tests wait for a batch.
type collector struct {
	mu   sync.Mutex
	msgs []EventMsg
}

func (c *collector) send(msg tea.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ev, ok := msg.(EventMsg); ok {
		c.msgs = append(c.msgs, ev)
	}
}

func (c *collector) wait(t *testing.T, n int) []EventMsg {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.msgs) >= n {
			out := append([]EventMsg(nil), c.msgs...)
			c.mu.Unlock()
			return out
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("timed out waiting for %d events, have %v", n, c.msgs)
	return nil
}

func (c *collector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.msgs)
}

// service returns a fast-debounce Service wired to a collector, bypassing
// fsnotify: tests feed raw events through note() like ingest would.
func service() (*Service, *collector) {
	c := &collector{}
	s := New(c.send)
	s.debounce = 10 * time.Millisecond
	return s, c
}

func TestDebounceCoalescesBursts(t *testing.T) {
	s, c := service()
	for i := 0; i < 5; i++ {
		s.note("/p/a.go", FileChanged)
	}
	got := c.wait(t, 1)
	if len(got) != 1 || got[0].Kind != FileChanged {
		t.Fatalf("5 writes must coalesce to one FileChanged, got %v", got)
	}
}

func TestMergeCreateThenWriteStaysCreated(t *testing.T) {
	s, c := service()
	s.note("/p/new.go", FileCreated)
	s.note("/p/new.go", FileChanged)
	s.note("/p/gone.go", FileChanged)
	s.note("/p/gone.go", FileRemoved)
	got := c.wait(t, 2)
	kinds := map[string]Kind{}
	for _, ev := range got {
		kinds[filepath.Base(ev.Path)] = ev.Kind
	}
	if kinds["new.go"] != FileCreated {
		t.Fatalf("create+write must stay FileCreated, got %v", kinds["new.go"])
	}
	if kinds["gone.go"] != FileRemoved {
		t.Fatalf("write+remove must end FileRemoved, got %v", kinds["gone.go"])
	}
}

func TestSelfEventSuppression(t *testing.T) {
	s, c := service()
	s.MarkSaved("/p/mine.go")
	s.note("/p/mine.go", FileChanged)
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("own save must be suppressed, got %d events", n)
	}
	// Outside the suppression window the same path reports again.
	s.now = func() time.Time { return time.Now().Add(suppressWindow + time.Second) }
	s.note("/p/mine.go", FileChanged)
	if got := c.wait(t, 1); got[0].Kind != FileChanged {
		t.Fatalf("post-window change must report, got %v", got)
	}
}

func TestFsnotifyEndToEnd(t *testing.T) {
	dir := t.TempDir()
	s, c := service()
	if err := s.Start(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	found := false
	for _, ev := range got {
		if ev.Path == path && (ev.Kind == FileCreated || ev.Kind == FileChanged) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a create/change for %s, got %v", path, got)
	}
}

func TestPollDetectsChangeAndIgnoresTouch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(path, []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	s.Track(path)

	// A bare touch (new mtime, same size + content) must not report.
	if err := os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.Poll()
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("touch must not report, got %d events", n)
	}

	// A real same-size edit is caught by the hash-on-suspicion path.
	if err := os.WriteFile(path, []byte("bbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Poll()
	got := c.wait(t, 1)
	if got[0].Kind != FileChanged || got[0].Path != path {
		t.Fatalf("same-size edit must report FileChanged, got %v", got)
	}

	// Removal reports and drops tracking.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.Poll()
	got = c.wait(t, 2)
	if got[len(got)-1].Kind != FileRemoved {
		t.Fatalf("removal must report FileRemoved, got %v", got)
	}
}

func TestMarkSavedRefreshesPollStamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	s.Track(path)
	// IKE writes the file itself and stamps the epoch: Poll stays silent.
	if err := os.WriteFile(path, []byte("v2!"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.MarkSaved(path)
	s.Poll()
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("own save must not poll-report, got %d events", n)
	}
}
