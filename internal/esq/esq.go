// Package esq is the Elasticsearch console backend (#1927): a small typed
// client over the cluster's read-only HTTP APIs (cat, mapping, search), the
// mapping→field derivation the console's completion source builds on, and the
// search-response→grid mapping the console pane renders. The pane
// (internal/espane) speaks only this package — no raw JSON or HTTP leaks past
// it — and the client is read-only by construction: the only requests it can
// form are GET _cat/…, GET …/_mapping and POST …/_search.
package esq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ike/internal/config"
)

// Timeout bounds every single cluster request. It is deliberately shorter
// than the .http client's 30 s default: the console fires requests as part of
// interactive browsing, and a dead endpoint must degrade to an error notice
// quickly rather than look like a hang.
const Timeout = 10 * time.Second

// maxBodyBytes caps a response read, mirroring the .http client's guard — a
// runaway aggregation cannot balloon the process.
const maxBodyBytes = 10 << 20

// Endpoint is one configured cluster, the esq-side mirror of
// config.ESEndpoint (the config type stays inside the config package's
// contract; this one is what clients are built from).
type Endpoint struct {
	Name     string
	URL      string
	Username string
	Password string
	APIKey   string
}

// Endpoints lists the configured clusters from the live config, in config
// order.
func Endpoints() []Endpoint {
	var eps []Endpoint
	for _, e := range config.Get().Elasticsearch.Endpoints {
		eps = append(eps, Endpoint{Name: e.Name, URL: e.URL, Username: e.Username, Password: e.Password, APIKey: e.APIKey})
	}
	return eps
}

// FindEndpoint resolves a configured endpoint by name against the live
// config, so a console pane restored from a session picks up config edits
// made since.
func FindEndpoint(name string) (Endpoint, bool) {
	for _, e := range Endpoints() {
		if e.Name == name {
			return e, true
		}
	}
	return Endpoint{}, false
}

// Client talks to one cluster. It is safe for concurrent use; every method
// takes a context and respects its cancellation.
type Client struct {
	ep Endpoint
	hc *http.Client
}

// NewClient builds a client for the endpoint. The endpoint URL is assumed
// valid (config validation drops unusable entries before they get here).
func NewClient(ep Endpoint) *Client {
	return &Client{ep: ep, hc: &http.Client{Timeout: Timeout}}
}

// Index is one sidebar row: an index or an alias, with the doc count shown
// next to the name. Docs is -1 when no count is available (an alias — sizing
// one means resolving every target), rendered as "?" like the data viewer's
// unsized views.
type Index struct {
	Name  string
	Docs  int64
	Alias bool
}

// Indices lists the cluster's indices and aliases, indices first, each group
// sorted by name. Doc counts come from _cat/indices — the cluster's own
// metadata, exact and cheap, so listing never scans.
func (c *Client) Indices(ctx context.Context) ([]Index, error) {
	var cat []struct {
		Index string `json:"index"`
		Docs  string `json:"docs.count"`
	}
	if err := c.getJSON(ctx, "/_cat/indices?format=json&h=index,docs.count", &cat); err != nil {
		return nil, err
	}
	var out []Index
	for _, e := range cat {
		docs := int64(-1)
		if n, err := strconv.ParseInt(e.Docs, 10, 64); err == nil {
			docs = n
		}
		out = append(out, Index{Name: e.Index, Docs: docs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	var aliases []struct {
		Alias string `json:"alias"`
	}
	// Aliases are additive sugar — a cluster (or a restrictive API key) that
	// refuses _cat/aliases still yields a usable index list.
	if err := c.getJSON(ctx, "/_cat/aliases?format=json&h=alias", &aliases); err == nil {
		seen := map[string]bool{}
		var al []Index
		for _, a := range aliases {
			if a.Alias == "" || seen[a.Alias] {
				continue
			}
			seen[a.Alias] = true
			al = append(al, Index{Name: a.Alias, Docs: -1, Alias: true})
		}
		sort.Slice(al, func(i, j int) bool { return al[i].Name < al[j].Name })
		out = append(out, al...)
	}
	return out, nil
}

// Mapping returns the index's mapping: the raw document pretty-printed for
// the mapping view, and the flattened field list for completion.
func (c *Client) Mapping(ctx context.Context, index string) (pretty string, fields []Field, err error) {
	raw, err := c.get(ctx, "/"+url.PathEscape(index)+"/_mapping")
	if err != nil {
		return "", nil, err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return "", nil, fmt.Errorf("mapping of %s is not valid JSON: %w", index, err)
	}
	return buf.String(), FieldsOf(raw), nil
}

// Result is one executed search, already shaped for the pane: the hit page as
// grid columns and rows, the raw hit documents for the per-row JSON view, and
// the aggregations (pretty JSON) when the query asked for any.
type Result struct {
	// Total is the query's full hit count; Exact is false when the cluster
	// reported a lower bound ("gte") instead of a count.
	Total int64
	Exact bool
	// Columns are _id, _score, then the union of _source keys across the
	// page's hits, sorted — a deterministic order that stays stable across
	// pages even when documents carry different fields.
	Columns []string
	// Rows holds len(Columns) cells per hit.
	Rows [][]Cell
	// Hits are the raw hit objects (_id, _score, _source, …) in page order,
	// for the read-only JSON view of one document.
	Hits []json.RawMessage
	// Aggs is the pretty-printed aggregations object, "" when the query had
	// none.
	Aggs string
	// From echoes the offset the page was fetched at.
	From int
}

// Cell is one grid value. Null marks a field the document does not carry (or
// carries as JSON null) — the grid draws it distinctly, like the data
// viewer's SQL NULL.
type Cell struct {
	Text string
	Null bool
}

// Search runs body — a Query-DSL search body, empty for match-all — against
// the index, paged by from/size. The body's own from/size/track_total_hits
// are overridden: paging belongs to the grid, and totals are always tracked
// exactly so the pane can page to the end without a second count request.
func (c *Client) Search(ctx context.Context, index, body string, from, size int) (*Result, error) {
	doc := map[string]any{}
	if s := strings.TrimSpace(body); s != "" {
		if err := json.Unmarshal([]byte(s), &doc); err != nil {
			return nil, fmt.Errorf("query is not valid JSON: %w", err)
		}
	}
	doc["from"] = from
	doc["size"] = size
	doc["track_total_hits"] = true
	payload, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	raw, err := c.post(ctx, "/"+url.PathEscape(index)+"/_search", payload)
	if err != nil {
		return nil, err
	}
	return parseSearch(raw, from)
}

// parseSearch shapes a raw _search response into a Result. Split out so the
// response→grid mapping is testable without a cluster.
func parseSearch(raw []byte, from int) (*Result, error) {
	var resp struct {
		Hits struct {
			Total struct {
				Value    int64  `json:"value"`
				Relation string `json:"relation"`
			} `json:"total"`
			Hits []json.RawMessage `json:"hits"`
		} `json:"hits"`
		Aggregations json.RawMessage `json:"aggregations"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("search response is not valid JSON: %w", err)
	}
	r := &Result{
		Total: resp.Hits.Total.Value,
		Exact: resp.Hits.Total.Relation == "" || resp.Hits.Total.Relation == "eq",
		Hits:  resp.Hits.Hits,
		From:  from,
	}
	type hit struct {
		ID     string                     `json:"_id"`
		Score  *float64                   `json:"_score"`
		Source map[string]json.RawMessage `json:"_source"`
	}
	hits := make([]hit, len(resp.Hits.Hits))
	cols := map[string]bool{}
	for i, h := range resp.Hits.Hits {
		if err := json.Unmarshal(h, &hits[i]); err != nil {
			return nil, fmt.Errorf("hit is not valid JSON: %w", err)
		}
		for k := range hits[i].Source {
			cols[k] = true
		}
	}
	srcCols := make([]string, 0, len(cols))
	for k := range cols {
		srcCols = append(srcCols, k)
	}
	sort.Strings(srcCols)
	r.Columns = append([]string{"_id", "_score"}, srcCols...)
	for _, h := range hits {
		row := make([]Cell, 0, len(r.Columns))
		row = append(row, Cell{Text: h.ID})
		if h.Score != nil {
			row = append(row, Cell{Text: strconv.FormatFloat(*h.Score, 'g', -1, 64)})
		} else {
			row = append(row, Cell{Null: true})
		}
		for _, col := range srcCols {
			row = append(row, cellOf(h.Source[col]))
		}
		r.Rows = append(r.Rows, row)
	}
	if len(resp.Aggregations) > 0 {
		var buf bytes.Buffer
		if err := json.Indent(&buf, resp.Aggregations, "", "  "); err == nil {
			r.Aggs = buf.String()
		}
	}
	return r, nil
}

// cellOf renders one _source value for the grid. Scalars render bare; a
// nested object or array renders as its compact JSON — the in-cell fallback
// for nested documents, with the full pretty document one keypress away.
func cellOf(v json.RawMessage) Cell {
	if len(v) == 0 || bytes.Equal(v, []byte("null")) {
		return Cell{Null: true}
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return Cell{Text: s}
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, v); err == nil {
		return Cell{Text: buf.String()}
	}
	return Cell{Text: string(v)}
}

// PrettyHit renders one raw hit for the read-only document view.
func PrettyHit(h json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, h, "", "  "); err != nil {
		return string(h)
	}
	return buf.String()
}

// get issues a GET and returns the body of a 2xx response, or the cluster's
// own error reason.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

// post issues a POST with a JSON body.
func (c *Client) post(ctx context.Context, path string, body []byte) ([]byte, error) {
	return c.do(ctx, http.MethodPost, path, body)
}

// getJSON is get plus decoding into out.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	raw, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("response is not valid JSON: %w", err)
	}
	return nil
}

// do is the single request funnel: it exists so auth is applied in exactly
// one place and so the method/path surface above stays the whole write
// surface — read-only against the cluster by construction.
func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	base := strings.TrimRight(c.ep.URL, "/")
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch {
	case c.ep.APIKey != "":
		req.Header.Set("Authorization", "ApiKey "+c.ep.APIKey)
	case c.ep.Username != "":
		req.SetBasicAuth(c.ep.Username, c.ep.Password)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s: %s", resp.Status, esError(raw))
	}
	return raw, nil
}

// esError digs the human-readable reason out of an Elasticsearch error
// document, falling back to a trimmed body when the shape is unfamiliar.
func esError(raw []byte) string {
	var doc struct {
		Error struct {
			Reason    string `json:"reason"`
			Type      string `json:"type"`
			RootCause []struct {
				Reason string `json:"reason"`
			} `json:"root_cause"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &doc); err == nil {
		if len(doc.Error.RootCause) > 0 && doc.Error.RootCause[0].Reason != "" {
			return doc.Error.RootCause[0].Reason
		}
		if doc.Error.Reason != "" {
			return doc.Error.Reason
		}
		if doc.Error.Type != "" {
			return doc.Error.Type
		}
	}
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		s = "no response body"
	}
	return s
}
