package httpclient

// redirect.go records the chain a dispatch walked before it landed on the
// response the pane shows (#2716). Until now only the *final* answer was
// kept: a 301 → 302 → 200 looked exactly like a direct 200, except that the
// timing line (#2404) reported the accumulated setup of three exchanges with
// nothing to explain the number. The chain answers "where did this end up,
// and over which hosts and addresses did it get there".
//
// The capture replaces the client's redirect policy rather than adding to it:
// Go's default CheckRedirect is the ten-redirect limit and nothing else, so a
// policy of our own has to restate the limit (MaxRedirects). The connection
// facts per hop come from the timing collector's hook set — Go fires the
// httptrace hooks once per hop, so the facts standing in the collector when
// CheckRedirect is consulted belong to the hop that just answered.

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// MaxRedirects is how many redirects a dispatch follows before giving up —
// Go's own default limit, restated because recording the chain means
// replacing the default policy. It also caps the recorded chain: past it the
// client errors anyway, so there is nothing further to show.
const MaxRedirects = 10

// Hop is one exchange of a followed redirect chain (#2716). Method and URL
// are what was *sent*; Status and Location come from the response that came
// back, Location being empty on the final one. The remaining fields are the
// connection facts of that hop as httptrace reported them — all optional,
// since a reused connection resolves no name and performs no handshake.
type Hop struct {
	// Method is the request method of this hop, after any redirect-induced
	// change (a 303 turns a POST into a GET).
	Method string `json:"method"`
	// URL is the full URL this hop was sent to.
	URL string `json:"url"`
	// Status is the status code that came back.
	Status int `json:"status,omitempty"`
	// Location is the Location header of a redirecting response, "" for the
	// final one.
	Location string `json:"location,omitempty"`
	// DNSAddrs are the addresses name resolution returned for the hop's host;
	// empty when the host was an IP literal or the connection was reused.
	DNSAddrs []string `json:"dnsAddrs,omitempty"`
	// RemoteAddr is the address actually connected to, host:port.
	RemoteAddr string `json:"remoteAddr,omitempty"`
	// TLSServerName is the SNI name the handshake used; "" for plain HTTP and
	// for a reused connection, which performs no handshake.
	TLSServerName string `json:"tlsServerName,omitempty"`
	// Proto is the protocol ALPN negotiated during the handshake ("h2",
	// "http/1.1"); "" when there was no handshake on this hop.
	Proto string `json:"proto,omitempty"`
	// Reused records that this hop went out on an existing connection, which
	// is why its DNS/connect/TLS facts are empty rather than unmeasured.
	Reused bool `json:"reused,omitempty"`
	// MethodChanged marks a hop whose *successor* was sent with a different
	// method — the 303 (and the de-facto 301/302) POST → GET rewrite, which
	// also drops the body.
	MethodChanged bool `json:"methodChanged,omitempty"`
}

// Host is the host name of the hop's URL, "" when it cannot be read. The
// rendered chain names it once in front of the resolved addresses rather than
// repeating the whole URL.
func (h Hop) Host() string {
	u, err := url.Parse(h.URL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

// HasConn reports whether the hop carries any connection fact worth a line of
// its own.
func (h Hop) HasConn() bool {
	return len(h.DNSAddrs) > 0 || h.RemoteAddr != "" || h.TLSServerName != "" || h.Proto != "" || h.Reused
}

// RedirectCount is how many redirects the exchange followed: the recorded
// chain holds one hop per exchange, the final response included, so a
// 301 → 302 → 200 is three hops and two redirects. 0 for a direct answer and
// for a response restored from a history file written before the capture
// existed.
func (r *Response) RedirectCount() int {
	if r == nil || len(r.Redirects) == 0 {
		return 0
	}
	return len(r.Redirects) - 1
}

// redirectRecorder is the per-dispatch chain collector. It lives on the
// prepared request and is driven from the client's CheckRedirect, which Go
// calls on the caller's own goroutine — the hops therefore need no lock; only
// the connection facts do, and those are guarded inside the timing collector.
type redirectRecorder struct {
	// follow mirrors the .curlrc `-L` decision: false declines every redirect
	// (http.ErrUseLastResponse), which is what the pane reports as "redirect
	// not followed".
	follow bool
	// off records that a redirect actually arrived while follow was false —
	// only then is there something to say.
	off bool
	// trace is the exchange's timing collector, the source of the per-hop
	// connection facts. Set by begin, nil until the exchange starts.
	trace *timingTrace

	hops     []Hop
	finalURL string
	done     bool
}

// newRedirectRecorder returns the collector for one dispatch; follow is the
// resolved .curlrc `-L` decision.
func newRedirectRecorder(follow bool) *redirectRecorder {
	return &redirectRecorder{follow: follow}
}

// begin arms the collector for one exchange with the timing collector whose
// hooks it reads the per-hop connection facts from.
func (r *redirectRecorder) begin(tr *timingTrace) {
	if r == nil {
		return
	}
	r.trace, r.hops, r.off, r.finalURL, r.done = tr, nil, false, "", false
}

// checkRedirect is the client's redirect policy: it records the hop that just
// answered and then either lets the redirect through, declines it (`-L` off)
// or stops the chain at Go's limit.
func (r *redirectRecorder) checkRedirect(req *http.Request, via []*http.Request) error {
	if r == nil {
		// No recorder: Go's default policy, which is the limit and nothing
		// else. Declining here instead would silently turn `-L` off.
		if len(via) >= MaxRedirects {
			return fmt.Errorf("stopped after %d redirects", MaxRedirects)
		}
		return nil
	}
	if !r.follow {
		r.off = true
		return http.ErrUseLastResponse
	}
	r.record(req, via)
	if len(via) >= MaxRedirects {
		// Go's default policy's own wording and bound, restated because
		// replacing CheckRedirect replaces the limit with it.
		return fmt.Errorf("stopped after %d redirects", MaxRedirects)
	}
	return nil
}

// record appends the hop that produced the redirect now being followed. req
// is the *next* request; via holds the ones already sent, so the hop's own
// request is via's last and the response that redirected it is req.Response —
// the only place Go hands the intermediate response to a policy.
func (r *redirectRecorder) record(req *http.Request, via []*http.Request) {
	if len(via) == 0 || len(r.hops) >= MaxRedirects {
		return
	}
	sent := via[len(via)-1]
	h := Hop{Method: sent.Method, URL: sent.URL.String()}
	if prev := req.Response; prev != nil {
		h.Status = prev.StatusCode
		h.Location = prev.Header.Get("Location")
	}
	h.MethodChanged = !strings.EqualFold(sent.Method, req.Method)
	h.readConn(r.trace)
	r.hops = append(r.hops, h)
}

// finish closes the chain with the exchange that actually answered. The final
// hop is only appended when the chain has one at all: a direct answer records
// nothing, so an empty Redirects keeps meaning "no redirect happened".
func (r *redirectRecorder) finish(resp *http.Response) {
	if r == nil || r.done || resp == nil {
		return
	}
	r.done = true
	if resp.Request != nil && resp.Request.URL != nil {
		r.finalURL = resp.Request.URL.String()
	}
	if len(r.hops) == 0 {
		return
	}
	h := Hop{Status: resp.StatusCode}
	if resp.Request != nil {
		h.Method = resp.Request.Method
		if resp.Request.URL != nil {
			h.URL = resp.Request.URL.String()
		}
	}
	h.readConn(r.trace)
	r.hops = append(r.hops, h)
}

// readConn takes the connection facts the timing collector gathered for the
// hop that just ended, and resets it for the next one.
func (h *Hop) readConn(tr *timingTrace) {
	if tr == nil {
		return
	}
	c := tr.takeHop()
	h.DNSAddrs, h.RemoteAddr = c.dnsAddrs, c.remoteAddr
	h.TLSServerName, h.Proto, h.Reused = c.tlsServerName, c.tlsProto, c.reused
}

// chain is the recorded hops, nil when nothing redirected.
func (r *redirectRecorder) chain() []Hop {
	if r == nil {
		return nil
	}
	return r.hops
}

// final is the URL the answering response came from — the request URL when no
// redirect happened.
func (r *redirectRecorder) final() string {
	if r == nil {
		return ""
	}
	return r.finalURL
}

// declined reports that a redirect arrived and was not followed because
// .curlrc turned `-L` off.
func (r *redirectRecorder) declined() bool {
	return r != nil && r.off
}
