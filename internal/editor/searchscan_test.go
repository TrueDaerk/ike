package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
)

// Tests for the background search landing (#2734): a landing the bounded
// pass cannot settle continues off the loop, its answer lands through
// SearchScanMsg, and Esc / retyping / closing the tab drop it.

// hugeNoMatch is a buffer far past the synchronous scan budget with the
// needle only on its last line, so a forward landing from the top is always
// pending after the first pass.
func hugeNoMatch(t *testing.T) Model {
	t.Helper()
	var sb strings.Builder
	for sb.Len() < 2*search.SyncScanBytes {
		sb.WriteString("plain text line without the word\n")
	}
	sb.WriteString("the needle line\n")
	m, _ := loaded(t, sb.String())
	return m
}

// scanMsgs runs cmd and collects the SearchScanMsgs it (or its batch) yields.
func scanMsgs(cmd tea.Cmd) []SearchScanMsg {
	var out []SearchScanMsg
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case SearchScanMsg:
			out = append(out, msg)
		case tea.BatchMsg:
			for _, sub := range msg {
				walk(sub)
			}
		}
	}
	walk(cmd)
	return out
}

func TestSearchPreviewPastBudgetLandsInBackground(t *testing.T) {
	m := hugeNoMatch(t)
	m = send(m, key('/'), key('n'), key('e'), key('e'), key('d'), key('l'))
	m, cmd := m.Update(key('e'))
	if !m.SearchPending() {
		t.Fatal("a landing past the sync budget must be pending")
	}
	if m.cursor != (buffer.Position{}) {
		t.Fatalf("the cursor parks at the origin while the scan runs, got %v", m.cursor)
	}
	msgs := scanMsgs(cmd)
	if len(msgs) != 1 || !msgs[0].Landing.Found {
		t.Fatalf("the background scan must yield one found landing, got %+v", msgs)
	}
	m, _ = m.Update(msgs[0])
	last := m.buf.LineCount() - 1
	if m.SearchPending() || m.cursor != (buffer.Position{Line: last, Col: 4}) {
		t.Fatalf("the landing must move the preview to the needle (line %d col 4), got %v pending=%v", last, m.cursor, m.SearchPending())
	}
	if m.view.Top > last || m.view.Top+m.view.Height() <= last {
		t.Fatalf("the landing must scroll the match into view, top=%d", m.view.Top)
	}
}

func TestEscCancelsPendingScanAndDropsItsResult(t *testing.T) {
	m := hugeNoMatch(t)
	m = send(m, key('/'), key('n'), key('e'), key('e'), key('d'), key('l'))
	m, cmd := m.Update(key('e'))
	if !m.SearchPending() {
		t.Fatal("setup: the landing must be pending")
	}
	m = send(m, special(tea.KeyEscape))
	if m.SearchPending() {
		t.Fatal("Esc must drop the pending landing")
	}
	// The goroutine sees the bumped generation and answers with silence.
	if msgs := scanMsgs(cmd); len(msgs) != 0 {
		t.Fatalf("a cancelled scan must not produce a landing, got %+v", msgs)
	}
	// A result that had already been produced is dropped by the same check.
	stale := SearchScanMsg{Key: m.ParseKey(), Gen: 1, Version: m.docVersion,
		Landing: search.Landing{Pos: buffer.Position{Line: 5}, Found: true, Done: true}}
	m, _ = m.Update(stale)
	if m.cursor != (buffer.Position{}) {
		t.Fatalf("a stale landing must not move the cursor, got %v", m.cursor)
	}
}

func TestRetypingThePatternSupersedesThePendingScan(t *testing.T) {
	m := hugeNoMatch(t)
	m = send(m, key('/'), key('n'), key('e'), key('e'), key('d'), key('l'))
	m, first := m.Update(key('e'))
	m, second := m.Update(key('X')) // "needleX" matches nowhere
	if !m.SearchPending() {
		t.Fatal("the retyped pattern's landing must be pending")
	}
	if msgs := scanMsgs(first); len(msgs) != 0 {
		t.Fatalf("the superseded scan must yield nothing, got %+v", msgs)
	}
	msgs := scanMsgs(second)
	if len(msgs) != 1 || msgs[0].Landing.Found {
		t.Fatalf("the new scan must report no match, got %+v", msgs)
	}
	m, _ = m.Update(msgs[0])
	if m.SearchPending() || m.cursor != (buffer.Position{}) {
		t.Fatalf("a miss keeps the cursor at the origin, got %v pending=%v", m.cursor, m.SearchPending())
	}
}

func TestCommitPastBudgetJumpsWhenTheLandingArrives(t *testing.T) {
	m := hugeNoMatch(t)
	m = send(m, key('/'), key('n'), key('e'), key('e'), key('d'), key('l'), key('e'))
	m.cancelSearchScan() // pretend the preview never settled
	m, cmd := m.Update(special(tea.KeyEnter))
	if !m.SearchPending() || !m.HasSearch() {
		t.Fatal("Enter past the budget commits the query and waits for the landing")
	}
	msgs := scanMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("one landing expected, got %d", len(msgs))
	}
	m, _ = m.Update(msgs[0])
	last := m.buf.LineCount() - 1
	if m.cursor.Line != last {
		t.Fatalf("the committed landing must jump to the needle line %d, got %v", last, m.cursor)
	}
	// n from there walks the whole buffer round to the same (only) match —
	// past the budget again, so it lands late, with the wrap hint.
	m, cmd = m.Update(key('n'))
	if !m.SearchPending() {
		t.Fatal("n round the whole buffer must be pending")
	}
	msgs = scanMsgs(cmd)
	if len(msgs) != 1 || !msgs[0].Landing.Found {
		t.Fatalf("landing expected, got %+v", msgs)
	}
	m, _ = m.Update(msgs[0])
	if m.SearchPending() || m.cursor.Line != last || m.cmdMsg != "search wrapped" {
		t.Fatalf("n must land on the only match again with the wrap hint, got %v pending=%v msg=%q", m.cursor, m.SearchPending(), m.cmdMsg)
	}
}

func TestRepeatSearchPastBudgetReturnsTheScanCommand(t *testing.T) {
	m := hugeNoMatch(t)
	m.SeedSearch(search.Compile("needle", false, search.CaseExact), search.Forward)
	found, cmd := m.RepeatSearch(false)
	if !found || cmd == nil || !m.SearchPending() {
		t.Fatalf("RepeatSearch past the budget must report pending work with a command (found=%v cmd=%v)", found, cmd != nil)
	}
	msgs := scanMsgs(cmd)
	if len(msgs) != 1 || !msgs[0].Landing.Found {
		t.Fatalf("landing expected, got %+v", msgs)
	}
	m, _ = m.Update(msgs[0])
	if m.cursor.Line != m.buf.LineCount()-1 {
		t.Fatalf("the late landing must move the cursor, got %v", m.cursor)
	}
}

func TestCloseCancelsThePendingScan(t *testing.T) {
	m := hugeNoMatch(t)
	m = send(m, key('/'), key('n'), key('e'), key('e'), key('d'), key('l'))
	m, cmd := m.Update(key('e'))
	m.Close()
	if m.SearchPending() {
		t.Fatal("Close must drop the pending landing")
	}
	if msgs := scanMsgs(cmd); len(msgs) != 0 {
		t.Fatalf("a closed view's scan must yield nothing, got %+v", msgs)
	}
}

func TestEditDuringScanDropsTheLanding(t *testing.T) {
	m := hugeNoMatch(t)
	m.SeedSearch(search.Compile("needle", false, search.CaseExact), search.Forward)
	_, cmd := m.RepeatSearch(false)
	msgs := scanMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("landing expected, got %d", len(msgs))
	}
	m = send(m, key('i'), key('Y'), special(tea.KeyEscape)) // the document moved on
	m, _ = m.Update(msgs[0])
	if m.cursor.Line != 0 {
		t.Fatalf("a landing for an older document version must be dropped, got %v", m.cursor)
	}
}

func TestSmallBufferSearchStaysSynchronous(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta needle\ngamma\n")
	m = send(m, key('/'), key('n'), key('e'))
	if m.SearchPending() || m.cursor != (buffer.Position{Line: 1, Col: 5}) {
		t.Fatalf("a small buffer lands on the spot, got %v pending=%v", m.cursor, m.SearchPending())
	}
}
