package httpclient

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"ike/internal/httpfile"
)

// fakeClock returns a clock whose every call advances by step, so the trace
// hooks see distinguishable instants without a real wait.
func fakeClock(step time.Duration) func() time.Time {
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	n := 0
	return func() time.Time {
		n++
		return base.Add(time.Duration(n) * step)
	}
}

// TestTimingTraceHookMapping drives the httptrace hooks directly: each pair
// of start/done hooks must land in its own phase, TTFB must be measured from
// the exchange start, and everything after the first byte is transfer (#2404).
func TestTimingTraceHookMapping(t *testing.T) {
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	var now time.Time
	tr := newTimingTrace(func() time.Time { return now }, base)
	ct := tr.clientTrace()

	now = base.Add(10 * time.Millisecond)
	ct.DNSStart(httptrace.DNSStartInfo{})
	now = base.Add(30 * time.Millisecond)
	ct.DNSDone(httptrace.DNSDoneInfo{})

	now = base.Add(30 * time.Millisecond)
	ct.ConnectStart("tcp", "example:443")
	now = base.Add(80 * time.Millisecond)
	ct.ConnectDone("tcp", "example:443", nil)

	now = base.Add(80 * time.Millisecond)
	ct.TLSHandshakeStart()
	now = base.Add(200 * time.Millisecond)
	ct.TLSHandshakeDone(tls.ConnectionState{}, nil)

	ct.GotConn(httptrace.GotConnInfo{})

	now = base.Add(500 * time.Millisecond)
	ct.GotFirstResponseByte()

	got := tr.result(base.Add(600 * time.Millisecond))
	if got == nil {
		t.Fatal("no timing captured")
	}
	want := Timing{
		DNS:      20 * time.Millisecond,
		Connect:  50 * time.Millisecond,
		TLS:      120 * time.Millisecond,
		TTFB:     500 * time.Millisecond,
		Transfer: 100 * time.Millisecond,
	}
	if *got != want {
		t.Fatalf("timing = %+v, want %+v", *got, want)
	}
}

// TestTimingTraceAccumulatesRetries: a redirect chain fires the setup hooks
// more than once and the phases sum — what the user waited for is the total,
// not the last leg.
func TestTimingTraceAccumulatesRetries(t *testing.T) {
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	var now time.Time
	tr := newTimingTrace(func() time.Time { return now }, base)
	ct := tr.clientTrace()
	for i := 0; i < 2; i++ {
		now = base.Add(time.Duration(i*100) * time.Millisecond)
		ct.ConnectStart("tcp", "example:80")
		now = base.Add(time.Duration(i*100+40) * time.Millisecond)
		ct.ConnectDone("tcp", "example:80", nil)
	}
	now = base.Add(300 * time.Millisecond)
	ct.GotFirstResponseByte()
	got := tr.result(base.Add(300 * time.Millisecond))
	if got == nil || got.Connect != 80*time.Millisecond {
		t.Fatalf("connect = %v, want 80ms (two legs summed)", got)
	}
}

// TestTimingTraceReusedConnection: a keep-alive connection has no setup
// phases, and the breakdown says so instead of leaving three silent zeros.
func TestTimingTraceReusedConnection(t *testing.T) {
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	now := base
	tr := newTimingTrace(func() time.Time { return now }, base)
	ct := tr.clientTrace()
	ct.GotConn(httptrace.GotConnInfo{Reused: true})
	now = base.Add(5 * time.Millisecond)
	ct.GotFirstResponseByte()
	got := tr.result(base.Add(6 * time.Millisecond))
	if got == nil || !got.Reused {
		t.Fatalf("timing = %+v, want Reused", got)
	}
	if got.DNS != 0 || got.Connect != 0 || got.TLS != 0 {
		t.Fatalf("setup phases should be zero on a reused connection: %+v", got)
	}
	if line := got.String(); !strings.HasPrefix(line, "conn reused") {
		t.Fatalf("String() = %q, want it to lead with the reuse note", line)
	}
}

// TestTimingResultEmpty: nothing measured yields no breakdown at all, which
// is what keeps a pre-#2404 history entry from rendering a row of zeros.
func TestTimingResultEmpty(t *testing.T) {
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	tr := newTimingTrace(func() time.Time { return base }, base)
	if got := tr.result(base); got != nil {
		t.Fatalf("result = %+v, want nil", got)
	}
	var nilT *Timing
	if !nilT.IsZero() || nilT.String() != "" {
		t.Fatal("a nil Timing must read as empty")
	}
}

// TestDispatchCapturesTiming is the end-to-end hook wiring: a real exchange
// against a local server must come back with a breakdown whose TTFB is set.
func TestDispatchCapturesTiming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	req := &httpfile.Request{Method: "GET", Target: srv.URL}
	resp, err := Dispatch(context.Background(), req, Options{DisableConfig: true, Now: fakeClock(time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Timing == nil {
		t.Fatal("Dispatch returned no timing breakdown")
	}
	if resp.Timing.TTFB <= 0 {
		t.Fatalf("TTFB = %v, want > 0", resp.Timing.TTFB)
	}
	if resp.Timing.Connect <= 0 {
		t.Fatalf("Connect = %v, want > 0 on a fresh connection", resp.Timing.Connect)
	}
	if line := resp.Timing.String(); line == "" {
		t.Fatal("String() rendered nothing for a measured exchange")
	}
}

// TestFormatPhase pins the spelling the pane and the indicator share.
func TestFormatPhase(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{120 * time.Millisecond, "120ms"},
		{999 * time.Millisecond, "999ms"},
		{1500 * time.Millisecond, "1.5s"},
	} {
		if got := FormatPhase(tc.d); got != tc.want {
			t.Errorf("FormatPhase(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// TestTimingStringSkipsMissingPhases: a reused connection or a plain-HTTP
// request must not print phases that never happened.
func TestTimingStringSkipsMissingPhases(t *testing.T) {
	tm := &Timing{TTFB: 210 * time.Millisecond, Transfer: 4 * time.Millisecond}
	want := "ttfb 210ms · transfer 4ms"
	if got := tm.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
