package httpclient

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// bodylimit_test.go covers the `# @stream` and `# @max-body` directives at
// dispatch time (#3117): forced spooling, the early head, the cap, the
// closed connection and what captures/assertions see of a truncated body.

// patternBody is n bytes whose every KiB is distinguishable, so "the first
// 1024 bytes" can be checked byte for byte.
func patternBody(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a' + byte(i/1024%26)
	}
	return b
}

func TestStreamDirectiveSpoolsSmallBody(t *testing.T) {
	body := patternBody(10 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	for name, run := range map[string]func(*testing.T) *Response{
		"Dispatch": func(t *testing.T) *Response {
			resp, err := Dispatch(context.Background(), parseOne(t, "# @stream\nGET "+srv.URL+"/\n"), noConfig)
			if err != nil {
				t.Fatal(err)
			}
			return resp
		},
		"DispatchStream": func(t *testing.T) *Response {
			rec := &streamRecorder{}
			resp, err := DispatchStream(context.Background(), parseOne(t, "# @stream\nGET "+srv.URL+"/\n"), noConfig, rec.callbacks())
			if err != nil {
				t.Fatal(err)
			}
			if rec.headers != 1 || rec.joined() != string(body) {
				t.Errorf("live view: headers=%d bytes=%d, want the head handed out while arriving", rec.headers, len(rec.joined()))
			}
			return resp
		},
	} {
		t.Run(name, func(t *testing.T) {
			resp := run(t)
			if !resp.Spooled() {
				t.Fatal("a @stream body below SpoolThreshold must still be spooled")
			}
			if !bytes.Equal(resp.Body, body) || resp.BodySize != len(body) || resp.Truncated {
				t.Errorf("head=%d size=%d truncated=%v", len(resp.Body), resp.BodySize, resp.Truncated)
			}
			data, err := os.ReadFile(resp.SpoolPath)
			if err != nil || !bytes.Equal(data, body) {
				t.Errorf("spool file = %d bytes (%v), want the whole body", len(data), err)
			}
		})
	}
}

// The head reaches the live view before the rest of the body has arrived.
func TestStreamDirectiveHandsOutHeadEarly(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("head line\n"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
		w.Write([]byte("tail line\n"))
	}))
	defer srv.Close()

	gotHead := make(chan string, 1)
	var once sync.Once
	cb := StreamCallbacks{OnChunk: func(c []byte) { once.Do(func() { gotHead <- string(c) }) }}
	done := make(chan *Response, 1)
	go func() {
		resp, err := DispatchStream(context.Background(), parseOne(t, "# @stream\nGET "+srv.URL+"/\n"), noConfig, cb)
		if err != nil {
			t.Error(err)
		}
		done <- resp
	}()
	select {
	case h := <-gotHead:
		if h != "head line\n" {
			t.Errorf("first chunk = %q", h)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the head was not handed out while the body was still arriving")
	}
	select {
	case <-done:
		t.Fatal("the dispatch finished before the server sent the rest")
	default:
	}
	close(release)
	resp := <-done
	if resp == nil || string(resp.Body) != "head line\ntail line\n" || !resp.Spooled() {
		t.Fatalf("final response = %+v", resp)
	}
}

// A forced stream hands out the head only: the remainder goes to the spool
// file, not into the live view's in-memory rows.
func TestStreamDirectiveLiveViewGetsHeadOnly(t *testing.T) {
	body := patternBody(SpoolThreshold + 300<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()
	rec := &streamRecorder{}
	resp, err := DispatchStream(context.Background(), parseOne(t, "# @stream\nGET "+srv.URL+"/\n"), noConfig, rec.callbacks())
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.joined(); got != string(body[:SpoolThreshold]) {
		t.Errorf("live view got %d bytes, want exactly the %d-byte head", len(got), SpoolThreshold)
	}
	if resp.BodySize != len(body) || !resp.Spooled() {
		t.Errorf("size=%d spooled=%v", resp.BodySize, resp.Spooled())
	}
}

// Without the directive a plain body takes the collect path: no live view.
func TestNoStreamDirectiveKeepsCollectMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(patternBody(10 << 10))
	}))
	defer srv.Close()
	rec := &streamRecorder{}
	resp, err := DispatchStream(context.Background(), parseOne(t, "GET "+srv.URL+"/\n"), noConfig, rec.callbacks())
	if err != nil {
		t.Fatal(err)
	}
	if rec.headers != 0 || resp.Spooled() {
		t.Errorf("headers=%d spooled=%v, want the plain collect path", rec.headers, resp.Spooled())
	}
}

// capServer writes 10 KiB in flushed 1 KiB pieces and then holds the
// response open; closed reports whether the client hung up on it.
func capServer(t *testing.T) (*httptest.Server, <-chan bool) {
	t.Helper()
	closed := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		body := patternBody(10 << 10)
		for i := 0; i < len(body); i += 1024 {
			if _, err := w.Write(body[i : i+1024]); err != nil {
				closed <- true
				return
			}
			w.(http.Flusher).Flush()
		}
		select {
		case <-r.Context().Done():
			closed <- true
		case <-time.After(5 * time.Second):
			closed <- false
		}
	}))
	t.Cleanup(srv.Close)
	return srv, closed
}

func TestMaxBodyDirectiveCutsAndClosesConnection(t *testing.T) {
	for name, src := range map[string]string{
		"collect": "# @max-body 1kb\n",
		"stream":  "# @stream\n# @max-body 1kb\n",
	} {
		t.Run(name, func(t *testing.T) {
			srv, closed := capServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			resp, err := DispatchStream(ctx, parseOne(t, src+"GET "+srv.URL+"/\n"), noConfig, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			want := patternBody(10 << 10)[:1024]
			full, _ := resp.FullBody()
			if !bytes.Equal(full, want) || resp.BodySize != 1024 || !resp.Truncated {
				t.Errorf("body=%d size=%d truncated=%v, want exactly the first 1024 bytes, truncated", len(full), resp.BodySize, resp.Truncated)
			}
			if !strings.Contains(strings.Join(resp.Warnings, "\n"), "@max-body 1kb") {
				t.Errorf("warnings %v must name the directive", resp.Warnings)
			}
			select {
			case c := <-closed:
				if !c {
					t.Error("the server never saw the connection close")
				}
			case <-time.After(4 * time.Second):
				t.Error("the server never saw the connection close")
			}
		})
	}
}

// With a spooled body the spool file stops growing at the limit.
func TestMaxBodyDirectiveCapsSpoolFile(t *testing.T) {
	const limit = 1536 << 10 // 1.5 MiB: past SpoolThreshold
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(patternBody(3 << 20))
	}))
	defer srv.Close()
	resp, err := Dispatch(context.Background(), parseOne(t, "# @max-body 1536kb\nGET "+srv.URL+"/\n"), noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Spooled() || !resp.Truncated || len(resp.Body) != SpoolThreshold || resp.BodySize != limit {
		t.Fatalf("spooled=%v truncated=%v head=%d size=%d", resp.Spooled(), resp.Truncated, len(resp.Body), resp.BodySize)
	}
	info, err := os.Stat(resp.SpoolPath)
	if err != nil || info.Size() != limit {
		t.Errorf("spool file size = %v (%v), want %d", info.Size(), err, limit)
	}
}

// The directive replaces MaxBodyBytes in both directions.
func TestMaxBodyDirectiveOverridesDefaultCap(t *testing.T) {
	p := &prepared{snapshot: &RequestSnapshot{MaxBody: 64 << 20}}
	if s := p.newSink("application/json"); s.max != 64<<20 || !s.directive {
		t.Errorf("max=%d directive=%v, want the directive's 64 MiB", s.max, s.directive)
	}
	p = &prepared{snapshot: &RequestSnapshot{}}
	if s := p.newSink(""); s.max != MaxBodyBytes || s.directive || s.force {
		t.Errorf("max=%d directive=%v force=%v, want the defaults", s.max, s.directive, s.force)
	}
}

// Assertions see the bytes that were read (captures read the same FullBody).
func TestTruncatedBodyFeedsAssertions(t *testing.T) {
	srv, _ := capServer(t)
	src := "# @max-body 1kb\n" +
		"# @assert body contains aaaa\n" +
		"# @assert body contains bbbb\n" +
		"GET " + srv.URL + "/\n"
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	resp, err := Dispatch(ctx, parseOne(t, src), noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Assertions) != 2 || !resp.Assertions[0].Pass || resp.Assertions[1].Pass {
		t.Fatalf("assertions = %+v, want the first KiB to match and the second to be gone", resp.Assertions)
	}
}

// A re-send reads the body the way the original did.
func TestResendKeepsBodyDirectives(t *testing.T) {
	srv, _ := capServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	resp, err := Dispatch(ctx, parseOne(t, "# @stream\n# @max-body 2kb\nGET "+srv.URL+"/\n"), noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Request.Stream || resp.Request.MaxBody != 2048 {
		t.Fatalf("snapshot = %+v, want the directives recorded", resp.Request)
	}
	srv2, _ := capServer(t)
	snap := resp.Request.Clone()
	snap.URL = srv2.URL + "/"
	again, err := Resend(ctx, "0", snap, noConfig, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Truncated || again.BodySize != 2048 || !again.Spooled() {
		t.Errorf("re-send: truncated=%v size=%d spooled=%v", again.Truncated, again.BodySize, again.Spooled())
	}
}
