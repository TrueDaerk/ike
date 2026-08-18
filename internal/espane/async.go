package espane

// async.go drives everything that touches the cluster. The pane follows the
// data viewer's background-open pattern (#1795) — but applies it to *every*
// fetch, not just the open: a page of hits is a network round trip, so no
// fetch may run inside Update. All results funnel into the one ResultMsg the
// root model routes back by pane key; a result landing after its pane closed,
// or after a newer fetch superseded it, is dropped.

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"ike/internal/esq"
)

// ResultMsg carries one background result to its pane: exactly one of the
// pointers is set. The root model routes it by Key.
type ResultMsg struct {
	Key     string
	open    *openResult
	page    *pageResult
	mapping *mappingResult
}

// Discard drops an unroutable result (its pane is gone). The console holds no
// releasable handles, so this is bookkeeping parity with the data viewer's
// routing shape, not cleanup.
func (msg ResultMsg) Discard() {}

type openResult struct {
	indices []esq.Index
	err     error
}

type pageResult struct {
	index string
	seq   int
	res   *esq.Result
	err   error
}

type mappingResult struct {
	index  string
	pretty string
	fields []esq.Field
	show   bool
	err    error
}

// Init is the one-shot background connect: resolve the endpoint against the
// live config, then list the cluster's indices off the UI goroutine. An
// endpoint edited out of the config fails immediately with a notice.
func (m *Model) Init() tea.Cmd {
	if m.started || m.err != nil {
		return nil
	}
	m.started, m.opening = true, true
	ep, ok := esq.FindEndpoint(m.endpoint)
	if !ok {
		m.opening = false
		m.err = fmt.Errorf("endpoint %q is not configured (Settings › Elasticsearch)", m.endpoint)
		return nil
	}
	m.client = esq.NewClient(ep)
	client, key := m.client, m.key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), esq.Timeout)
		defer cancel()
		indices, err := client.Indices(ctx)
		return ResultMsg{Key: key, open: &openResult{indices: indices, err: err}}
	}
}

// applyResult lands one background result.
func (m *Model) applyResult(msg ResultMsg) tea.Cmd {
	if m.closed {
		return nil
	}
	switch {
	case msg.open != nil:
		return m.applyOpen(msg.open)
	case msg.page != nil:
		return m.applyPage(msg.page)
	case msg.mapping != nil:
		return m.applyMapping(msg.mapping)
	}
	return nil
}

// applyOpen installs the index listing and loads the first index — the same
// first-frame behaviour as the data viewer, so the console opens onto data.
func (m *Model) applyOpen(r *openResult) tea.Cmd {
	m.opening = false
	if r.err != nil {
		m.err = r.err
		return nil
	}
	m.indices = r.indices
	m.clampScroll()
	if p := m.pending; p != nil {
		// A query queued while the connect was out (run-from-editor, #1927)
		// beats the default first-index load.
		m.pending = nil
		if cmd, ok := m.RunQuery(p.index, p.body); ok {
			return cmd
		}
	}
	if len(m.indices) > 0 {
		return m.loadIndex(0)
	}
	return nil
}

// fetchCmd fetches one page of the selected index's active query, unless a
// fetch is already out — holding n down must not fan out a request per
// keypress. Index switches and query runs go through forceFetchCmd instead:
// they supersede the flight rather than wait for it.
func (m *Model) fetchCmd(from int) tea.Cmd {
	if m.loading {
		return nil
	}
	return m.forceFetchCmd(from)
}

// forceFetchCmd starts a page fetch unconditionally, stamping it with the
// next sequence number so the result of any older flight is dropped on
// landing.
func (m *Model) forceFetchCmd(from int) tea.Cmd {
	if m.client == nil || m.sel < 0 || m.sel >= len(m.indices) {
		return nil
	}
	name := m.indices[m.sel].Name
	body := m.queries[name]
	m.loading = true
	m.fetchSeq++
	seq := m.fetchSeq
	client, key := m.client, m.key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), esq.Timeout)
		defer cancel()
		res, err := client.Search(ctx, name, body, from, PageSize)
		return ResultMsg{Key: key, page: &pageResult{index: name, seq: seq, res: res, err: err}}
	}
}

// applyPage lands one page. A stale flight — superseded by a newer fetch or a
// switched index — is dropped; an error keeps the last good page and shows in
// the grid, like the data viewer's clause errors.
func (m *Model) applyPage(r *pageResult) tea.Cmd {
	if r.seq != m.fetchSeq {
		return nil
	}
	m.loading = false
	if r.index != m.SelectedIndex() {
		return nil
	}
	if r.err != nil {
		m.pageErr = r.err
		return nil
	}
	m.pageErr = nil
	m.res = r.res
	if m.pendingBottom {
		m.rowCur = len(r.res.Rows) - 1
	} else {
		m.rowCur = 0
	}
	m.pendingBottom = false
	m.rowTop = 0
	m.clampScroll()
	return nil
}

// mappingCmd fetches the index mapping in the background. The result primes
// the query buffer's completion (esq.PrimeFields) and the s view's cache;
// show additionally opens the read-only view once it lands. An already-cached
// mapping is not refetched.
func (m *Model) mappingCmd(index string, show bool) tea.Cmd {
	if m.client == nil {
		return nil
	}
	if _, ok := m.mappings[index]; ok && !show {
		return nil
	}
	client, key := m.client, m.key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), esq.Timeout)
		defer cancel()
		pretty, fields, err := client.Mapping(ctx, index)
		return ResultMsg{Key: key, mapping: &mappingResult{index: index, pretty: pretty, fields: fields, show: show, err: err}}
	}
}

// applyMapping caches the mapping and primes completion. A fetch that failed
// stays silent unless the user asked to see it — completion simply lacks
// fields until the next trigger retries.
func (m *Model) applyMapping(r *mappingResult) tea.Cmd {
	if r.err != nil {
		if r.show {
			m.pageErr = r.err
		}
		return nil
	}
	m.mappings[r.index] = r.pretty
	esq.PrimeFields(m.endpoint, r.index, r.fields)
	if r.show {
		msg := ShowJSONMsg{Endpoint: m.endpoint, Name: r.index + ".mapping", JSON: r.pretty}
		return func() tea.Msg { return msg }
	}
	return nil
}
