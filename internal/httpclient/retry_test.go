package httpclient

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// retry_test.go covers the `# @retry` series (#3116): which outcomes are
// retried, the pause between attempts, the cancel, and what the final
// Response records about the earlier attempts.

// scriptedServer answers its requests with the given status codes in turn
// (the last one repeating) and counts the requests; each answer echoes the
// request body so a re-sent body can be checked.
func scriptedServer(t *testing.T, codes ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		if i >= len(codes) {
			i = len(codes) - 1
		}
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(codes[i])
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func retryDispatch(t *testing.T, src string, opts Options) (*Response, error) {
	t.Helper()
	opts.DisableConfig = true
	return DispatchStream(context.Background(), timeoutRequest(t, src), opts, StreamCallbacks{})
}

func TestRetryEndsWithSuccess(t *testing.T) {
	srv, hits := scriptedServer(t, 503, 503, 200)
	resp, err := retryDispatch(t, "# @retry 3\nPOST "+srv.URL+"/\n\npayload\n", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || hits.Load() != 3 {
		t.Fatalf("status %d after %d requests, want 200 after 3", resp.StatusCode, hits.Load())
	}
	if got := resp.AttemptLabel(); got != "attempt 3/4" {
		t.Errorf("AttemptLabel = %q, want attempt 3/4", got)
	}
	if len(resp.Attempts) != 2 || resp.Attempts[0].StatusCode != 503 || resp.Attempts[1].Outcome() != "503 Service Unavailable" {
		t.Errorf("Attempts = %+v, want two 503s", resp.Attempts)
	}
	// Every attempt carried the body, not just the first one.
	if string(resp.Body) != "payload" {
		t.Errorf("final body = %q, want the echoed payload", resp.Body)
	}
}

func TestRetryExhaustedKeepsLastResponse(t *testing.T) {
	srv, hits := scriptedServer(t, 503, 503, 200)
	resp, err := retryDispatch(t, "# @retry 1\nGET "+srv.URL+"/\n", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 503 || hits.Load() != 2 || resp.AttemptLabel() != "attempt 2/2" {
		t.Fatalf("status %d, %d requests, %q; want the second 503 at attempt 2/2", resp.StatusCode, hits.Load(), resp.AttemptLabel())
	}
}

func TestRetryOnlyOnNamedConditions(t *testing.T) {
	srv, hits := scriptedServer(t, 503, 200)
	resp, err := retryDispatch(t, "# @retry 3 on 404\nGET "+srv.URL+"/\n", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 503 || hits.Load() != 1 || resp.AttemptLabel() != "attempt 1/4" {
		t.Fatalf("status %d after %d requests (%q), want one 503", resp.StatusCode, hits.Load(), resp.AttemptLabel())
	}
	// The default conditions never retry a 4xx.
	srv2, hits2 := scriptedServer(t, 404, 200)
	if resp, _ := retryDispatch(t, "# @retry 3\nGET "+srv2.URL+"/\n", Options{}); resp.StatusCode != 404 || hits2.Load() != 1 {
		t.Fatalf("default conditions retried a 404: status %d, %d requests", resp.StatusCode, hits2.Load())
	}
}

func TestRetryOnTimeout(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			select { // the first attempt hangs past its deadline
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	resp, err := retryDispatch(t, "# @timeout 100ms\n# @retry 2 on timeout\nGET "+srv.URL+"/\n", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || len(resp.Attempts) != 1 || !resp.Attempts[0].TimedOut {
		t.Fatalf("status %d, attempts %+v; want 200 after one timed-out attempt", resp.StatusCode, resp.Attempts)
	}
	if resp.Attempts[0].Outcome() != "timed out" {
		t.Errorf("Outcome = %q", resp.Attempts[0].Outcome())
	}
}

// refusedURL is an address nothing listens on.
func refusedURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return "http://" + addr + "/"
}

func TestRetryOnErrorRetriesRefusedConnection(t *testing.T) {
	url := refusedURL(t)
	var attempts []int
	req := timeoutRequest(t, "# @retry 2 on error\nGET "+url+"\n")
	_, err := DispatchStream(context.Background(), req, Options{DisableConfig: true},
		StreamCallbacks{OnAttempt: func(n, max int) { attempts = append(attempts, n*10+max) }})
	var re *RetryError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want a RetryError", err)
	}
	if re.Label() != "attempt 3/3" || len(re.Attempts) != 2 || !strings.Contains(re.Attempts[0].Outcome(), "refused") {
		t.Fatalf("RetryError %q, attempts %+v", re.Label(), re.Attempts)
	}
	if len(attempts) != 2 || attempts[0] != 23 || attempts[1] != 33 {
		t.Errorf("OnAttempt saw %v, want attempts 2/3 and 3/3", attempts)
	}
	// Not named in the conditions: one attempt only, and the plain error.
	req = timeoutRequest(t, "# @retry 2 on 5xx\nGET "+url+"\n")
	_, err = DispatchStream(context.Background(), req, Options{DisableConfig: true}, StreamCallbacks{})
	if !errors.As(err, &re) || len(re.Attempts) != 0 {
		t.Fatalf("err = %v, want a single attempt", err)
	}
}

func TestRetryTimeoutErrorStaysATimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	_, err := retryDispatch(t, "# @timeout 50ms\n# @retry 1\nGET "+srv.URL+"/\n", Options{})
	var te *TimeoutError
	if !errors.As(err, &te) || !strings.Contains(err.Error(), "attempt 2/2") {
		t.Fatalf("err = %v, want a TimeoutError at attempt 2/2", err)
	}
}

func TestRetryHonoursDelay(t *testing.T) {
	srv, _ := scriptedServer(t, 503, 503, 200)
	start := time.Now()
	resp, err := retryDispatch(t, "# @retry 3 150ms\nGET "+srv.URL+"/\n", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < 300*time.Millisecond {
		t.Errorf("series took %v, want at least two 150ms pauses", took)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestRetryCancelDuringDelay(t *testing.T) {
	srv, hits := scriptedServer(t, 503)
	ctx, cancel := context.WithCancel(context.Background())
	req := timeoutRequest(t, "# @retry 5 10s\nGET "+srv.URL+"/\n")
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := DispatchStream(ctx, req, Options{DisableConfig: true}, StreamCallbacks{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancel", err)
	}
	if hits.Load() != 1 || time.Since(start) > 5*time.Second {
		t.Fatalf("%d requests in %v, want the series to stop in the pause", hits.Load(), time.Since(start))
	}
}

func TestRetryCancelDuringAttempt(t *testing.T) {
	var hits atomic.Int32
	var once sync.Once
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		once.Do(func() { close(started) })
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	req := timeoutRequest(t, "# @retry 5\nGET "+srv.URL+"/\n")
	_, err := DispatchStream(ctx, req, Options{DisableConfig: true}, StreamCallbacks{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancel", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("%d requests, want the cancel to end the series", hits.Load())
	}
}

// A retried attempt that announces a stream is collected, never shown live:
// only the final attempt's headers reach the view.
func TestRetryDoesNotStreamRetriedAttempt(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if n.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer srv.Close()
	var heads []int
	req := timeoutRequest(t, "# @retry 1\nGET "+srv.URL+"/\n")
	resp, err := DispatchStream(context.Background(), req, Options{DisableConfig: true},
		StreamCallbacks{OnHeaders: func(_ string, code int, _ string, _ http.Header) { heads = append(heads, code) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 1 || heads[0] != 200 || resp.StatusCode != 200 {
		t.Fatalf("OnHeaders saw %v, final %d; want only the 200", heads, resp.StatusCode)
	}
}

// Captures and assertions see the final attempt only.
func TestRetryCapturesAndAssertionsUseFinalAttempt(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"id":"first"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"final"}`))
	}))
	defer srv.Close()
	src := "# @retry 2\n# @capture id = .id\n# @assert status == 200\nGET " + srv.URL + "/\n"
	resp, err := retryDispatch(t, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Captures) != 1 || resp.Captures[0].Value != "final" {
		t.Errorf("captures = %+v, want id=final", resp.Captures)
	}
	if resp.AssertionsFailed() != 0 || len(resp.Assertions) != 1 {
		t.Errorf("assertions = %+v, want one pass", resp.Assertions)
	}
}

func TestPlainDispatchRetries(t *testing.T) {
	srv, hits := scriptedServer(t, 500, 200)
	resp, err := Dispatch(context.Background(), timeoutRequest(t, "# @retry 1\nGET "+srv.URL+"/\n"), Options{DisableConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || hits.Load() != 2 || resp.AttemptLabel() != "attempt 2/2" {
		t.Fatalf("status %d after %d requests (%q)", resp.StatusCode, hits.Load(), resp.AttemptLabel())
	}
	// Without the directive there is no indicator.
	resp, _ = Dispatch(context.Background(), timeoutRequest(t, "GET "+srv.URL+"/\n"), Options{DisableConfig: true})
	if resp.AttemptLabel() != "" || resp.Attempts != nil {
		t.Errorf("label %q, attempts %v; want none", resp.AttemptLabel(), resp.Attempts)
	}
}
