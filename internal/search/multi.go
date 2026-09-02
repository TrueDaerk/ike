package search

import (
	"context"
	"fmt"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// multi.go is the all-projects fan-out (#2394): one MultiQuery scans several
// roots with the same pattern and globs, sharing a single result cap. Roots
// are scanned one after another on a background goroutine — each root reuses
// the single-root backends, so it honours its own .gitignore stack — and the
// whole run is cancelled and superseded by the next ScanMulti call, exactly
// like Service. A MultiService is separate from the project Service so the
// open project's find-in-path state is never disturbed.

// MultiQuery describes one all-roots scan. The embedded Query's Root is
// ignored; Roots lists the directories to scan, in order. MaxResults bounds
// the total across all roots (0 selects DefaultMaxResults).
type MultiQuery struct {
	Query
	Roots []string
}

// MultiBatchMsg carries streamed matches from one root of a multi scan.
type MultiBatchMsg struct {
	Gen     int
	Root    string
	Matches []Match
}

// MultiDoneMsg ends a multi scan. Total counts matches across all roots,
// Truncated reports the shared cap stopped the scan early, and Errs maps each
// failing root to its scan error (readable roots that simply had no matches
// are absent). A cancelled scan ends with its stale Gen — consumers drop it.
type MultiDoneMsg struct {
	Gen       int
	Total     int
	Truncated bool
	Errs      map[string]error
}

// MultiService owns multi-root scan lifecycles: one running scan at a time,
// cancelled and superseded by the next ScanMulti call.
type MultiService struct {
	send func(tea.Msg)

	mu     sync.Mutex
	gen    int
	cancel context.CancelFunc

	// forceGo skips the rg backend (tests exercise the fallback on machines
	// that have ripgrep installed).
	forceGo bool
}

// NewMulti returns an idle MultiService reporting through send.
func NewMulti(send func(tea.Msg)) *MultiService {
	return &MultiService{send: send}
}

// ScanMulti cancels any running scan and starts q, returning the new scan's
// generation so the caller can filter incoming messages.
func (s *MultiService) ScanMulti(q MultiQuery) int {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.gen++
	gen := s.gen
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()

	go s.run(ctx, gen, q)
	return gen
}

// Cancel stops the running scan without starting a new one.
func (s *MultiService) Cancel() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.gen++ // invalidate in-flight messages
	s.mu.Unlock()
}

// Gen returns the current (latest) scan generation.
func (s *MultiService) Gen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// run scans the roots in order on one goroutine, so each root's batches
// arrive grouped and the shared cap needs no cross-goroutine budget split.
func (s *MultiService) run(ctx context.Context, gen int, q MultiQuery) {
	c := &multiCollector{service: s, gen: gen, max: q.maxResults()}
	errs := map[string]error{}
	for _, root := range q.Roots {
		if ctx.Err() != nil || c.capped() {
			break
		}
		c.root = root
		if fi, statErr := os.Stat(root); statErr != nil {
			errs[root] = statErr
			continue
		} else if !fi.IsDir() {
			errs[root] = fmt.Errorf("not a directory: %s", root)
			continue
		}
		rq := q.Query
		rq.Root = root
		var err error
		if rg := rgPath(); rg != "" && !s.forceGo {
			err = scanRG(ctx, rg, rq, c)
		} else {
			err = scanGo(ctx, rq, c)
		}
		c.flush()
		if err != nil && ctx.Err() == nil {
			errs[root] = err
		}
	}
	if ctx.Err() != nil {
		errs = nil // superseded/cancelled: the gen is stale anyway
	}
	c.finish(errs)
}

// multiCollector is the multi-root sibling of collector: it enforces the
// shared result bound across roots and stamps every batch with the root it
// came from.
type multiCollector struct {
	service *MultiService
	gen     int
	max     int
	root    string // current root; run() sets it before each scan

	mu        sync.Mutex
	buf       []Match
	total     int
	truncated bool
	done      bool
}

func (c *multiCollector) add(m Match) bool {
	c.mu.Lock()
	if c.done || c.total >= c.max {
		c.truncated = c.truncated || c.total >= c.max
		c.mu.Unlock()
		return false
	}
	c.total++
	c.buf = append(c.buf, m)
	var flush []Match
	if len(c.buf) >= batchSize {
		flush = c.buf
		c.buf = nil
	}
	c.mu.Unlock()
	if flush != nil {
		c.emit(MultiBatchMsg{Gen: c.gen, Root: c.root, Matches: flush})
	}
	return true
}

// capped reports whether the shared bound has been reached.
func (c *multiCollector) capped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total >= c.max
}

// flush emits the buffered tail of the current root, so a batch never mixes
// matches from two roots.
func (c *multiCollector) flush() {
	c.mu.Lock()
	flush := c.buf
	c.buf = nil
	c.mu.Unlock()
	if len(flush) > 0 {
		c.emit(MultiBatchMsg{Gen: c.gen, Root: c.root, Matches: flush})
	}
}

// finish emits the MultiDoneMsg exactly once.
func (c *multiCollector) finish(errs map[string]error) {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return
	}
	c.done = true
	total, truncated := c.total, c.truncated
	c.mu.Unlock()
	c.emit(MultiDoneMsg{Gen: c.gen, Total: total, Truncated: truncated, Errs: errs})
}

func (c *multiCollector) emit(msg tea.Msg) {
	if c.service.send != nil {
		c.service.send(msg)
	}
}
