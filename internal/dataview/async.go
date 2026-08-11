package dataview

// async.go is what keeps a large database from freezing the IDE (#1795).
//
// Two things used to run on the UI thread while the pane opened: the engine
// open with its table listing, and an exact `COUNT(*)` per table — a full scan
// each, so a multi-gigabyte file blocked IKE for as long as it took to count
// every row in it. Both moved here:
//
//   - Init opens the database, lists it and reads the first page in one
//     background command; the pane exists (and draws its loading notice) from
//     the first frame.
//   - The exact row count runs lazily, one background command for the *loaded*
//     table only, and lands in a per-table cache. Paging reads the cache and
//     never counts again, and until the count arrives the sidebar shows the
//     engine's cheap estimate ("~1200") or "?".
//
// Results come back as one ResultMsg the root model routes by pane key, so a
// data viewer that lives in a tab (#1778) receives them too.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/datasrc"
)

// ResultMsg carries one background result to the pane that asked for it: the
// open, or an exact row count. The payload is the pane's own business — the
// root model only routes it by Key, and discards it when no pane matches.
type ResultMsg struct {
	Key   string
	open  *openResult
	count *countResult
}

// Discard releases what an unrouted result holds. Only the open carries a
// resource: a database whose pane closed while it was opening must not leak
// its handle.
func (msg ResultMsg) Discard() {
	if msg.open != nil && msg.open.src != nil {
		msg.open.src.Close()
	}
}

// openResult is what the background open produced: the engine, the table list
// and the first table's first page.
type openResult struct {
	src     datasrc.Source
	tables  []datasrc.Table
	err     error
	page    datasrc.Page
	pageErr error
}

// countResult is one finished row count, identified by what it counted.
type countResult struct {
	key totalKey
	n   int64
	err error
}

// totalKey identifies a countable result set: one table under one filter
// clause ("" — the whole table).
type totalKey struct{ table, filter string }

// totalVal is what the pane knows about a result set's size. exact separates
// the engine's own count from the metadata estimate the listing started with;
// done marks that an exact count was already attempted, so a failed count is
// never retried on every keystroke.
type totalVal struct {
	n     int64
	exact bool
	done  bool
}

// Init opens the database in the background and returns the command doing it.
// It is called once per pane by the root model — a second call is a no-op, so
// a restored pane that is also opened explicitly does not open twice.
func (m *Model) Init() tea.Cmd {
	if m.started || m.src != nil || m.err != nil {
		return nil
	}
	m.started, m.opening = true, true
	key, path := m.key, m.path
	return func() tea.Msg {
		r := &openResult{}
		r.src, r.err = datasrc.Open(path)
		if r.err == nil {
			if r.tables, r.err = r.src.Tables(); r.err != nil {
				r.src.Close()
				r.src = nil
			}
		}
		// The first table's first page rides along: one command, one frame —
		// the pane goes from "opening" straight to rows.
		if r.err == nil && len(r.tables) > 0 {
			r.page, r.pageErr = r.src.Page(r.tables[0].Name, 0, PageSize)
		}
		return ResultMsg{Key: key, open: r}
	}
}

// Opening reports whether the background open is still running (tests).
func (m *Model) Opening() bool { return m.opening }

// applyResult folds one background result into the pane.
func (m *Model) applyResult(msg ResultMsg) tea.Cmd {
	switch {
	case msg.open != nil:
		return m.applyOpen(msg.open)
	case msg.count != nil:
		return m.applyCount(msg.count)
	}
	return nil
}

// applyOpen installs the opened database. A pane closed while the open ran
// releases the engine instead — the result outlived what asked for it.
func (m *Model) applyOpen(r *openResult) tea.Cmd {
	m.opening = false
	if m.closed {
		if r.src != nil {
			r.src.Close()
		}
		return nil
	}
	m.src, m.tables, m.err = r.src, r.tables, r.err
	if m.err != nil {
		return nil
	}
	m.seedTotals()
	if len(m.tables) > 0 {
		m.sel, m.tcur = 0, 0
		m.page, m.pageErr = r.page, r.pageErr
		if m.pageErr != nil {
			m.page = datasrc.Page{Offset: 0}
		}
		m.adoptTotal()
	}
	m.clampScroll()
	return m.countCmd()
}

// applyCount caches one finished count and reflects it in the sidebar and the
// status line. A count that failed (a view over a dropped table) keeps the
// estimate the listing had — the number is unconfirmed, not wrong — and is
// marked done so it is not retried.
func (m *Model) applyCount(r *countResult) tea.Cmd {
	m.counting = false
	v := m.totals[r.key]
	v.done = true
	if r.err == nil && r.n >= 0 {
		v.n, v.exact = r.n, true
		if r.key.filter == "" {
			for i := range m.tables {
				if m.tables[i].Name == r.key.table {
					m.tables[i].Rows, m.tables[i].Estimated = r.n, false
				}
			}
		}
	}
	m.setTotal(r.key, v)
	m.adoptTotal()
	// The selection may have moved on while this count ran — the next one
	// starts only now, so at most one count is ever in flight per pane.
	return m.countCmd()
}

// countCmd returns the command counting the loaded table under the active
// filter, or nil when the total is already known, one count is already
// running, or nothing is loaded. This is the whole lazy-count policy: only the
// table the user is looking at is ever counted, and only once.
func (m *Model) countCmd() tea.Cmd {
	if m.src == nil || m.counting || m.sel < 0 || m.sel >= len(m.tables) {
		return nil
	}
	k := totalKey{table: m.tables[m.sel].Name, filter: m.filter}
	if v, ok := m.totals[k]; ok && v.done {
		return nil
	}
	m.counting = true
	src, key := m.src, m.key
	return func() tea.Msg {
		n, err := src.Count(k.table, k.filter)
		return ResultMsg{Key: key, count: &countResult{key: k, n: n, err: err}}
	}
}

// seedTotals turns the listing's metadata sizes into cache entries: an exact
// one (a Parquet footer, an empty SQLite table) is final and spares the table
// its background count, an estimate is shown until the count replaces it.
func (m *Model) seedTotals() {
	for _, t := range m.tables {
		exact := t.Rows >= 0 && !t.Estimated
		m.setTotal(totalKey{table: t.Name}, totalVal{n: t.Rows, exact: exact, done: exact})
	}
}

// setTotal writes one cache entry.
func (m *Model) setTotal(k totalKey, v totalVal) {
	if m.totals == nil {
		m.totals = map[totalKey]totalVal{}
	}
	m.totals[k] = v
}

// adoptTotal puts the cached total of what the grid currently shows on the
// loaded page. It is why paging costs no count: every page of a table reads
// the one number its table was counted for.
func (m *Model) adoptTotal() {
	if m.sel < 0 || m.sel >= len(m.tables) {
		m.page.Total, m.totalExact = -1, false
		return
	}
	v, ok := m.totals[totalKey{table: m.tables[m.sel].Name, filter: m.filter}]
	if !ok {
		m.page.Total, m.totalExact = -1, false
		return
	}
	m.page.Total, m.totalExact = v.n, v.exact
}

// recordPageTotal keeps a total a backend handed over for free (a Parquet
// footer). Nothing else counts during a fetch, so this is the only way an
// exact total arrives without a background count.
func (m *Model) recordPageTotal() {
	if m.page.Total < 0 || m.sel < 0 || m.sel >= len(m.tables) {
		return
	}
	k := totalKey{table: m.tables[m.sel].Name, filter: m.filter}
	if v, ok := m.totals[k]; ok && v.exact {
		return
	}
	m.setTotal(k, totalVal{n: m.page.Total, exact: true, done: true})
}
