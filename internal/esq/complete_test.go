package esq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/complete"
	"ike/internal/config"
	"ike/internal/host"
)

func TestQueryPathRoundTrip(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path, err := QueryPath("Prod Cluster", "web-logs-2026")
	if err != nil {
		t.Fatalf("QueryPath: %v", err)
	}
	if !IsQueryPath(path) {
		t.Fatalf("IsQueryPath(%q) = false", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != QueryTemplate {
		t.Fatalf("fresh buffer = %q (%v), want the match-all template", raw, err)
	}
	ep, idx, ok := QueryRef(path)
	if !ok || ep != "Prod Cluster" || idx != "web-logs-2026" {
		t.Fatalf("QueryRef = %q/%q/%v, want the registered originals", ep, idx, ok)
	}
	// A second allocation reuses the file untouched.
	os.WriteFile(path, []byte(`{"query":{"term":{"a":1}}}`), 0o644)
	again, err := QueryPath("Prod Cluster", "web-logs-2026")
	if err != nil || again != path {
		t.Fatalf("re-allocation = %q (%v), want the same path", again, err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "match_all") {
		t.Fatal("re-allocation overwrote an existing buffer")
	}
}

func TestQueryRefFallsBackToLayout(t *testing.T) {
	// A path never registered this run (session restore) resolves through the
	// directory layout.
	ep, idx, ok := QueryRef(filepath.Join("/state", "es", "prod", "logs"+QueryExt))
	if !ok || ep != "prod" || idx != "logs" {
		t.Fatalf("QueryRef = %q/%q/%v, want prod/logs", ep, idx, ok)
	}
	if _, _, ok := QueryRef("/tmp/other.json"); ok {
		t.Fatal("a plain .json file must not resolve as a query buffer")
	}
}

func TestStringPrefix(t *testing.T) {
	cases := []struct {
		line   string
		col    int
		want   string
		inside bool
	}{
		{`    "match`, 10, "match", true},
		{`    "user.add`, 13, "user.add", true},
		{`  "done": `, 10, "", false},
		{`  "a": "b`, 9, "b", true},
		{`  "esc\"aped`, 12, `esc\"aped`, true},
		{`plain`, 5, "", false},
		{`""`, 2, "", false},
	}
	for _, c := range cases {
		got, in := stringPrefix(c.line, c.col)
		if got != c.want || in != c.inside {
			t.Errorf("stringPrefix(%q, %d) = %q/%v, want %q/%v", c.line, c.col, got, in, c.want, c.inside)
		}
	}
}

// primedSource builds a source with a registered query buffer and a primed
// field cache, plus the buffer text stashed via Observe.
func primedSource(t *testing.T, text string) (*CompletionSource, string) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path, err := QueryPath("prod", "products")
	if err != nil {
		t.Fatalf("QueryPath: %v", err)
	}
	resetFieldCache()
	s := NewCompletionSource()
	PrimeFields("prod", "products", []Field{
		{Name: "title", Type: "text"},
		{Name: "title.keyword", Type: "keyword"},
		{Name: "seller.address.city", Type: "keyword"},
	})
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: path, Text: text})
	return s, path
}

func TestCompleteOffersFieldsAndDSLKeys(t *testing.T) {
	s, path := primedSource(t, "{\n  \"query\": {\n    \"ti\n}")
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 2, Col: 7})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	labels := map[string]string{}
	for _, it := range items {
		labels[it.Label] = it.Detail
	}
	if labels["title"] != "text" || labels["title.keyword"] != "keyword" {
		t.Errorf("mapping fields missing or missing their type badge: %v", labels)
	}
	if labels["time_zone"] != "dsl" {
		t.Errorf("DSL keys should be offered too, got %v", labels)
	}
	if _, ok := labels["match"]; ok {
		t.Errorf(`"ti" must not fuzzy-match "match": %v`, labels)
	}
}

func TestCompleteDottedPrefixSetsReplacePrefix(t *testing.T) {
	s, path := primedSource(t, "{\n  \"seller.add\n}")
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 13})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	for _, it := range items {
		if it.Label == "seller.address.city" {
			if it.ReplacePrefix != "seller." {
				t.Fatalf("ReplacePrefix = %q, want %q so the accept replaces the whole dotted prefix", it.ReplacePrefix, "seller.")
			}
			return
		}
	}
	t.Fatalf("seller.address.city not offered: %+v", items)
}

func TestCompleteOutsideStringOffersNothing(t *testing.T) {
	s, path := primedSource(t, "{\n  \"size\": 10\n}")
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 12})
	if err != nil || items != nil {
		t.Fatalf("Complete outside a string = %+v/%v, want nothing", items, err)
	}
}

func TestCompleteIgnoresForeignFiles(t *testing.T) {
	s := NewCompletionSource()
	if s.Exclusive("/x/main.go") || !s.Exclusive("/x/logs"+QueryExt) {
		t.Fatal("exclusive claim must cover exactly the query buffers")
	}
	items, err := s.Complete(context.Background(), complete.Request{Path: "/x/main.go", Line: 0, Col: 0})
	if err != nil || items != nil {
		t.Fatalf("Complete on a foreign file = %+v/%v, want nothing", items, err)
	}
}

func TestCompleteFetchesMappingOnCacheMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/products/_mapping" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(mappingFixture))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	cfg, _ := config.Load(config.Options{})
	cfg.Elasticsearch.Endpoints = []config.ESEndpoint{{Name: "prod", URL: srv.URL}}
	config.Set(cfg)

	path, err := QueryPath("prod", "products")
	if err != nil {
		t.Fatalf("QueryPath: %v", err)
	}
	resetFieldCache()
	s := NewCompletionSource()
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: path, Text: "{\n  \"pri\n}"})
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 1, Col: 6})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	found := false
	for _, it := range items {
		if it.Label == "price" && it.Detail == "double" {
			found = true
		}
	}
	if !found {
		t.Fatalf("price not offered after a mapping fetch: %+v", items)
	}
}

func TestCompleteReadsUneditedBufferFromDisk(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path, err := QueryPath("prod", "products")
	if err != nil {
		t.Fatalf("QueryPath: %v", err)
	}
	resetFieldCache()
	s := NewCompletionSource()
	PrimeFields("prod", "products", []Field{{Name: "title", Type: "text"}})
	// No Observe: the template on disk has `"match_all"` on line 2; complete
	// inside that string.
	items, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 2, Col: 10})
	if err != nil || len(items) == 0 {
		t.Fatalf("Complete without an Observe = %+v/%v, want items from the on-disk text", items, err)
	}
}
