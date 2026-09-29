package app

// playpage.go is the playground's progressive result (#2796): a run no longer
// collects everything up to the cap before the buffer shows a line. The
// evaluation is a jqplay.Producer that hands out pages — the first one is the
// run's result as far as the reader is concerned, installed the moment it
// arrives — and stays suspended on its goroutine until the reader approaches
// the end of what is loaded. Then the next page is pulled and *appended*:
// the result editor grows in place (editor.AppendReadOnly), the folds,
// value signs and outline extend rather than reset, and the cursor, scroll
// position and search are untouched. `G` walks to the true end, one page at
// a time, until the stream is exhausted or the total budget is spent.
//
// The info row counts what is loaded with a `+` while more is pending
// (`200+ value(s)`) and the exact count once the last page landed; the
// enlarged budget still ends in `(stopped at N)`. Every path that abandons a
// run — a new program, ctrl+l, closing, parking — goes through cancelRun,
// which cancels the run context and with it the producer, so no goroutine
// outlives the result it was feeding.

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	"ike/internal/jqplay"
)

// playPageMsg carries one appended page back to the model.
type playPageMsg struct {
	st   *playState
	gen  int
	page jqplay.Page
	ok   bool
	dur  time.Duration
}

// playPageMargin is how close to the end of the loaded result the viewport
// or cursor must be, in screen heights, before the next page is pulled: one
// screen of look-ahead keeps an ordinary scroll from ever hitting the edge.
const playPageMargin = 1

// playWantsPage reports whether the next page should be pulled now: a
// producer holds more, no page is in flight, and the reader is within
// playPageMargin screens of the loaded end — by viewport or by cursor, so
// wheel scrolling and `G` both count.
func (s *playState) playWantsPage() bool {
	if s == nil || s.producer == nil || s.loading || s.pending || s.resultEd == nil {
		return false
	}
	ed := s.resultEd
	n, h := ed.LineCount(), ed.Height()
	if h <= 0 {
		return false
	}
	line, _ := ed.Cursor()
	edge := n - playPageMargin*h
	return ed.ScrollTop()+h >= edge || line >= edge
}

// playPageCmd pulls the next page when playWantsPage says so; nil otherwise.
// It is called after every key, wheel tick and scrollbar drag in the
// result, and after each page lands, so a reader parked at the end keeps
// receiving pages until the stream is done.
func (m *Model) playPageCmd() tea.Cmd {
	s := m.play
	if !s.playWantsPage() {
		return nil
	}
	line, _ := s.resultEd.Cursor()
	s.toEnd = line >= s.resultEd.LineCount()
	s.loading = true
	ctx, p, gen := s.runCtx, s.producer, s.gen
	return func() tea.Msg {
		start := time.Now()
		pg, ok := p.Next(ctx)
		return playPageMsg{st: s, gen: gen, page: pg, ok: ok, dur: time.Since(start)}
	}
}

// finishPlayPage appends a page to the installed result unless the run it
// belongs to was superseded. The buffer grows in place; folds, value signs
// and the outline extend; the table view re-reads the grown result. A
// cursor that stood on the last line (`G`) follows the new end and asks for
// the next page, so the walk continues until the producer is done.
func (m *Model) finishPlayPage(msg playPageMsg) tea.Cmd {
	s := m.play
	if s == nil || msg.st != s || msg.gen != s.gen || s.producer == nil {
		return nil
	}
	s.loading = false
	if !msg.ok {
		return nil // cancelled: cancelRun already dropped the producer
	}
	n := len(s.result.Outputs)
	s.result.Append(msg.page)
	text := s.result.TextSince(n)
	s.shownText += text
	cmd := s.resultEd.AppendReadOnly(text)
	s.extendResultFolds(n)
	s.setResultValueSigns()
	s.extendOutline()
	if s.tableOn {
		if notice := s.syncPlayTable(); notice != "" {
			s.status, s.statusWarn = notice, true
		}
	}
	if msg.page.Err != "" {
		// A runtime error after the loaded pages ends the result the way it
		// ends a whole run (#2412): the values stand, the row says why the
		// stream stopped.
		s.runErr = msg.page.Err
	}
	if msg.page.Done {
		s.producer = nil
		s.cancelRun()
	}
	if s.toEnd {
		s.resultEd.SetCursor(s.resultEd.LineCount(), 0)
	}
	m.sizePlayResult()
	return tea.Batch(cmd, m.playPageCmd())
}

// extendResultFolds adds the folds of the outputs appended from index n on
// to the result editor's host folds (#2029), keeping the ranges already
// installed — and with them the reader's collapsed state. An xmq result's
// chunks are one document, whose folds are re-scanned whole.
func (s *playState) extendResultFolds(n int) {
	folds := s.result.FoldsSince(n)
	if s.dialect == jqplay.DialectXMQ {
		s.setResultFolds(folds)
		return
	}
	for _, f := range folds {
		s.folds[f.HeaderLine] = f
		s.foldRanges = append(s.foldRanges, highlight.Fold{HeaderLine: f.HeaderLine, EndLine: f.EndLine})
	}
	if s.resultEd != nil && len(folds) > 0 {
		s.resultEd.SetHostFolds(s.foldRanges)
	}
}

// extendOutline re-reads the structure strip's entries (#2793) for the
// grown result, keeping the strip's selection and scroll window where they
// were rather than resetting them the way a new result does.
func (s *playState) extendOutline() {
	sel, top := s.stripSel, s.stripTop
	s.setOutline(s.result.Outline())
	if sel < len(s.outline) {
		s.stripSel, s.stripTop = sel, top
	}
}

// playCountLabel is the info row's value count: the exact count, or the
// loaded count with a `+` while the producer holds more pages.
func (s *playState) playCountLabel() string {
	n := s.result.Count()
	if s.result.Partial() {
		return strconv.Itoa(n) + "+"
	}
	return strconv.Itoa(n)
}
