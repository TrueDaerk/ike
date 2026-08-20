package openapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fetch_test.go covers URL discovery (#2009): a direct document URL, the
// well-known probe run under a base URL, and the failure modes the import
// dialog has to spell out — dead host, 404 everywhere, a body that is neither
// JSON nor YAML nor OpenAPI.

// specServer serves the mini spec at the given paths; everything else 404s.
// It records which paths were requested, in order.
func specServer(t *testing.T, body string, paths ...string) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	at := map[string]bool{}
	for _, p := range paths {
		at[p] = true
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		if !at[r.URL.Path] {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// fixture reads a testdata spec.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestDiscoverDirectURL (#2009): a URL naming the document is fetched as-is,
// without probing anything.
func TestDiscoverDirectURL(t *testing.T) {
	srv, seen := specServer(t, fixture(t, "petstore.yaml"), "/specs/petstore.yaml")
	d, err := Discover(context.Background(), srv.Client(), srv.URL+"/specs/petstore.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != srv.URL+"/specs/petstore.yaml" {
		t.Errorf("URL = %q", d.URL)
	}
	if len(d.Probed) != 0 {
		t.Errorf("a direct URL must not probe, probed %v", d.Probed)
	}
	if d.Spec == nil || len(d.Spec.Operations) == 0 {
		t.Fatal("the document must be parsed")
	}
	if d.Name() != "petstore.yaml" {
		t.Errorf("Name() = %q, want petstore.yaml", d.Name())
	}
	if got := len(*seen); got != 1 {
		t.Errorf("%d requests, want 1: %v", got, *seen)
	}
}

// TestDiscoverProbesWellKnownPaths (#2009): a base URL walks ProbePaths in
// order and stops at the first parseable document, reporting what it tried.
func TestDiscoverProbesWellKnownPaths(t *testing.T) {
	srv, seen := specServer(t, fixture(t, "petstore.yaml"), "/v3/api-docs")
	d, err := Discover(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != srv.URL+"/v3/api-docs" {
		t.Errorf("resolved %q, want %s/v3/api-docs", d.URL, srv.URL)
	}
	// Every earlier probe path was tried, in ProbePaths order, and nothing
	// after the hit was.
	want := ProbePaths[:6] // …up to and including /v3/api-docs
	if len(*seen) != len(want) {
		t.Fatalf("probed %v, want %v", *seen, want)
	}
	for i, p := range want {
		if (*seen)[i] != p {
			t.Errorf("probe %d = %q, want %q", i, (*seen)[i], p)
		}
	}
	if len(d.Probed) != len(want)-1 {
		t.Errorf("Probed = %v, want the %d paths before the hit", d.Probed, len(want)-1)
	}
	if d.Name() != "api-docs" {
		t.Errorf("Name() = %q, want api-docs", d.Name())
	}
}

// TestDiscoverProbesUnderPathPrefix (#2009): a base URL with a path prefix
// that is not a document probes *under* that prefix.
func TestDiscoverProbesUnderPathPrefix(t *testing.T) {
	srv, _ := specServer(t, fixture(t, "petstore.yaml"), "/service/openapi.json")
	d, err := Discover(context.Background(), srv.Client(), srv.URL+"/service")
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != srv.URL+"/service/openapi.json" {
		t.Errorf("resolved %q", d.URL)
	}
}

// TestDiscoverErrors (#2009): each failure mode produces its own readable
// message — the dialog shows nothing else.
func TestDiscoverErrors(t *testing.T) {
	notJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			_, _ = w.Write([]byte("<html>not a spec at all: [unclosed"))
			return
		}
		http.NotFound(w, r)
	}))
	defer notJSON.Close()
	swagger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"swagger":"2.0","paths":{}}`))
	}))
	defer swagger.Close()
	empty := httptest.NewServer(http.NotFoundHandler())
	defer empty.Close()
	serverErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer serverErr.Close()
	// A closed server's address is a host nothing listens on.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	cases := []struct {
		name string
		url  string
		want []string // substrings the message must carry
	}{
		{"dead host", deadURL, []string{"/openapi.json", "connect"}},
		{"dead host, direct document", deadURL + "/openapi.json", []string{"connect"}},
		{"404 everywhere", empty.URL, []string{"no OpenAPI document at", "/openapi.json", "/v3/api-docs"}},
		{"direct 404", empty.URL + "/spec.json", []string{"spec.json", "HTTP 404"}},
		{"direct 500", serverErr.URL + "/spec.json", []string{"HTTP 500"}},
		{"neither JSON nor YAML", notJSON.URL, []string{"/openapi.json", "cannot parse spec as JSON or YAML"}},
		{"parses but is not OpenAPI 3", swagger.URL, []string{"Swagger 2.0"}},
		{"not a URL at all", "https://", []string{"names no host"}},
		{"unsupported scheme", "ftp://example.com/spec.json", []string{"only http and https"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Discover(context.Background(), nil, tc.url)
			if err == nil {
				t.Fatalf("want an error, got %s", d.URL)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q must mention %q", err, w)
				}
			}
		})
	}
}

// TestDiscoverDeadHostStopsAtFirstProbe (#2009): an unreachable host must not
// cost one timeout per well-known path.
func TestDiscoverDeadHostStopsAtFirstProbe(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()

	_, err := Discover(context.Background(), nil, url)
	if err == nil {
		t.Fatal("want an error")
	}
	// The message names the first probe path only: the run aborted there.
	if !strings.Contains(err.Error(), ProbePaths[0]) || strings.Contains(err.Error(), ProbePaths[1]) {
		t.Errorf("error = %q, want the run to stop at %s", err, ProbePaths[0])
	}
}

// TestIsURL (#2009): only http(s) input takes the URL flow; everything else
// is a filesystem path.
func TestIsURL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"https://api.example.com", true},
		{"HTTP://api.example.com/openapi.json", true},
		{"  https://api.example.com  ", true},
		{"./openapi.yaml", false},
		{"/tmp/openapi.yaml", false},
		{"file:///tmp/openapi.yaml", false},
		{"~/specs/openapi.json", false},
		{"", false},
	} {
		if got := IsURL(tc.in); got != tc.want {
			t.Errorf("IsURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestImportDocumentFromDiscovery (#2009): the fetched bytes import into the
// chosen directory, named after the resolved URL, exactly like a file import.
func TestImportDocumentFromDiscovery(t *testing.T) {
	srv, _ := specServer(t, fixture(t, "petstore.yaml"), "/openapi.json")
	d, err := Discover(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res, err := ImportDocument(d.Data, d.Name(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "openapi.http"); res.HTTPFile != want {
		t.Errorf("wrote %s, want %s", res.HTTPFile, want)
	}
	if res.Operations == 0 {
		t.Error("the import must generate the document's operations")
	}
	for _, name := range []string{"openapi.http", "http-client.env.json", "http-client.private.env.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s beside the request file: %v", name, err)
		}
	}
}
