package httpclient

import (
	"crypto/tls"
	"fmt"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"
)

// timing.go adds the per-phase breakdown of one exchange (#2404). A total
// duration answers "was it slow?" but never "slow where?" — a 2 s flight
// spent in DNS is a resolver problem, the same 2 s spent waiting for the
// first byte is the server's. net/http/httptrace reports the phase
// boundaries; Timing is what the viewer, the history and the telemetry event
// carry.

// Timing is the phase breakdown of one exchange. Every field is the time
// spent *in* that phase, not a timestamp, so the numbers read as a sum. A
// phase that did not happen is zero: a reused keep-alive connection has no
// DNS/connect/TLS, and a plain-HTTP request has no TLS.
type Timing struct {
	// DNS is the name resolution time; 0 when the host was an IP literal or
	// the connection was reused.
	DNS time.Duration `json:"dns,omitempty"`
	// Connect is the TCP (or proxy) connect time; 0 on a reused connection.
	Connect time.Duration `json:"connect,omitempty"`
	// TLS is the handshake time; 0 for plain HTTP and reused connections.
	TLS time.Duration `json:"tls,omitempty"`
	// TTFB is the time from the start of the exchange to the first response
	// byte — connection setup included, so it is comparable across a fresh
	// and a reused connection.
	TTFB time.Duration `json:"ttfb,omitempty"`
	// Transfer is the time spent reading the body after the first byte.
	Transfer time.Duration `json:"transfer,omitempty"`
	// Reused records that the request went out on an existing connection,
	// which is why the setup phases are zero rather than unmeasured.
	Reused bool `json:"reused,omitempty"`
}

// IsZero reports whether the breakdown carries nothing worth showing — a
// response restored from a history file written before the capture existed.
func (t *Timing) IsZero() bool {
	return t == nil || (t.DNS == 0 && t.Connect == 0 && t.TLS == 0 && t.TTFB == 0 && t.Transfer == 0 && !t.Reused)
}

// String renders the breakdown as the single line the response pane shows
// under the status line: only the phases that happened, in wire order. A
// reused connection says so instead of printing three zeros.
func (t *Timing) String() string {
	if t.IsZero() {
		return ""
	}
	var parts []string
	if t.Reused {
		parts = append(parts, "conn reused")
	}
	add := func(label string, d time.Duration) {
		if d > 0 {
			parts = append(parts, fmt.Sprintf("%s %s", label, FormatPhase(d)))
		}
	}
	add("dns", t.DNS)
	add("connect", t.Connect)
	add("tls", t.TLS)
	add("ttfb", t.TTFB)
	add("transfer", t.Transfer)
	return strings.Join(parts, " · ")
}

// FormatPhase spells one phase duration: whole milliseconds below a second,
// one decimal of seconds above it — the spelling the in-flight indicator
// already uses, so a phase and a total read alike.
func FormatPhase(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// timingTrace collects the httptrace hook timestamps of one exchange. The
// hooks fire on the transport's own goroutines, hence the mutex; a redirect
// chain (or a retried connection) fires the setup hooks more than once, and
// the phases accumulate rather than the last one winning — what the user
// waited for is the sum.
type timingTrace struct {
	now func() time.Time

	mu        sync.Mutex
	start     time.Time
	dnsStart  time.Time
	connStart time.Time
	tlsStart  time.Time
	t         Timing
	gotFirst  time.Time
}

// newTimingTrace starts a collector; now is the dispatcher's clock, so tests
// drive the breakdown the same way they drive Duration.
func newTimingTrace(now func() time.Time, start time.Time) *timingTrace {
	if now == nil {
		now = time.Now
	}
	return &timingTrace{now: now, start: start}
}

// clientTrace is the httptrace hook set to hang into the request context.
func (t *timingTrace) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.dnsStart = t.now()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			if !t.dnsStart.IsZero() {
				t.t.DNS += t.now().Sub(t.dnsStart)
				t.dnsStart = time.Time{}
			}
		},
		ConnectStart: func(string, string) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.connStart = t.now()
		},
		ConnectDone: func(string, string, error) {
			t.mu.Lock()
			defer t.mu.Unlock()
			if !t.connStart.IsZero() {
				t.t.Connect += t.now().Sub(t.connStart)
				t.connStart = time.Time{}
			}
		},
		TLSHandshakeStart: func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.tlsStart = t.now()
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			t.mu.Lock()
			defer t.mu.Unlock()
			if !t.tlsStart.IsZero() {
				t.t.TLS += t.now().Sub(t.tlsStart)
				t.tlsStart = time.Time{}
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			if info.Reused {
				t.t.Reused = true
			}
		},
		GotFirstResponseByte: func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.gotFirst.IsZero() {
				t.gotFirst = t.now()
				t.t.TTFB = t.gotFirst.Sub(t.start)
			}
		},
	}
}

// result closes the breakdown at the end of the exchange: everything after
// the first byte is transfer. end is the same instant Response.Duration is
// measured against, so TTFB + Transfer equals the total.
func (t *timingTrace) result(end time.Time) *Timing {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.t
	if !t.gotFirst.IsZero() {
		if d := end.Sub(t.gotFirst); d > 0 {
			out.Transfer = d
		}
	}
	if out.IsZero() {
		return nil
	}
	return &out
}
