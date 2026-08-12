package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// seen records what a test server received.
type seen struct {
	method string
	path   string
	query  string
	header http.Header
	host   string
	body   string
}

// recordingServer answers every request with 200 and captures the last one.
func recordingServer(t *testing.T, got *seen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = seen{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			header: r.Header.Clone(),
			host:   r.Host,
			body:   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDispatchCapturesRequestSnapshot: every dispatch records the request as
// it went out — substituted, .curlrc/.netrc applied (#1832).
func TestDispatchCapturesRequestSnapshot(t *testing.T) {
	var got seen
	srv := recordingServer(t, &got)
	req := parseOne(t, "### one\nPOST "+srv.URL+"/things?v=1\n"+
		"Content-Type: application/json\nAuthorization: Bearer ${TOKEN}\n\n"+
		`{"name":"${NAME}"}`)

	env := map[string]string{"TOKEN": "s3cret", "NAME": "example"}
	resp, err := Dispatch(context.Background(), req, Options{
		DisableConfig: true,
		Lookup:        func(k string) (string, bool) { v, ok := env[k]; return v, ok },
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := resp.Request
	if snap == nil {
		t.Fatal("the response must carry the as-sent request")
	}
	if snap.Method != "POST" {
		t.Errorf("method: %q, want POST", snap.Method)
	}
	if want := srv.URL + "/things?v=1"; snap.URL != want {
		t.Errorf("url: %q, want %q", snap.URL, want)
	}
	if v := snap.Headers.Get("Authorization"); v != "Bearer s3cret" {
		t.Errorf("authorization: %q, want the substituted value", v)
	}
	if string(snap.Body) != `{"name":"example"}` {
		t.Errorf("body: %q, want the substituted body", snap.Body)
	}
}

// TestResendSendsSnapshotVerbatim: a stored snapshot goes out unchanged even
// when the environment that produced it resolves differently now — the
// re-send path never substitutes anything (#1832).
func TestResendSendsSnapshotVerbatim(t *testing.T) {
	var got seen
	srv := recordingServer(t, &got)
	req := parseOne(t, "### one\nPUT "+srv.URL+"/things/7?v=1\n"+
		"Content-Type: application/json\nX-Token: ${TOKEN}\n\n"+
		`{"name":"first"}`)

	resp, err := Dispatch(context.Background(), req, Options{
		DisableConfig: true,
		Lookup:        func(string) (string, bool) { return "old", true },
	})
	if err != nil {
		t.Fatal(err)
	}
	first := got

	// The world moved on: a different variable value, and the caller has no
	// request object at all — only the snapshot.
	again, err := Resend(context.Background(), "one", resp.Request, Options{
		DisableConfig: true,
		Lookup:        func(string) (string, bool) { return "new", true },
	}, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != first.method || got.path != first.path || got.query != first.query {
		t.Errorf("re-sent %s %s?%s, want %s %s?%s", got.method, got.path, got.query,
			first.method, first.path, first.query)
	}
	if got.header.Get("X-Token") != "old" {
		t.Errorf("X-Token: %q, want the stored value old", got.header.Get("X-Token"))
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type: %q", got.header.Get("Content-Type"))
	}
	if got.body != first.body {
		t.Errorf("body: %q, want %q", got.body, first.body)
	}
	if again.RequestKey != "one" {
		t.Errorf("request key: %q, want one", again.RequestKey)
	}
	if again.Request == nil || again.Request.URL != resp.Request.URL {
		t.Error("the re-sent response must carry the same snapshot again")
	}
}

// TestResendRestoresHostHeader: a `Host:` line is not part of Go's header map,
// so the snapshot carries it explicitly and the re-send puts it back (#1832).
func TestResendRestoresHostHeader(t *testing.T) {
	var got seen
	srv := recordingServer(t, &got)
	req := parseOne(t, "### one\nGET "+srv.URL+"/a\nHost: virtual.example\n")

	resp, err := Dispatch(context.Background(), req, Options{DisableConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Request.Headers.Get("Host") != "virtual.example" {
		t.Fatalf("snapshot host: %q", resp.Request.Headers.Get("Host"))
	}
	if _, err := Resend(context.Background(), "one", resp.Request,
		Options{DisableConfig: true}, StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	if got.host != "virtual.example" {
		t.Errorf("re-sent Host: %q, want virtual.example", got.host)
	}
	if got.header.Get("Host") != "" {
		t.Errorf("Host must not be duplicated into the header map: %q", got.header.Get("Host"))
	}
}

// TestResendWithoutSnapshotFails: a legacy history entry has nothing to send,
// and the error names the request (#1832).
func TestResendWithoutSnapshotFails(t *testing.T) {
	_, err := Resend(context.Background(), "one", nil, Options{DisableConfig: true}, StreamCallbacks{})
	if err == nil {
		t.Fatal("re-sending without a snapshot must fail")
	}
	if !strings.Contains(err.Error(), "one") {
		t.Errorf("error %q must name the request", err)
	}
}

// TestResendReopensStream: a re-sent streaming endpoint streams again — the
// documented decision for #1776 responses (#1832).
func TestResendReopensStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 2; i++ {
			w.Write([]byte("data: tick\n\n"))
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()

	snap := &RequestSnapshot{Method: "GET", URL: srv.URL + "/events", Headers: http.Header{}}
	headers := 0
	var chunks int
	resp, err := Resend(context.Background(), "one", snap,
		Options{DisableConfig: true, StreamIdleTimeout: 2 * time.Second},
		StreamCallbacks{
			OnHeaders: func(string, int, string, http.Header) { headers++ },
			OnChunk:   func([]byte) { chunks++ },
		})
	if err != nil {
		t.Fatal(err)
	}
	if headers != 1 || chunks == 0 {
		t.Fatalf("stream callbacks: %d headers, %d chunks — a re-sent stream must stream", headers, chunks)
	}
	if !strings.Contains(string(resp.Body), "data: tick") {
		t.Errorf("body: %q", resp.Body)
	}
}
