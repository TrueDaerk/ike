package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/httpfile"
)

// redirectServer answers /a with a 301 to /b, /b with a 302 to /c and /c with
// a 200 — the two-redirect chain the pane's acceptance case is written for.
func redirectServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/a", func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, srv.URL+"/b", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, srv.URL+"/c", http.StatusFound)
	})
	mux.HandleFunc("/c", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("arrived"))
	})
	return srv
}

func dispatchTarget(t *testing.T, method, target, body string, opts Options) *Response {
	t.Helper()
	req := &httpfile.Request{Method: method, Target: target, Body: body}
	resp, err := Dispatch(context.Background(), req, opts)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	return resp
}

// TestRedirectChainRecorded: a 301 → 302 → 200 records three hops, the
// answering one last, with the method, URL, status and Location of each
// (#2716).
func TestRedirectChainRecorded(t *testing.T) {
	srv := redirectServer(t)
	resp := dispatchTarget(t, "GET", srv.URL+"/a", "", Options{DisableConfig: true})

	if got := resp.RedirectCount(); got != 2 {
		t.Fatalf("RedirectCount = %d, want 2 (hops %+v)", got, resp.Redirects)
	}
	if len(resp.Redirects) != 3 {
		t.Fatalf("hops = %d, want 3", len(resp.Redirects))
	}
	want := []struct {
		url    string
		status int
		loc    string
	}{
		{srv.URL + "/a", http.StatusMovedPermanently, srv.URL + "/b"},
		{srv.URL + "/b", http.StatusFound, srv.URL + "/c"},
		{srv.URL + "/c", http.StatusOK, ""},
	}
	for i, w := range want {
		h := resp.Redirects[i]
		if h.URL != w.url || h.Status != w.status || h.Location != w.loc {
			t.Errorf("hop %d = %s %d loc=%q, want %s %d loc=%q", i, h.URL, h.Status, h.Location, w.url, w.status, w.loc)
		}
		if h.Method != "GET" {
			t.Errorf("hop %d method = %q, want GET", i, h.Method)
		}
	}
	if resp.FinalURL != srv.URL+"/c" {
		t.Errorf("FinalURL = %q, want %q", resp.FinalURL, srv.URL+"/c")
	}
	if resp.RedirectsOff {
		t.Error("RedirectsOff set on a followed chain")
	}
}

// TestRedirectTraceAttributedPerHop: the httptrace facts land on the hop they
// belong to rather than all on the last one — every hop of a chain over one
// test server names a connected address, and the later hops say they reused
// the connection the first one opened (#2716).
func TestRedirectTraceAttributedPerHop(t *testing.T) {
	srv := redirectServer(t)
	resp := dispatchTarget(t, "GET", srv.URL+"/a", "", Options{DisableConfig: true})

	for i, h := range resp.Redirects {
		if h.RemoteAddr == "" {
			t.Errorf("hop %d has no RemoteAddr", i)
			continue
		}
		if !strings.Contains(srv.URL, h.RemoteAddr) {
			t.Errorf("hop %d RemoteAddr = %q, want the test server's address in %q", i, h.RemoteAddr, srv.URL)
		}
		if !h.HasConn() {
			t.Errorf("hop %d reports no connection facts", i)
		}
	}
	// The first hop opened the connection; a keep-alive chain over the same
	// host rides it, which is exactly the fact the block exists to show.
	if resp.Redirects[0].Reused {
		t.Error("first hop reports a reused connection")
	}
	if !resp.Redirects[1].Reused || !resp.Redirects[2].Reused {
		t.Errorf("later hops not marked reused: %+v", resp.Redirects[1:])
	}
}

// TestDirectResponseHasNoChain: an answer that redirected nowhere records no
// hops at all, so "empty" keeps meaning "no redirect happened" (#2716).
func TestDirectResponseHasNoChain(t *testing.T) {
	srv := redirectServer(t)
	resp := dispatchTarget(t, "GET", srv.URL+"/c", "", Options{DisableConfig: true})

	if len(resp.Redirects) != 0 {
		t.Errorf("Redirects = %+v, want none", resp.Redirects)
	}
	if resp.RedirectCount() != 0 {
		t.Errorf("RedirectCount = %d, want 0", resp.RedirectCount())
	}
	if resp.FinalURL != srv.URL+"/c" {
		t.Errorf("FinalURL = %q, want the request URL %q", resp.FinalURL, srv.URL+"/c")
	}
}

// TestRedirectMethodChangeRecorded: a POST answered 303 continues as a GET,
// and the step that caused it says so (#2716).
func TestRedirectMethodChangeRecorded(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/post", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", srv.URL+"/done")
		w.WriteHeader(http.StatusSeeOther)
	})
	mux.HandleFunc("/done", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Method))
	})

	resp := dispatchTarget(t, "POST", srv.URL+"/post", "payload", Options{DisableConfig: true})
	if len(resp.Redirects) != 2 {
		t.Fatalf("hops = %d, want 2: %+v", len(resp.Redirects), resp.Redirects)
	}
	first, second := resp.Redirects[0], resp.Redirects[1]
	if first.Method != "POST" || !first.MethodChanged {
		t.Errorf("first hop = %s changed=%v, want POST changed=true", first.Method, first.MethodChanged)
	}
	if second.Method != "GET" || second.MethodChanged {
		t.Errorf("second hop = %s changed=%v, want GET changed=false", second.Method, second.MethodChanged)
	}
	if got := string(resp.Body); got != "GET" {
		t.Errorf("server saw %q, want GET", got)
	}
}

// TestRedirectNotFollowedWithLocationOff: `.curlrc` turning -L off leaves the
// 3xx as the answer, records no chain and says the redirect was declined
// (#2716).
func TestRedirectNotFollowedWithLocationOff(t *testing.T) {
	srv := redirectServer(t)
	dir := t.TempDir()
	rc := filepath.Join(dir, ".curlrc")
	if err := os.WriteFile(rc, []byte("location = off\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := dispatchTarget(t, "GET", srv.URL+"/a", "", Options{CurlrcPath: rc})
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", resp.StatusCode)
	}
	if len(resp.Redirects) != 0 {
		t.Errorf("Redirects = %+v, want none", resp.Redirects)
	}
	if !resp.RedirectsOff {
		t.Error("RedirectsOff not set on a declined redirect")
	}
	if got := resp.Headers.Get("Location"); got != srv.URL+"/b" {
		t.Errorf("Location = %q, want %q", got, srv.URL+"/b")
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("warnings = %v, want none — location=off is understood", resp.Warnings)
	}
}

// TestRedirectDeclinedOnlyWhenOneArrives: a direct answer under -L off does
// not claim a redirect was declined (#2716).
func TestRedirectDeclinedOnlyWhenOneArrives(t *testing.T) {
	srv := redirectServer(t)
	dir := t.TempDir()
	rc := filepath.Join(dir, ".curlrc")
	if err := os.WriteFile(rc, []byte("--no-location\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := dispatchTarget(t, "GET", srv.URL+"/c", "", Options{CurlrcPath: rc})
	if resp.RedirectsOff {
		t.Error("RedirectsOff set although nothing redirected")
	}
}

// TestCurlrcLocationSpellings: the bare option is on, curl's negative
// spellings are off (#2716).
func TestCurlrcLocationSpellings(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"-L", true},
		{"location", true},
		{"location = on", true},
		{"location = off", false},
		{"location false", false},
		{"--no-location", false},
	} {
		dir := t.TempDir()
		rc := filepath.Join(dir, ".curlrc")
		if err := os.WriteFile(rc, []byte(tc.line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := parseCurlrc(rc)
		if cfg.FollowRedirect == nil {
			t.Errorf("%q: FollowRedirect unset", tc.line)
			continue
		}
		if *cfg.FollowRedirect != tc.want {
			t.Errorf("%q: FollowRedirect = %v, want %v", tc.line, *cfg.FollowRedirect, tc.want)
		}
		if len(cfg.Warnings) != 0 {
			t.Errorf("%q: warnings = %v, want none", tc.line, cfg.Warnings)
		}
	}
}

// TestRedirectLimitStopsTheChain: replacing Go's redirect policy keeps its
// limit, and the recorded chain does not grow past it (#2716).
func TestRedirectLimitStopsTheChain(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", srv.URL+r.URL.Path+"x")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	req := &httpfile.Request{Method: "GET", Target: srv.URL + "/loop"}
	_, err := Dispatch(context.Background(), req, Options{DisableConfig: true})
	if err == nil {
		t.Fatal("an endless redirect loop did not fail")
	}
	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Errorf("error = %v, want Go's ten-redirect wording", err)
	}
}

// TestRedirectAssertionSubjects: `redirects` and `finalUrl` are assertable
// (#2716, #2546).
func TestRedirectAssertionSubjects(t *testing.T) {
	srv := redirectServer(t)
	req := &httpfile.Request{Method: "GET", Target: srv.URL + "/a", Assertions: []httpfile.Assertion{
		{Subject: httpfile.AssertRedirects, Op: httpfile.OpEq, Expected: "2"},
		{Subject: httpfile.AssertFinalURL, Op: httpfile.OpContains, Expected: "/c"},
		{Subject: httpfile.AssertRedirects, Op: httpfile.OpEq, Expected: "0"},
	}}
	resp, err := Dispatch(context.Background(), req, Options{DisableConfig: true})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(resp.Assertions) != 3 {
		t.Fatalf("assertions = %d, want 3", len(resp.Assertions))
	}
	if !resp.Assertions[0].Pass || !resp.Assertions[1].Pass {
		t.Errorf("redirects/finalUrl assertions failed: %+v", resp.Assertions[:2])
	}
	if resp.Assertions[2].Pass {
		t.Error("redirects == 0 passed on a two-redirect chain")
	}
	if resp.Assertions[2].Actual != "2" {
		t.Errorf("actual = %q, want 2", resp.Assertions[2].Actual)
	}
}

// TestHopHostAndTruncationInputs: the rendered chain names the host once, so
// a hop must be able to answer what its host is (#2716).
func TestHopHost(t *testing.T) {
	if got := (Hop{URL: "https://www.example.com:8443/deep/path"}).Host(); got != "www.example.com" {
		t.Errorf("Host = %q, want www.example.com", got)
	}
	if got := (Hop{URL: "::not a url"}).Host(); got != "" {
		t.Errorf("Host = %q, want empty for an unreadable URL", got)
	}
}
