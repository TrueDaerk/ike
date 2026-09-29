package app

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
)

// playpage_test.go covers the progressive result (#2796): the first page is
// installed at once and counted with a `+`, `G` walks the rest in, the
// folds and the search cover the appended pages, a program change or a
// close ends the producer without a leak, and the enlarged budget still
// reports its cap.

// bigJSONArray is a JSON array of n small objects `{"i": k}`.
func bigJSONArray(n int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"i":%d}`, i)
	}
	b.WriteString("]")
	return b.String()
}

// pagedPlay opens the playground over a 5,000-element array and runs
// program: the first page is on screen, the producer holds the rest.
func pagedPlay(t *testing.T, program string) Model {
	t.Helper()
	m := playNoOnboarding(openJQ(t, playApp(t, bigJSONArray(5000))))
	m = setProgram(m, program)
	s := m.play
	if s.producer == nil || !s.result.Partial() {
		t.Fatalf("a 5,000-value result must start paged: producer=%v partial=%v", s.producer != nil, s.result.Partial())
	}
	return m
}

// walkToEnd presses `G` in the result buffer, which pulls page after page
// until the producer is done (the test driver runs every command inline).
func walkToEnd(m Model) Model {
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	return playKeys(m, "G")
}

// TestPlayPagesFirstPageThenEnd is the issue's acceptance case: `.[]` over
// 5,000 elements shows the first page at once, the info row says `200+`
// until the pages are exhausted, and `G` reaches the true end with the
// exact count.
func TestPlayPagesFirstPageThenEnd(t *testing.T) {
	m := pagedPlay(t, ".[] | .i")
	s := m.play
	if n := len(s.result.Outputs); n != jqplay.PageOutputs {
		t.Fatalf("first page holds %d outputs, want %d", n, jqplay.PageOutputs)
	}
	if row := ansi.Strip(m.playInfoRow(120)); !strings.Contains(row, "200+ value(s)") {
		t.Fatalf("the info row must count the loaded page with a +, got %q", row)
	}
	if got := s.resultEd.LineCount(); got != jqplay.PageOutputs {
		t.Fatalf("the buffer holds %d lines, want the first page's %d", got, jqplay.PageOutputs)
	}
	m = walkToEnd(m)
	s = m.play
	if s.producer != nil || s.result.Partial() || s.loading {
		t.Fatalf("after G: producer=%v partial=%v loading=%v", s.producer != nil, s.result.Partial(), s.loading)
	}
	if n := s.result.Count(); n != 5000 {
		t.Fatalf("count = %d, want 5000", n)
	}
	if got := s.resultEd.LineCount(); got != 5000 {
		t.Fatalf("the buffer holds %d lines, want 5000", got)
	}
	if line, _ := s.resultEd.Cursor(); line != 5000 {
		t.Errorf("G must follow the new end, cursor on line %d", line)
	}
	if !strings.HasSuffix(s.resultEd.Text(), "\n4999") {
		t.Error("the buffer's tail is not the last value")
	}
	row := ansi.Strip(m.playInfoRow(120))
	if !strings.Contains(row, "5000 value(s)") || strings.Contains(row, "+") || strings.Contains(row, "stopped") {
		t.Fatalf("the info row must say the exact count once exhausted, got %q", row)
	}
	if len(s.valueStarts) != 5000 {
		t.Errorf("value starts = %d, want 5000", len(s.valueStarts))
	}
	if s.shownText != s.result.Text() {
		t.Error("shownText (the next diff's base) must be the whole loaded text")
	}
}

// TestPlayPagesScrollPullsAhead: an ordinary scroll pulls the next page
// before the reader reaches the edge — one page, not the whole stream.
func TestPlayPagesScrollPullsAhead(t *testing.T) {
	m := pagedPlay(t, ".[] | .i")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	s := m.play
	// Well inside the first page: nothing is pulled.
	m = playKeys(m, "50j")
	if len(s.result.Outputs) != jqplay.PageOutputs {
		t.Fatalf("a motion far from the end pulled a page: %d outputs", len(s.result.Outputs))
	}
	// Within a screen of the loaded end: one page more.
	h := s.resultEd.Height()
	m = playKeys(m, fmt.Sprintf("%dj", jqplay.PageOutputs-50-h))
	if got := len(s.result.Outputs); got != 2*jqplay.PageOutputs {
		t.Fatalf("a motion near the end must pull exactly one page: %d outputs", got)
	}
	if !s.result.Partial() || s.producer == nil {
		t.Error("the producer must still hold the rest")
	}
	if line, _ := s.resultEd.Cursor(); line != jqplay.PageOutputs-h+1 {
		t.Errorf("an ordinary scroll must not move the cursor: line %d", line)
	}
}

// TestPlayPagesFoldsAndSearchCoverAppendedPages: the folds of a later page
// are installed when it lands, a fold in it collapses, and the search finds
// a value that was not loaded when the first page came.
func TestPlayPagesFoldsAndSearchCoverAppendedPages(t *testing.T) {
	m := pagedPlay(t, ".[] | {v: .i, a: [.i, 1]}")
	s := m.play
	firstFolds := len(s.folds)
	if firstFolds == 0 {
		t.Fatal("the first page's objects must fold")
	}
	m = walkToEnd(m)
	s = m.play
	if len(s.folds) <= firstFolds {
		t.Fatalf("folds were not extended: %d before, %d after", firstFolds, len(s.folds))
	}
	last := s.valueStarts[4999]
	if _, ok := s.folds[last]; !ok {
		t.Fatalf("the last value's object (line %d) has no fold", last)
	}
	// The fold works: collapse the last value from its header line.
	s.resultEd.SetCursor(last, 0)
	m = playKeys(m, "za")
	if view := ansi.Strip(m.render()); !strings.Contains(view, "⋯ 2 keys }") {
		t.Fatalf("the appended page's fold must collapse, got:\n%s", view)
	}
	m = playKeys(m, "zagg")
	// The search finds a value on the last page.
	m = drainKey(m, playFindKey())
	m = playSearchFor(m, `"v": 4999`)
	// gojq writes keys sorted, so `v` is the object's last member: header line
	// (0-based) + 6, as a 1-based line.
	if line, _ := s.resultEd.Cursor(); line != last+6 {
		t.Fatalf("the search landed on line %d, want %d (the last value's v)", line, last+6)
	}
}

// TestPlayPagesProgramChangeStopsProducer is the leak check: a program
// change while pages are pending cancels the producer, and the goroutines
// it held are gone.
func TestPlayPagesProgramChangeStopsProducer(t *testing.T) {
	m := openJQ(t, playApp(t, bigJSONArray(5000)))
	m = setProgram(m, ".[0]")
	waitGoroutines(t, runtime.NumGoroutine()) // settle
	base := runtime.NumGoroutine()
	m = setProgram(m, ".[] | .i")
	s := m.play
	if s.producer == nil {
		t.Fatal("the run must be paged")
	}
	if runtime.NumGoroutine() <= base {
		t.Fatal("a paged run must hold its producer goroutine")
	}
	m = setProgram(m, ".[0]")
	if s.producer != nil || s.runCtx != nil || s.cancel != nil {
		t.Fatalf("the program change must drop the producer: producer=%v ctx=%v cancel=%v", s.producer != nil, s.runCtx != nil, s.cancel != nil)
	}
	waitGoroutines(t, base)
	// The same on close.
	m = setProgram(m, ".[] | .i")
	if m.play.producer == nil {
		t.Fatal("the run must be paged")
	}
	m.closePlayground()
	waitGoroutines(t, base)
}

// waitGoroutines waits for the goroutine count to fall back to at most n.
func waitGoroutines(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	buf := make([]byte, 1<<16)
	buf = buf[:runtime.Stack(buf, true)]
	t.Fatalf("%d goroutines still alive, want at most %d:\n%s", runtime.NumGoroutine(), n, buf)
}

// TestPlayPagesCapReportsStoppedAt: the enlarged total budget still ends
// in the cap message once every page is in, and the count is the budget.
func TestPlayPagesCapReportsStoppedAt(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, "null")))
	m = setProgram(m, "range(infinite)")
	s := m.play
	if !s.result.Partial() || s.result.Truncated {
		t.Fatalf("the first page must be partial and not yet capped: partial=%v truncated=%v", s.result.Partial(), s.result.Truncated)
	}
	m = walkToEnd(m)
	s = m.play
	if !s.result.Truncated || s.result.Partial() || s.result.Count() != jqplay.MaxOutputs {
		t.Fatalf("after G: truncated=%v partial=%v count=%d", s.result.Truncated, s.result.Partial(), s.result.Count())
	}
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)
	if row := ansi.Strip(m.playInfoRow(120)); !strings.Contains(row, fmt.Sprintf("(stopped at %d)", jqplay.MaxOutputs)) {
		t.Fatalf("the cap must be reported, got %q", row)
	}
}

// TestPlayPagesErrorAfterPages: a runtime error on a later page keeps the
// loaded values and puts the error on the info row, like a failed run.
func TestPlayPagesErrorAfterPages(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, bigJSONArray(300)+"\n"+`{"i":"x"}`)))
	m = setProgram(m, ".[]? | .i + 1")
	s := m.play
	if !s.result.Partial() {
		t.Fatal("300 values page")
	}
	m = walkToEnd(m)
	s = m.play
	if s.result.Count() != 300 || s.runErr == "" || s.producer != nil {
		t.Fatalf("count=%d runErr=%q producer=%v", s.result.Count(), s.runErr, s.producer != nil)
	}
}

// TestPlayPagesParkRerunsOnResume: a playground parked with pages pending
// re-runs on resume (its producer was bound to the old model's run) rather
// than resuming a stream that is gone.
func TestPlayPagesParkRerunsOnResume(t *testing.T) {
	m := pagedPlay(t, ".[] | .i")
	s := m.parkPlayground()
	if s.producer != nil || !s.pending {
		t.Fatalf("park must drop the producer and mark the run pending: producer=%v pending=%v", s.producer != nil, s.pending)
	}
	m.resumePlayground(s, m.pal(), m.host.Config())
	m = drainCmd(m, m.resumePlayRun())
	if m.play.producer == nil || m.play.pending || len(m.play.result.Outputs) != jqplay.PageOutputs {
		t.Fatalf("resume must re-run into a fresh paged result: producer=%v pending=%v n=%d", m.play.producer != nil, m.play.pending, len(m.play.result.Outputs))
	}
	m.closePlayground()
}

// TestPlayPagesClearStopsProducer: ctrl+l in the result drops the pages
// still pending with the output it clears.
func TestPlayPagesClearStopsProducer(t *testing.T) {
	m := pagedPlay(t, ".[] | .i")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainKey(m, tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	if s := m.play; s.producer != nil || s.result.Partial() || len(s.result.Outputs) != 0 {
		t.Fatalf("clear must stop the producer: producer=%v partial=%v n=%d", s.producer != nil, s.result.Partial(), len(s.result.Outputs))
	}
}

// TestPlayPagesTiming records the numbers the issue asks for: the time to
// the first page and the cost of appending one page, over `.[]` on 5,000
// objects (run with -v to read them).
func TestPlayPagesTiming(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, bigJSONArray(5000))))
	m.play.program.Set(".[]")
	start := time.Now()
	m = drainCmd(m, m.runPlayNow())
	first := time.Since(start)
	s := m.play
	if s.producer == nil {
		t.Fatal("paged")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	// G walks every remaining page in (the cursor follows the end).
	start = time.Now()
	m = playKeys(m, "G")
	appended := time.Since(start)
	pages := (len(s.result.Outputs) - jqplay.PageOutputs) / jqplay.PageOutputs
	if s.producer != nil || pages != 24 {
		t.Fatalf("producer=%v pages=%d", s.producer != nil, pages)
	}
	t.Logf("time to first page: %s; %d pages appended in %s, %s each (compute + append + fold/sign/outline refresh)", first, pages, appended, appended/time.Duration(pages))
}
