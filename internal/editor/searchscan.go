package editor

import (
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
)

// searchscan.go keeps the in-file search off the update loop on huge buffers
// (#2734). Every landing — the "/" preview per keystroke, Enter, n/N, "*"/"#"
// and the cmd+g step on the open line — goes through searchLand: one bounded
// pass (search.SyncScanBytes of line text) runs synchronously, which answers
// nearly every search on the spot, and whatever the pass could not decide
// continues on a goroutine over a snapshot of the buffer. Its answer comes
// back as a SearchScanMsg the app routes by ParseKey, and lands exactly as the
// synchronous answer would have.
//
// Cancellation is a generation counter behind a pointer the value copies of a
// Model share (like the tally store): starting another landing, Esc on the
// search line, retyping the pattern, clearing the search, installing other
// content into the view, and closing the tab all bump it. The goroutine checks
// the generation between slices and returns a nil message — which bubbletea
// drops before Update — once it is stale; a result that still arrives is
// dropped by the same check on the loop, so no stale landing is ever applied.

// searchScanStore is the shared cancellation state.
type searchScanStore struct{ gen atomic.Int64 }

func newSearchScan() *searchScanStore { return &searchScanStore{} }

// searchPurpose says what a landing is for, which decides how its result
// applies when it arrives late.
type searchPurpose int

const (
	scanPreview searchPurpose = iota // the "/" line's incremental preview
	scanCommit                       // Enter on the "/" line
	scanRepeat                       // n/N, "*"/"#", RepeatSearch
	scanStep                         // cmd+g / cmd+shift+g on the open line
)

// pendingSearch describes the background landing the view is waiting for.
type pendingSearch struct {
	gen     int64
	purpose searchPurpose
	query   search.Query
	dir     search.Direction
	from    buffer.Position // the departure, for the wrap hint
}

// SearchScanMsg is a background landing's answer, routed to the view by Key
// (its ParseKey).
type SearchScanMsg struct {
	Key     string
	Gen     int64
	Version int // document version the scan ran against
	Landing search.Landing
}

// searchLand finds the count-th match from `from` in dir for purpose. It
// returns the landing when the bounded pass settled it (found or not). Pending
// reports that the pass ran out of budget: the rest of the scan is running in
// the background, and its result arrives as a SearchScanMsg carried by the
// command searchCmd parks (drained by takeSearchCmd). Any earlier pending scan
// is cancelled — a new landing supersedes it.
func (m *Model) searchLand(q search.Query, from buffer.Position, dir search.Direction, count int, purpose searchPurpose) (p buffer.Position, found, pending bool) {
	m.cancelSearchScan()
	budget := search.SyncScanBytes
	if q.IsStructural() {
		// A structural query's matches are cached spans (#2363) evaluated on
		// the loop under their own timeout; the walk over them is cheap, and
		// the cache is not for sharing with a goroutine.
		budget = 0
	}
	l := q.Step(m.buf, q.Begin(from, dir, count), budget)
	if l.Done {
		return l.Pos, l.Found, false
	}
	if m.scan == nil {
		m.scan = newSearchScan()
	}
	gen := m.scan.gen.Add(1)
	m.searchPending = &pendingSearch{gen: gen, purpose: purpose, query: q, dir: dir, from: from}
	key, version, store := m.ParseKey(), m.docVersion, m.scan
	// The loop may edit the buffer while the scan runs: scan a snapshot (the
	// line slice copies; the strings are shared and immutable).
	snapshot := buffer.New(m.buf.Lines())
	scan := l.Scan
	m.searchCmd = func() tea.Msg {
		for {
			if store.gen.Load() != gen {
				return nil // superseded or cancelled: silence, not a stale landing
			}
			l := q.Step(snapshot, scan, search.AsyncScanBytes)
			if l.Done {
				return SearchScanMsg{Key: key, Gen: gen, Version: version, Landing: l}
			}
			scan = l.Scan
		}
	}
	return from, false, true
}

// cancelSearchScan drops the pending background landing, if any: the
// goroutine stops at its next slice and its result, should it still arrive,
// is ignored.
func (m *Model) cancelSearchScan() {
	if m.searchPending == nil {
		return
	}
	m.searchPending = nil
	m.searchCmd = nil
	if m.scan != nil {
		m.scan.gen.Add(1)
	}
}

// takeSearchCmd drains the command carrying a background landing, the way
// takeDepSignal drains the dependency prompt: the key and action paths batch
// it into their returned command.
func (m *Model) takeSearchCmd() tea.Cmd {
	c := m.searchCmd
	m.searchCmd = nil
	return c
}

// SearchPending reports whether a landing is still being searched for in the
// background — the status line shows it (#2734), so a long scan on a huge
// file is visible as work in progress rather than a search that found
// nothing.
func (m Model) SearchPending() bool { return m.searchPending != nil }

// Close releases what the view holds outside the update loop (#2734): a
// background search scan stops, so a closed tab never keeps a goroutine
// walking its buffer. The pane calls it when the tab closes.
func (m *Model) Close() { m.cancelSearchScan() }

// applySearchScan lands a background scan's answer — only the one the view
// is waiting for: the generation, the route key and the document version
// must all still match, or the answer describes text or a search the user
// has since left behind.
func (m Model) applySearchScan(msg SearchScanMsg) (Model, tea.Cmd) {
	p := m.searchPending
	if p == nil || msg.Gen != p.gen || msg.Key != m.ParseKey() || msg.Version != m.docVersion {
		return m, nil
	}
	m.searchPending = nil
	if m.cmdMsg == searchingMsg {
		m.cmdMsg = ""
	}
	l := msg.Landing
	switch p.purpose {
	case scanPreview:
		if !m.searching || m.preview.ID() != p.query.ID() {
			return m, nil
		}
		if l.Found {
			m.cursor = l.Pos
			m.desiredCol = l.Pos.Col
			m.landOnMatch(m.preview)
			m.view.Left = m.searchOrigLft
			m.scroll()
		}
	case scanStep:
		if !m.searching || m.preview.ID() != p.query.ID() || !l.Found {
			return m, nil
		}
		if wrapped(p.from, l.Pos, p.dir) {
			m.cmdMsg = "search wrapped"
		}
		m.searchStepped = true
		m.cursor = l.Pos
		m.desiredCol = l.Pos.Col
		m.landOnMatch(m.preview)
		m.scroll()
	case scanCommit, scanRepeat:
		if m.query.ID() != p.query.ID() {
			return m, nil
		}
		if !l.Found {
			m.cmdMsg = "no matches: " + m.query.Pattern
			if p.purpose == scanCommit {
				m.hlActive = false
				m.restoreSearchOrigin()
			}
			return m, nil
		}
		if wrapped(p.from, l.Pos, p.dir) {
			m.cmdMsg = "search wrapped"
		}
		m.hlActive = true
		if p.purpose == scanCommit {
			m.cursor = p.from // the jump departs from the origin (commitSearch)
		}
		m.jumpTo(l.Pos)
		m.landOnMatch(m.query)
		m.scroll()
	}
	return m, nil
}

// searchingMsg is the ex-line hint while a landing runs in the background.
const searchingMsg = "searching…"
