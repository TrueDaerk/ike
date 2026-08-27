package dataview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/datasrc"
	"ike/internal/theme"
)

// lazy_test.go covers the pane's side of #1795: the database opens in the
// background, and the exact row count is a lazy, cached, per-table job the
// grid never waits for. The backend is a fake that records every call, so
// "paging issues no COUNT(*)" is an assertion rather than a hope.

// fakeSource is a Source counting what the pane asks it for. rows is the real
// size of its single big table; est is the metadata estimate the listing hands
// over, deliberately off by a few rows the way max(rowid) is after a delete.
type fakeSource struct {
	rows     int64
	est      int64
	countErr error

	pages   int      // Page/PageWhere calls
	counts  int      // Count calls
	counted []string // what each count was for, "table|clause"

	// sorted records the sort of every PageWhere call, and pageBlock (when
	// non-nil) suspends every page fetch until the test closes it — the
	// export's cancel path needs a backend that can be caught mid-scan
	// (#2248).
	sorted    []datasrc.Sort
	pageBlock chan struct{}

	// Column profile (#1940): profiles counts the calls, profiled records
	// what each was for, block (when non-nil) suspends the call until the
	// test closes it, and profileErr makes it fail.
	profiles   int
	profiled   []string
	block      chan struct{}
	profileErr error
}

func (f *fakeSource) Tables() ([]datasrc.Table, error) {
	return []datasrc.Table{
		{Name: "big", Type: "table", Rows: f.est, Estimated: true},
		{Name: "other", Type: "table", Rows: 7, Estimated: true},
		{Name: "v", Type: "view", Rows: -1},
	}, nil
}

func (f *fakeSource) Page(table string, offset, limit int64) (datasrc.Page, error) {
	f.pages++
	n := f.rows
	if table != "big" {
		n = 0
	}
	page := datasrc.Page{Columns: []string{"id"}, Offset: offset, Total: -1}
	for i := offset; i < offset+limit && i < n; i++ {
		page.Rows = append(page.Rows, []datasrc.Cell{{Text: fmt.Sprintf("%d", i+1)}})
	}
	return page, nil
}

func (f *fakeSource) PageWhere(table, clause string, sort datasrc.Sort, offset, limit int64) (datasrc.Page, error) {
	if f.pageBlock != nil {
		// The export's page fetches hang here until the test releases them,
		// which is how "the export is async and cancelable" (#2248) is
		// asserted without a database slow enough to catch in the act.
		// Nothing is recorded on this path, so the blocked goroutine touches
		// no field the test reads.
		<-f.pageBlock
		return datasrc.Page{Columns: []string{"id"}, Offset: offset,
			Total: -1, Rows: [][]datasrc.Cell{{{Text: "1"}}}}, nil
	}
	f.sorted = append(f.sorted, sort)
	if clause == "" && !sort.Active() {
		return f.Page(table, offset, limit)
	}
	if clause == "" {
		// Sorted but unfiltered: the fake serves the same rows in reverse for
		// a descending sort, which is enough for the pane's paging to be
		// checked without a real engine.
		page, err := f.Page(table, offset, limit)
		if err != nil || !sort.Desc {
			return page, err
		}
		for i, j := 0, len(page.Rows)-1; i < j; i, j = i+1, j-1 {
			page.Rows[i], page.Rows[j] = page.Rows[j], page.Rows[i]
		}
		return page, nil
	}
	f.pages++
	return datasrc.Page{Columns: []string{"id"}, Offset: offset, Total: -1,
		Rows: [][]datasrc.Cell{{{Text: "1"}}}}, nil
}

func (f *fakeSource) Count(table, clause string) (int64, error) {
	f.counts++
	f.counted = append(f.counted, table+"|"+clause)
	if f.countErr != nil {
		return -1, f.countErr
	}
	if clause != "" {
		return 1, nil
	}
	if table != "big" {
		return 0, nil
	}
	return f.rows, nil
}

func (f *fakeSource) FilterPrefix(table string) string { return "SELECT * FROM " + table + " " }

// Profile answers the column profile (#1940). block, when set, holds the
// call until the test releases it — that is how "the profile is async and
// cancelable" is asserted without a real database to be slow.
func (f *fakeSource) Profile(ctx context.Context, table, column, clause string) (datasrc.Profile, error) {
	f.profiles++
	f.profiled = append(f.profiled, table+"."+column+"|"+clause)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return datasrc.Profile{}, ctx.Err()
		}
	}
	if f.profileErr != nil {
		return datasrc.Profile{}, f.profileErr
	}
	return datasrc.Profile{
		Table: table, Column: column, Filter: clause,
		Rows: f.rows, Nulls: 2, Distinct: 3,
		Min: datasrc.Cell{Text: "1"}, Max: datasrc.Cell{Text: "9"},
		Top: []datasrc.TopValue{{Value: datasrc.Cell{Text: "7"}, Count: 4}},
	}, nil
}

func (f *fakeSource) Schema(string) (string, error) { return "CREATE TABLE big (id INTEGER)", nil }

func (f *fakeSource) Close() error { return nil }

// fakePane wires a pane onto src the way a finished background open would,
// then hands back the pane with the fake's counters reset — so every call a
// test observes is one the *pane* made.
func fakePane(t *testing.T, src *fakeSource) Model {
	t.Helper()
	m := New("data", "/tmp/big.db", theme.DefaultPalette())
	t.Cleanup(func() { m.Close() })
	m.SetSize(100, 24)
	m.SetFocused(true)
	m.started, m.opening = true, true
	tables, _ := src.Tables()
	page, _ := src.Page(tables[0].Name, 0, PageSize)
	src.pages, src.counts, src.counted = 0, 0, nil
	pump(t, &m, func() tea.Msg {
		return ResultMsg{Key: "data", open: &openResult{src: src, tables: tables, page: page}}
	})
	return m
}

// TestOpenIsAsyncAndTheDatabaseArrivesLater: New touches no file at all — the
// pane exists, sized and drawable, while its database is still opening.
func TestOpenIsAsyncAndTheDatabaseArrivesLater(t *testing.T) {
	m := New("data", writeFixtureDB(t), theme.DefaultPalette())
	defer m.Close()
	m.SetSize(100, 24)
	if m.Tables() != 0 || m.Err() != nil || m.Opening() {
		t.Fatalf("New must not open anything: tables=%d err=%v", m.Tables(), m.Err())
	}
	cmd := m.Init()
	if cmd == nil || !m.Opening() {
		t.Fatal("Init must start the background open")
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "opening") {
		t.Fatalf("an opening pane must say so:\n%s", view)
	}
	// A second Init does not open the database twice.
	if again := m.Init(); again != nil {
		t.Fatal("Init must be a one-shot")
	}
	pump(t, &m, cmd)
	if m.Opening() || m.Tables() != 3 || m.SelectedTable() != "empty" {
		t.Fatalf("after the open: opening=%v tables=%d selected=%q", m.Opening(), m.Tables(), m.SelectedTable())
	}
}

// TestOpenCountsOnlyTheLoadedTable: opening lists the database and counts
// exactly one object — the one the grid shows. The other tables keep their
// metadata estimate until the user goes there.
func TestOpenCountsOnlyTheLoadedTable(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1204}
	m := fakePane(t, src)
	if src.counts != 1 || src.counted[0] != "big|" {
		t.Fatalf("open counted %v, want the loaded table only", src.counted)
	}
	if m.tables[0].Rows != 1200 || m.tables[0].Estimated {
		t.Fatalf("the count must replace the estimate: %+v", m.tables[0])
	}
	// The unvisited table keeps the estimate, marked as one.
	view := stripANSI(m.View())
	if !strings.Contains(view, "~7") {
		t.Fatalf("an unvisited table must show its estimate:\n%s", view)
	}
	// Selecting it is what makes it worth counting.
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	if src.counts != 2 || src.counted[1] != "other|" {
		t.Fatalf("loading a table must count it: %v", src.counted)
	}
}

// TestPagingReusesTheCachedTotal: walking a table with n/p — and back to it
// after a detour — never counts again. This is the regression the whole change
// exists for: a COUNT(*) per fetch made every keystroke a full scan.
func TestPagingReusesTheCachedTotal(t *testing.T) {
	src := &fakeSource{rows: 5000, est: 5000}
	m := fakePane(t, src)
	if src.counts != 1 {
		t.Fatalf("counts after open = %d, want 1", src.counts)
	}
	feed(t, &m, key("tab")) // into the grid
	for i := 0; i < 4; i++ {
		feed(t, &m, key("n"))
	}
	feed(t, &m, key("p"))
	feed(t, &m, key("g"))
	feed(t, &m, key("G"))
	if src.counts != 1 {
		t.Fatalf("paging issued %d counts, want the cached total to serve them all", src.counts)
	}
	if src.pages < 6 {
		t.Fatalf("paging fetched %d pages — the test did not page", src.pages)
	}
	// The cached total still drives the status line and the last-page jump.
	if m.PageOffset() != 4500 || !strings.Contains(stripANSI(m.View()), "of 5000") {
		t.Fatalf("offset=%d, view:\n%s", m.PageOffset(), stripANSI(m.View()))
	}
	// Leaving the table and coming back reuses the cache as well.
	feed(t, &m, key("tab"))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	feed(t, &m, key("tab"))
	feed(t, &m, key("k"))
	feed(t, &m, key("enter"))
	if src.counts != 2 {
		t.Fatalf("returning to a counted table counted again: %v", src.counted)
	}
}

// TestEstimateIsMarkedUntilTheCountLands: the status line and the sidebar say
// "~1204" while the number is the engine's guess and "1200" once it is
// counted, so a guess never reads as a fact.
func TestEstimateIsMarkedUntilTheCountLands(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1204}
	m := New("data", "/tmp/big.db", theme.DefaultPalette())
	defer m.Close()
	m.SetSize(100, 24)
	m.SetFocused(true)
	m.started = true
	tables, _ := src.Tables()
	page, _ := src.Page("big", 0, PageSize)
	// Apply the open and leave the count it asks for in flight: this is the
	// frame between the rows appearing and the count landing.
	m.applyOpen(&openResult{src: src, tables: tables, page: page})
	if !m.counting {
		t.Fatal("the open must ask for the loaded table's count")
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, fmt.Sprintf("rows 1–%d of ~1204", PageSize)) {
		t.Fatalf("an estimated total must be marked:\n%s", view)
	}
	// G needs an exact total: an estimate would land the jump next to the end.
	feed(t, &m, key("tab"))
	feed(t, &m, key("G"))
	if m.PageOffset() != 0 {
		t.Fatalf("G jumped on an estimate: offset = %d", m.PageOffset())
	}
	// Paging still works without a trustworthy total: a full page means more.
	feed(t, &m, key("n"))
	if m.PageOffset() != PageSize {
		t.Fatalf("paging must work before the count lands: offset = %d", m.PageOffset())
	}
	// The count lands.
	m.counting = false
	pump(t, &m, m.countCmd())
	if view := stripANSI(m.View()); !strings.Contains(view, "of 1200") || strings.Contains(view, "~1200") {
		t.Fatalf("the counted total must lose the marker:\n%s", view)
	}
	feed(t, &m, key("G"))
	if m.PageOffset() != 1000 {
		t.Fatalf("G must jump once the count landed: offset = %d", m.PageOffset())
	}
}

// TestFailedCountKeepsTheEstimateAndIsNotRetried: a count that fails (a view
// over a dropped table) leaves the estimate standing — unconfirmed, not wrong
// — and is not run again on every keystroke.
func TestFailedCountKeepsTheEstimateAndIsNotRetried(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1204, countErr: errors.New("no such table")}
	m := fakePane(t, src)
	if src.counts != 1 {
		t.Fatalf("counts = %d, want the one attempt", src.counts)
	}
	feed(t, &m, key("tab"))
	for i := 0; i < 3; i++ {
		feed(t, &m, key("n"))
	}
	if src.counts != 1 {
		t.Fatalf("a failed count was retried %d times", src.counts-1)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "~1204") {
		t.Fatalf("the estimate must survive a failed count:\n%s", view)
	}
}

// TestFilterCountsItsOwnResult: a filter's total is counted under its own
// cache key, in the background — the apply itself never waits for a scan.
func TestFilterCountsItsOwnResult(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200}
	m := fakePane(t, src)
	feed(t, &m, key("tab"))
	feed(t, &m, key("/"))
	typeInto(t, &m, "WHERE id = 1")
	feed(t, &m, key("enter"))
	if m.Filter() != "WHERE id = 1" {
		t.Fatalf("filter = %q", m.Filter())
	}
	if len(src.counted) != 2 || src.counted[1] != "big|WHERE id = 1" {
		t.Fatalf("counts = %v, want the filtered result counted once", src.counted)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "rows 1–1 of 1") {
		t.Fatalf("the filtered total must reach the status line:\n%s", view)
	}
	// Dropping the filter falls back to the table's cached total.
	feed(t, &m, key("/"))
	feed(t, &m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if src.counts != 2 {
		t.Fatalf("clearing the filter counted again: %v", src.counted)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "of 1200") {
		t.Fatalf("the unfiltered total must come back from the cache:\n%s", view)
	}
}

// TestExactMetadataCountIsNeverRecounted: a backend whose listing is already
// exact (a Parquet footer) spares its table the background count entirely.
func TestExactMetadataCountIsNeverRecounted(t *testing.T) {
	m := newPane(t, writeFixtureParquet(t))
	if m.SelectedTable() != "events.parquet" {
		t.Fatalf("selected = %q", m.SelectedTable())
	}
	if !m.totalExact || m.page.Total != 1200 {
		t.Fatalf("the footer count must be adopted as exact: total=%d exact=%v", m.page.Total, m.totalExact)
	}
	if cmd := m.countCmd(); cmd != nil {
		t.Fatal("an exactly sized table must not be counted again")
	}
}
