package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/httpfile"
)

// timeout_test.go covers the overall deadline (#2630): where it comes from
// (directive > .curlrc max-time > setting > built-in default) and how an
// exchange that ends by it reports itself.

// timeoutRequest parses a one-request .http source and returns the request.
func timeoutRequest(t *testing.T, src string) *httpfile.Request {
	t.Helper()
	f := httpfile.Parse(src)
	if len(f.Errors) != 0 {
		t.Fatalf("parse: %v", f.Errors)
	}
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(f.Requests))
	}
	return f.Requests[0]
}

// curlrcWith writes a .curlrc holding lines and returns its path.
func curlrcWith(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".curlrc")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The whole precedence chain, read off the client the preparation builds.
func TestTimeoutPrecedence(t *testing.T) {
	directive := timeoutRequest(t, "# @timeout 5s\nGET http://example.test/a\n")
	plain := timeoutRequest(t, "GET http://example.test/a\n")
	maxTime := curlrcWith(t, "max-time = 12")

	cases := []struct {
		name string
		req  *httpfile.Request
		opts Options
		want time.Duration
	}{
		{"built-in default", plain, Options{DisableConfig: true}, DefaultTimeout},
		{"setting", plain, Options{DisableConfig: true, Timeout: 7 * time.Second}, 7 * time.Second},
		{"curlrc beats the setting", plain,
			Options{CurlrcPath: maxTime, Timeout: 7 * time.Second}, 12 * time.Second},
		{"directive beats curlrc", directive,
			Options{CurlrcPath: maxTime, Timeout: 7 * time.Second}, 5 * time.Second},
		{"directive beats the setting", directive,
			Options{DisableConfig: true, Timeout: 7 * time.Second}, 5 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := prepare(context.Background(), c.req, c.opts)
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if p.client.Timeout != c.want {
				t.Errorf("client timeout = %v, want %v", p.client.Timeout, c.want)
			}
			if p.timeout != c.want {
				t.Errorf("prepared timeout = %v, want %v", p.timeout, c.want)
			}
		})
	}
}

// A re-sent snapshot has no .http block to read a directive from, so the
// chain is .curlrc over the setting over the default.
func TestTimeoutPrecedenceOnResend(t *testing.T) {
	snap := &RequestSnapshot{Method: "GET", URL: "http://example.test/a"}
	p, err := prepareSnapshot(context.Background(), "one", snap,
		Options{DisableConfig: true, Timeout: 4 * time.Second})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if p.client.Timeout != 4*time.Second {
		t.Errorf("client timeout = %v, want the setting's 4s", p.client.Timeout)
	}
	p, err = prepareSnapshot(context.Background(), "one", snap,
		Options{CurlrcPath: curlrcWith(t, "max-time = 12"), Timeout: 4 * time.Second})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if p.client.Timeout != 12*time.Second {
		t.Errorf("client timeout = %v, want .curlrc's 12s", p.client.Timeout)
	}
}

// hangingServer never answers until the test ends.
func hangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	return srv
}

// A server that never answers ends the dispatch as a TimeoutError naming the
// limit, not as a generic transport failure.
func TestDispatchReportsTimeout(t *testing.T) {
	srv := hangingServer(t)
	req := timeoutRequest(t, "GET "+srv.URL+"/slow\n")
	_, err := Dispatch(context.Background(), req,
		Options{DisableConfig: true, Timeout: 150 * time.Millisecond})
	var timedOut *TimeoutError
	if !errors.As(err, &timedOut) {
		t.Fatalf("err = %v (%T), want a *TimeoutError", err, err)
	}
	if timedOut.Limit != 150*time.Millisecond {
		t.Errorf("limit = %v, want 150ms", timedOut.Limit)
	}
	if !strings.Contains(err.Error(), "timed out after") {
		t.Errorf("message %q must say it timed out", err.Error())
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("a timeout must still read as a deadline for errors.Is")
	}
}

// The directive is what actually bounds the wire, not only the client field.
func TestDispatchStreamHonoursDirectiveDeadline(t *testing.T) {
	srv := hangingServer(t)
	req := timeoutRequest(t, "# @timeout 120ms\nGET "+srv.URL+"/slow\n")
	start := time.Now()
	_, err := DispatchStream(context.Background(), req,
		Options{DisableConfig: true, Timeout: time.Minute}, StreamCallbacks{})
	var timedOut *TimeoutError
	if !errors.As(err, &timedOut) {
		t.Fatalf("err = %v (%T), want a *TimeoutError", err, err)
	}
	if timedOut.Limit != 120*time.Millisecond {
		t.Errorf("limit = %v, want the directive's 120ms", timedOut.Limit)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("waited %v — the directive did not bound the exchange", took)
	}
}

// A user cancel is not a timeout, even with a deadline armed: the pane must
// keep saying "canceled".
func TestCancelIsNotReportedAsTimeout(t *testing.T) {
	srv := hangingServer(t)
	req := timeoutRequest(t, "GET "+srv.URL+"/slow\n")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := DispatchStream(ctx, req,
		Options{DisableConfig: true, Timeout: 10 * time.Second}, StreamCallbacks{})
	var timedOut *TimeoutError
	if errors.As(err, &timedOut) {
		t.Fatalf("a cancel must not read as a timeout: %v", err)
	}
	if err == nil {
		t.Fatal("the aborted exchange must fail")
	}
}

func TestFormatTimeout(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second:        "30 s",
		time.Minute:             "60 s",
		1500 * time.Millisecond: "1.5s",
		250 * time.Millisecond:  "250ms",
	}
	for d, want := range cases {
		if got := FormatTimeout(d); got != want {
			t.Errorf("FormatTimeout(%v) = %q, want %q", d, got, want)
		}
	}
}
