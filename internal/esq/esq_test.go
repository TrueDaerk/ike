package esq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeES is a minimal fake cluster: a handler map keyed by "METHOD path".
func fakeES(t *testing.T, routes map[string]string) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.Method+" "+r.URL.Path]
		clone := r.Clone(context.Background())
		seen = append(seen, clone)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"reason":"no handler for ` + r.URL.Path + `"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestIndicesListsIndicesThenAliasesSorted(t *testing.T) {
	srv, _ := fakeES(t, map[string]string{
		"GET /_cat/indices": `[{"index":"zebra","docs.count":"12"},{"index":"apples","docs.count":"3"}]`,
		"GET /_cat/aliases": `[{"alias":"all-fruit"},{"alias":"all-fruit"},{"alias":"animals"}]`,
	})
	got, err := NewClient(Endpoint{Name: "t", URL: srv.URL}).Indices(context.Background())
	if err != nil {
		t.Fatalf("Indices: %v", err)
	}
	want := []Index{
		{Name: "apples", Docs: 3},
		{Name: "zebra", Docs: 12},
		{Name: "all-fruit", Docs: -1, Alias: true},
		{Name: "animals", Docs: -1, Alias: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestIndicesSurvivesAliasFailure(t *testing.T) {
	srv, _ := fakeES(t, map[string]string{
		"GET /_cat/indices": `[{"index":"a","docs.count":"1"}]`,
	})
	got, err := NewClient(Endpoint{URL: srv.URL}).Indices(context.Background())
	if err != nil {
		t.Fatalf("Indices: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %+v, want just index a", got)
	}
}

func TestIndicesDeadEndpointErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // dead on arrival
	if _, err := NewClient(Endpoint{URL: srv.URL}).Indices(context.Background()); err == nil {
		t.Fatal("want an error from a dead endpoint")
	}
}

func TestAuthHeaders(t *testing.T) {
	srv, seen := fakeES(t, map[string]string{
		"GET /_cat/indices": `[]`,
		"GET /_cat/aliases": `[]`,
	})
	if _, err := NewClient(Endpoint{URL: srv.URL, Username: "elastic", Password: "s3cret"}).Indices(context.Background()); err != nil {
		t.Fatalf("Indices: %v", err)
	}
	u, p, ok := (*seen)[0].BasicAuth()
	if !ok || u != "elastic" || p != "s3cret" {
		t.Fatalf("basic auth = %q/%q (%v), want elastic/s3cret", u, p, ok)
	}

	srv2, seen2 := fakeES(t, map[string]string{
		"GET /_cat/indices": `[]`,
		"GET /_cat/aliases": `[]`,
	})
	if _, err := NewClient(Endpoint{URL: srv2.URL, APIKey: "abc123=="}).Indices(context.Background()); err != nil {
		t.Fatalf("Indices: %v", err)
	}
	if got := (*seen2)[0].Header.Get("Authorization"); got != "ApiKey abc123==" {
		t.Fatalf("Authorization = %q, want ApiKey abc123==", got)
	}
}

func TestSearchInjectsPagingAndTracking(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = make([]byte, r.ContentLength)
		r.Body.Read(captured)
		w.Write([]byte(`{"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`))
	}))
	t.Cleanup(srv.Close)
	body := `{"query":{"match_all":{}},"from":99,"size":1}`
	if _, err := NewClient(Endpoint{URL: srv.URL}).Search(context.Background(), "idx", body, 500, 250); err != nil {
		t.Fatalf("Search: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal(captured, &sent); err != nil {
		t.Fatalf("sent body does not parse: %v (%s)", err, captured)
	}
	if sent["from"] != float64(500) || sent["size"] != float64(250) {
		t.Errorf("from/size = %v/%v, want 500/250 (the grid's paging overrides the body's)", sent["from"], sent["size"])
	}
	if sent["track_total_hits"] != true {
		t.Errorf("track_total_hits = %v, want true", sent["track_total_hits"])
	}
	if _, ok := sent["query"]; !ok {
		t.Error("the body's query clause was lost")
	}
}

func TestSearchRejectsInvalidJSONBody(t *testing.T) {
	_, err := NewClient(Endpoint{URL: "http://127.0.0.1:1"}).Search(context.Background(), "idx", `{"query"`, 0, 10)
	if err == nil || !strings.Contains(err.Error(), "query is not valid JSON") {
		t.Fatalf("err = %v, want a query-is-not-valid-JSON error before any request", err)
	}
}

func TestSearchSurfacesClusterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"reason":"outer","root_cause":[{"reason":"unknown query [matchh]"}]},"status":400}`))
	}))
	t.Cleanup(srv.Close)
	_, err := NewClient(Endpoint{URL: srv.URL}).Search(context.Background(), "idx", "", 0, 10)
	if err == nil || !strings.Contains(err.Error(), "unknown query [matchh]") {
		t.Fatalf("err = %v, want the cluster's root-cause reason", err)
	}
}

func TestParseSearchShapesGrid(t *testing.T) {
	raw := []byte(`{
		"hits": {
			"total": {"value": 1204, "relation": "eq"},
			"hits": [
				{"_id": "1", "_score": 1.5, "_source": {"name": "ada", "age": 36, "tags": ["a","b"], "addr": {"city": "x"}}},
				{"_id": "2", "_score": null, "_source": {"name": "bob", "active": true}}
			]
		},
		"aggregations": {"per_tag": {"buckets": []}}
	}`)
	r, err := parseSearch(raw, 500)
	if err != nil {
		t.Fatalf("parseSearch: %v", err)
	}
	wantCols := []string{"_id", "_score", "active", "addr", "age", "name", "tags"}
	if strings.Join(r.Columns, ",") != strings.Join(wantCols, ",") {
		t.Fatalf("columns = %v, want %v (meta first, _source sorted)", r.Columns, wantCols)
	}
	if r.Total != 1204 || !r.Exact {
		t.Errorf("total = %d exact=%v, want 1204 exact", r.Total, r.Exact)
	}
	if r.From != 500 {
		t.Errorf("from = %d, want 500", r.From)
	}
	if len(r.Rows) != 2 || len(r.Hits) != 2 {
		t.Fatalf("rows/hits = %d/%d, want 2/2", len(r.Rows), len(r.Hits))
	}
	row := map[string]Cell{}
	for i, c := range r.Columns {
		row[c] = r.Rows[0][i]
	}
	if row["_id"].Text != "1" || row["_score"].Text != "1.5" || row["name"].Text != "ada" || row["age"].Text != "36" {
		t.Errorf("scalar cells wrong: %+v", row)
	}
	if row["tags"].Text != `["a","b"]` || row["addr"].Text != `{"city":"x"}` {
		t.Errorf("nested cells should render compact JSON, got tags=%q addr=%q", row["tags"].Text, row["addr"].Text)
	}
	row2 := map[string]Cell{}
	for i, c := range r.Columns {
		row2[c] = r.Rows[1][i]
	}
	if !row2["tags"].Null || !row2["age"].Null || !row2["_score"].Null {
		t.Errorf("missing fields and null score must render as null cells: %+v", row2)
	}
	if !strings.Contains(r.Aggs, "per_tag") || !strings.Contains(r.Aggs, "\n") {
		t.Errorf("aggregations should be pretty JSON, got %q", r.Aggs)
	}
}

func TestParseSearchInexactTotal(t *testing.T) {
	r, err := parseSearch([]byte(`{"hits":{"total":{"value":10000,"relation":"gte"},"hits":[]}}`), 0)
	if err != nil {
		t.Fatalf("parseSearch: %v", err)
	}
	if r.Exact {
		t.Error("a gte relation must not report an exact total")
	}
}
