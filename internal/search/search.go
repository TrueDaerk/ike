// Package search is the project-wide search engine (Roadmap 0150): a
// streaming scanner behind one result shape, backed by `rg --json` when
// ripgrep is on PATH and by a pure-Go walker+matcher otherwise. Scans run off
// the update loop and report matches in batches through the host's Send, so
// first results render while the scan continues; starting a new scan cancels
// the running one via a generation counter — consumers drop messages whose
// generation is not the latest.
package search

import (
	"context"
	"os/exec"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// Query describes one project scan.
type Query struct {
	Pattern string
	Root    string // directory the scan is rooted at
	// CaseSensitive matches exactly; off folds case (rg -i).
	CaseSensitive bool
	// WholeWord matches only at word boundaries (rg -w).
	WholeWord bool
	// Regex treats Pattern as a regular expression; off is a literal search.
	Regex bool
	// Include/Exclude are path globs (rg -g / -g !); Exclude wins on overlap.
	Include []string
	Exclude []string
	// MaxResults bounds the total match count; 0 selects DefaultMaxResults.
	// Hitting the bound stops the scan and flags the result as truncated.
	MaxResults int
}

// DefaultMaxResults bounds a scan when the query does not say otherwise.
const DefaultMaxResults = 2000

func (q Query) maxResults() int {
	if q.MaxResults > 0 {
		return q.MaxResults
	}
	return DefaultMaxResults
}

// Match is one hit: a line of Text in Path with the matched range marked.
// Line is 1-based (editor/status-line convention); StartCol/EndCol are
// 0-based rune offsets into Text (half-open).
type Match struct {
	Path     string
	Line     int
	Text     string
	StartCol int
	EndCol   int
}

// BatchMsg carries a slice of streamed matches. Gen identifies the scan; a
// consumer keeps only the latest generation's batches.
type BatchMsg struct {
	Gen     int
	Matches []Match
}

// DoneMsg ends a scan. Truncated reports that the MaxResults bound stopped it
// early; Err is a scan failure (never "no matches", which is a clean empty
// Done). A cancelled scan (superseded by a newer query) ends with Err == nil
// and its stale Gen — consumers drop it either way.
type DoneMsg struct {
	Gen       int
	Total     int
	Truncated bool
	Err       error
}

// Service owns scan lifecycles: one running scan at a time, cancelled and
// superseded by the next Scan call.
type Service struct {
	send func(tea.Msg)

	mu     sync.Mutex
	gen    int
	cancel context.CancelFunc

	// forceGo skips the rg backend (tests exercise the fallback on machines
	// that have ripgrep installed).
	forceGo bool
}

// New returns an idle Service reporting through send (typically host.Send).
func New(send func(tea.Msg)) *Service {
	return &Service{send: send}
}

// Scan cancels any running scan and starts q, returning the new scan's
// generation so the caller can filter incoming messages.
func (s *Service) Scan(q Query) int {
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
func (s *Service) Cancel() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.gen++ // invalidate in-flight messages
	s.mu.Unlock()
}

// Gen returns the current (latest) scan generation.
func (s *Service) Gen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// run executes one scan on its own goroutine, streaming batches until the
// backend finishes, the result bound is hit, or ctx is cancelled.
func (s *Service) run(ctx context.Context, gen int, q Query) {
	c := &collector{service: s, gen: gen, max: q.maxResults()}
	var err error
	if rg := rgPath(); rg != "" && !s.forceGo {
		err = scanRG(ctx, rg, q, c)
	} else {
		err = scanGo(ctx, q, c)
	}
	if ctx.Err() != nil {
		err = nil // superseded/cancelled: not a failure, and the gen is stale anyway
	}
	c.finish(err)
}

// rgPath locates ripgrep, or "" when unavailable.
func rgPath() string {
	p, err := exec.LookPath("rg")
	if err != nil {
		return ""
	}
	return p
}

// batchSize is how many matches accumulate before a flush; small enough that
// first results appear immediately, large enough to not flood the update loop.
const batchSize = 64

// collector accumulates matches and flushes them as BatchMsgs, enforcing the
// total-result bound across both backends.
type collector struct {
	service *Service
	gen     int
	max     int

	mu        sync.Mutex
	buf       []Match
	total     int
	truncated bool
	done      bool
}

// add records one match, flushing on a full batch. It returns false once the
// result bound is reached, telling the backend to stop scanning.
func (c *collector) add(m Match) bool {
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
		c.emit(BatchMsg{Gen: c.gen, Matches: flush})
	}
	return true
}

// finish flushes the tail and emits the DoneMsg exactly once.
func (c *collector) finish(err error) {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return
	}
	c.done = true
	flush := c.buf
	c.buf = nil
	total, truncated := c.total, c.truncated
	c.mu.Unlock()
	if len(flush) > 0 {
		c.emit(BatchMsg{Gen: c.gen, Matches: flush})
	}
	c.emit(DoneMsg{Gen: c.gen, Total: total, Truncated: truncated, Err: err})
}

func (c *collector) emit(msg tea.Msg) {
	if c.service.send != nil {
		c.service.send(msg)
	}
}
