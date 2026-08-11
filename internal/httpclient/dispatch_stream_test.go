package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsStreamContentType(t *testing.T) {
	streaming := []string{
		"text/event-stream",
		"text/event-stream; charset=utf-8",
		"TEXT/EVENT-STREAM",
		"application/x-ndjson",
		"application/ndjson",
		"application/json-seq",
		"application/stream+json",
	}
	for _, ct := range streaming {
		if !IsStreamContentType(ct) {
			t.Errorf("IsStreamContentType(%q) = false, want true", ct)
		}
	}
	collected := []string{
		"", "application/json", "text/plain", "text/html",
		"application/octet-stream", // "stream" in the name is not streaming
		"application/xml",
	}
	for _, ct := range collected {
		if IsStreamContentType(ct) {
			t.Errorf("IsStreamContentType(%q) = true, want false", ct)
		}
	}
}

// streamRecorder collects the callback traffic of one DispatchStream run.
type streamRecorder struct {
	mu      sync.Mutex
	headers int
	status  string
	chunks  [][]byte
}

func (r *streamRecorder) callbacks() StreamCallbacks {
	return StreamCallbacks{
		OnHeaders: func(status string, _ int, _ string, _ http.Header) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.headers++
			r.status = status
		},
		OnChunk: func(chunk []byte) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.chunks = append(r.chunks, chunk)
		},
	}
}

func (r *streamRecorder) joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, c := range r.chunks {
		b.Write(c)
	}
	return b.String()
}

func TestDispatchStreamSSEDeliversChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, ev := range []string{"data: one\n\n", "data: two\n\n", "data: three\n\n"} {
			w.Write([]byte(ev))
			fl.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer srv.Close()

	rec := &streamRecorder{}
	req := parseOne(t, "GET "+srv.URL+"/events\n")
	resp, err := DispatchStream(context.Background(), req, noConfig, rec.callbacks())
	if err != nil {
		t.Fatal(err)
	}
	if rec.headers != 1 || !strings.Contains(rec.status, "200") {
		t.Errorf("OnHeaders: %d calls, status %q", rec.headers, rec.status)
	}
	want := "data: one\n\ndata: two\n\ndata: three\n\n"
	if got := rec.joined(); got != want {
		t.Errorf("chunks: %q, want %q", got, want)
	}
	if len(rec.chunks) < 2 {
		t.Errorf("expected incremental chunks, got %d", len(rec.chunks))
	}
	if string(resp.Body) != want {
		t.Errorf("final body: %q", resp.Body)
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", resp.Warnings)
	}
}

func TestDispatchStreamNonStreamCollects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	rec := &streamRecorder{}
	req := parseOne(t, "GET "+srv.URL+"/plain\n")
	resp, err := DispatchStream(context.Background(), req, noConfig, rec.callbacks())
	if err != nil {
		t.Fatal(err)
	}
	if rec.headers != 0 || len(rec.chunks) != 0 {
		t.Errorf("callbacks fired for a non-stream: headers=%d chunks=%d", rec.headers, len(rec.chunks))
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Errorf("body: %q", resp.Body)
	}
}

func TestDispatchStreamCancelKeepsPartial(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte("{\"n\":1}\n"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	rec := &streamRecorder{}
	cb := rec.callbacks()
	inner := cb.OnChunk
	cb.OnChunk = func(chunk []byte) {
		inner(chunk)
		cancel() // abort as soon as the first chunk is in
	}

	req := parseOne(t, "GET "+srv.URL+"/ndjson\n")
	resp, err := DispatchStream(ctx, req, noConfig, cb)
	if err != nil {
		t.Fatalf("cancel must keep the partial response, got error: %v", err)
	}
	if string(resp.Body) != "{\"n\":1}\n" {
		t.Errorf("partial body: %q", resp.Body)
	}
	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w, "canceled") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing cancel warning: %v", resp.Warnings)
	}
}

func TestDispatchStreamIdleTimeoutKeepsPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: hello\n\n"))
		w.(http.Flusher).Flush()
		select { // then go quiet until the client gives up
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	opts := noConfig
	opts.StreamIdleTimeout = 100 * time.Millisecond
	req := parseOne(t, "GET "+srv.URL+"/quiet\n")
	start := time.Now()
	resp, err := DispatchStream(context.Background(), req, opts, StreamCallbacks{})
	if err != nil {
		t.Fatalf("idle timeout must keep the partial response, got error: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("idle timeout did not fire in time")
	}
	if string(resp.Body) != "data: hello\n\n" {
		t.Errorf("partial body: %q", resp.Body)
	}
	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w, "idle") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing idle warning: %v", resp.Warnings)
	}
}

func TestDispatchStreamTruncatesAtMaxBodyBytes(t *testing.T) {
	chunk := strings.Repeat("x", 1<<20) + "\n" // ~1 MiB lines
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 12; i++ { // 12 MiB > MaxBodyBytes (10 MiB)
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
			fl.Flush()
		}
	}))
	defer srv.Close()

	rec := &streamRecorder{}
	req := parseOne(t, "GET "+srv.URL+"/big\n")
	resp, err := DispatchStream(context.Background(), req, noConfig, rec.callbacks())
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Truncated || len(resp.Body) != MaxBodyBytes {
		t.Errorf("truncation: truncated=%v len=%d", resp.Truncated, len(resp.Body))
	}
	if got := len(rec.joined()); got != MaxBodyBytes {
		t.Errorf("chunks past the cap were delivered: %d bytes", got)
	}
	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w, "truncated") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing truncation warning: %v", resp.Warnings)
	}
}
