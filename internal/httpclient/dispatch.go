// Package httpclient dispatches parsed .http request blocks (#1249, epic
// #1247): it substitutes environment placeholders, applies local client
// configuration (.netrc credentials, .curlrc options) and executes the
// request, returning a Response the viewer and history layers consume.
// DispatchStream (#1776) additionally recognizes streaming responses
// (SSE/NDJSON, see IsStreamContentType) and hands headers and body chunks to
// callbacks while the connection is open; everything else — and every caller
// of plain Dispatch — keeps the collect-then-return behavior.
//
// Precedence: explicit values in the .http file always win. .netrc
// credentials apply only when the request carries no Authorization header;
// .curlrc headers are added only when the request does not set the same
// header itself. Unsupported .curlrc options become warnings, never
// failures.
package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"ike/internal/httpfile"
	"ike/internal/pathcomplete"
)

// Defaults applied when neither the request nor .curlrc override them.
const (
	// DefaultTimeout bounds the whole exchange.
	DefaultTimeout = 30 * time.Second
	// MaxBodyBytes caps how much of a response body is kept (10 MiB is far
	// beyond what the TUI viewer renders; the rest is dropped with a note).
	MaxBodyBytes = 10 << 20
	// StreamIdleTimeout replaces the overall deadline once a response is
	// recognized as a stream (#1776): a hard 30 s limit would kill every
	// long-lived SSE/NDJSON connection, so a stream instead ends when the
	// server sends nothing for this long (or when the user cancels).
	StreamIdleTimeout = 60 * time.Second
)

// Response is the captured result of one dispatch.
type Response struct {
	Status     string // e.g. "200 OK"
	StatusCode int
	Proto      string
	Headers    http.Header
	Body       []byte
	// Truncated is set when the body exceeded MaxBodyBytes and was cut.
	Truncated bool
	// Duration is the wall-clock time of the exchange.
	Duration time.Duration
	// RequestKey identifies the originating request (httpfile.Request.Key).
	RequestKey string
	// Warnings lists non-fatal issues (e.g. ignored .curlrc options).
	Warnings []string
	// Request is the request exactly as it went out (#1832); nil for a
	// response restored from a history file written before the capture
	// existed, which is what makes re-send unavailable there.
	Request *RequestSnapshot
}

// RequestSnapshot is one request as it was actually sent (#1832): method,
// final URL, headers and body *after* placeholder substitution and after the
// local .netrc/.curlrc configuration was applied. Every dispatch captures one,
// it travels with the Response and is persisted with the history entry, so a
// stored response can be re-sent verbatim with Resend — no re-parse of the
// .http file, which may have changed since, and no re-substitution, which may
// resolve differently now.
//
// A Host header override (the request's own `Host:` line, which Go keeps out
// of Request.Header) is stored under the "Host" key and restored on re-send.
type RequestSnapshot struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers http.Header `json:"headers,omitempty"`
	Body    []byte      `json:"body,omitempty"`
}

// Clone returns a deep copy, so a snapshot handed to the UI (and back into a
// history file) can never be mutated through a shared header map.
func (s *RequestSnapshot) Clone() *RequestSnapshot {
	if s == nil {
		return nil
	}
	c := &RequestSnapshot{Method: s.Method, URL: s.URL, Headers: s.Headers.Clone()}
	if s.Body != nil {
		c.Body = append([]byte(nil), s.Body...)
	}
	return c
}

// Label is the short "METHOD /path" form of a snapshot, for the in-flight
// indicator of a re-send.
func (s *RequestSnapshot) Label() string {
	if s == nil {
		return ""
	}
	target := s.URL
	if u, err := url.Parse(target); err == nil && u.Path != "" {
		target = u.Path
		if u.RawQuery != "" {
			target += "?" + u.RawQuery
		}
	}
	if len(target) > 40 {
		target = target[:39] + "…"
	}
	return s.Method + " " + target
}

// Options configures a Dispatcher. The zero value uses the process
// environment and the real user configuration files.
type Options struct {
	// Lookup resolves placeholder variables; defaults to os.LookupEnv.
	Lookup func(string) (string, bool)
	// NetrcPath overrides the .netrc location ("" = $NETRC / $HOME/.netrc).
	NetrcPath string
	// CurlrcPath overrides the .curlrc location ("" = curl's lookup order).
	CurlrcPath string
	// DisableConfig skips .netrc/.curlrc detection entirely.
	DisableConfig bool
	// Timeout overrides DefaultTimeout when > 0.
	Timeout time.Duration
	// Now returns the current time; defaults to time.Now (tests).
	Now func() time.Time
	// BaseDir resolves relative external-body paths — the `< ./payload.json`
	// form (#1305). It is the .http file's own directory; empty falls back to
	// the process working directory.
	BaseDir string
	// StreamIdleTimeout overrides StreamIdleTimeout when > 0 (tests).
	StreamIdleTimeout time.Duration
}

// requestBody returns the bytes of a resolved request's body: the inline
// text, the contents of an external `< ./file` body (#1305), or — when the
// Content-Type declares a multipart boundary — the hand-written multipart
// structure normalised to CRLF with per-part `< file` directives embedded
// (#1707). Bodies are assembled up front rather than streamed so their length
// is known (no chunked encoding) and so the snapshot (#1832) can keep exactly
// what went out.
func requestBody(resolved *httpfile.Request, opts Options, lookup func(string) (string, bool)) ([]byte, error) {
	if resolved.BodyFile != "" {
		return loadBodyFile(resolved.BodyFile, resolved.BodyFileSubstitute, opts, lookup)
	}
	if ct, ok := resolved.Header("Content-Type"); ok {
		if boundary, ok := httpfile.MultipartBoundary(ct); ok {
			return httpfile.BuildMultipartBody(resolved.Body, boundary,
				func(path string, substitute bool) ([]byte, error) {
					return loadBodyFile(path, substitute, opts, lookup)
				})
		}
	}
	return []byte(resolved.Body), nil
}

// loadBodyFile reads one body file (#1305): relative paths resolve against
// opts.BaseDir — the .http file's own directory, not the process working
// directory — and a leading ~ expands. substitute (the `<@` spelling)
// replaces the file's own placeholders; the plain `<` form returns the bytes
// untouched, so binary content survives verbatim.
func loadBodyFile(file string, substitute bool, opts Options, lookup func(string) (string, bool)) ([]byte, error) {
	path := pathcomplete.Expand(file)
	if !filepath.IsAbs(path) {
		path = filepath.Join(opts.BaseDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("body file %s: %v", file, err)
	}
	if substitute {
		text, err := httpfile.Substitute(string(data), lookup)
		if err != nil {
			return nil, fmt.Errorf("body file %s: %v", file, err)
		}
		data = []byte(text)
	}
	return data, nil
}

// prepared is the executable form of one request — everything Dispatch and
// DispatchStream share before the wire is touched (#1776).
type prepared struct {
	httpReq  *http.Request
	client   *http.Client
	warnings []string
	now      func() time.Time
	// snapshot is what goes on the wire (#1832), captured once everything —
	// substitution, .curlrc, .netrc — has been applied.
	snapshot *RequestSnapshot
}

// prepare resolves placeholders in req, applies local configuration and
// builds the http.Request plus the configured client.
func prepare(ctx context.Context, req *httpfile.Request, opts Options) (*prepared, error) {
	lookup := opts.Lookup
	if lookup == nil {
		lookup = lookupEnv
	}
	resolved, err := req.Resolve(lookup)
	if err != nil {
		return nil, err
	}

	target, err := url.Parse(resolved.Target)
	if err != nil {
		return nil, fmt.Errorf("request %s: invalid target %q: %v", req.Key(), resolved.Target, err)
	}
	if target.Scheme == "" {
		target.Scheme = "http"
	}
	if target.Host == "" {
		return nil, fmt.Errorf("request %s: target %q has no host", req.Key(), resolved.Target)
	}

	var warnings []string
	cfg := &curlConfig{}
	if !opts.DisableConfig {
		path := opts.CurlrcPath
		if path == "" {
			path = curlrcPath()
		}
		cfg = parseCurlrc(path)
		warnings = append(warnings, cfg.Warnings...)
	}

	body, err := requestBody(resolved, opts, lookup)
	if err != nil {
		return nil, fmt.Errorf("request %s: %v", req.Key(), err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, resolved.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request %s: %v", req.Key(), err)
	}
	for _, h := range resolved.Headers {
		if strings.EqualFold(h.Name, "Host") {
			httpReq.Host = h.Value
			continue
		}
		httpReq.Header.Add(h.Name, h.Value)
	}

	applyCurlConfig(httpReq, resolved, cfg)
	if err := applyNetrc(httpReq, cfg, opts); err != nil {
		return nil, err
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &prepared{
		httpReq:  httpReq,
		client:   buildClient(cfg, opts),
		warnings: warnings,
		now:      now,
		snapshot: snapshotOf(httpReq, body),
	}, nil
}

// snapshotOf captures the fully prepared request (#1832). It runs after every
// header source has had its say, so what it holds is what the server sees.
func snapshotOf(httpReq *http.Request, body []byte) *RequestSnapshot {
	snap := &RequestSnapshot{
		Method:  httpReq.Method,
		URL:     httpReq.URL.String(),
		Headers: httpReq.Header.Clone(),
	}
	if snap.Headers == nil {
		snap.Headers = http.Header{}
	}
	if httpReq.Host != "" {
		// Go keeps a Host override out of Header; the snapshot carries it as an
		// ordinary header and prepareSnapshot puts it back where it belongs.
		snap.Headers.Set("Host", httpReq.Host)
	}
	if len(body) > 0 {
		snap.Body = append([]byte(nil), body...)
	}
	return snap
}

// prepareSnapshot builds the executable form of a stored request (#1832). It
// is deliberately *not* prepare: no placeholder substitution, no .netrc, no
// .curlrc header mapping — the snapshot already holds the outcome of all of
// them, and re-running those steps is exactly what "re-send verbatim" rules
// out. Only the client itself is configured from .curlrc, since proxy, TLS
// and timeouts are about reaching the host, not about the request's content.
func prepareSnapshot(ctx context.Context, key string, snap *RequestSnapshot, opts Options) (*prepared, error) {
	if snap == nil {
		return nil, fmt.Errorf("request %s: no stored request to re-send", key)
	}
	httpReq, err := http.NewRequestWithContext(ctx, snap.Method, snap.URL, bytes.NewReader(snap.Body))
	if err != nil {
		return nil, fmt.Errorf("request %s: %v", key, err)
	}
	for name, values := range snap.Headers {
		if strings.EqualFold(name, "Host") {
			if len(values) > 0 {
				httpReq.Host = values[0]
			}
			continue
		}
		for _, v := range values {
			httpReq.Header.Add(name, v)
		}
	}

	cfg := &curlConfig{}
	if !opts.DisableConfig {
		path := opts.CurlrcPath
		if path == "" {
			path = curlrcPath()
		}
		cfg = parseCurlrc(path)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &prepared{
		httpReq:  httpReq,
		client:   buildClient(cfg, opts),
		now:      now,
		snapshot: snap.Clone(),
	}, nil
}

// Dispatch resolves placeholders in req, applies local configuration and
// executes it. Unresolved placeholders or transport failures return an
// error; HTTP error statuses are regular responses.
func Dispatch(ctx context.Context, req *httpfile.Request, opts Options) (*Response, error) {
	p, err := prepare(ctx, req, opts)
	if err != nil {
		return nil, err
	}
	return p.collect(req.Key())
}

// collect executes the prepared request and reads the whole body — the
// non-streaming exchange, shared by Dispatch and by DispatchStream's fallback
// for a response that turns out not to be a stream.
func (p *prepared) collect(key string) (*Response, error) {
	start := p.now()
	httpResp, err := p.client.Do(p.httpReq)
	if err != nil {
		return nil, fmt.Errorf("request %s: %v", key, err)
	}
	defer httpResp.Body.Close()
	return p.collectBody(key, httpResp, start)
}

// collectBody reads a started exchange's body to the cap and composes the
// Response; start is when the exchange began.
func (p *prepared) collectBody(key string, httpResp *http.Response, start time.Time) (*Response, error) {
	body, readErr := io.ReadAll(io.LimitReader(httpResp.Body, MaxBodyBytes+1))
	elapsed := p.now().Sub(start)
	if readErr != nil {
		return nil, fmt.Errorf("request %s: reading response: %v", key, readErr)
	}
	truncated := false
	warnings := p.warnings
	if len(body) > MaxBodyBytes {
		body = body[:MaxBodyBytes]
		truncated = true
		warnings = append(warnings, fmt.Sprintf("response body exceeded %d bytes and was truncated", MaxBodyBytes))
	}
	return &Response{
		Status:     httpResp.Status,
		StatusCode: httpResp.StatusCode,
		Proto:      httpResp.Proto,
		Headers:    httpResp.Header,
		Body:       body,
		Truncated:  truncated,
		Duration:   elapsed,
		RequestKey: key,
		Warnings:   warnings,
		Request:    p.snapshot,
	}, nil
}

// IsStreamContentType reports whether a Content-Type header marks a response
// that arrives incrementally (#1776). The check is deliberately conservative
// — only media types that *promise* streaming qualify; everything else keeps
// the collect-then-show behavior, which is the safe fallback.
func IsStreamContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch ct {
	case "text/event-stream", // SSE
		"application/x-ndjson", "application/ndjson", // newline-delimited JSON
		"application/json-seq",    // RFC 7464 JSON text sequences
		"application/stream+json": // Spring's NDJSON precursor
		return true
	}
	return false
}

// StreamCallbacks receive the incremental parts of a streaming dispatch
// (#1776). They are only invoked when the response is recognized as a stream
// (IsStreamContentType) and run on the dispatch goroutine — implementations
// hand the data off (e.g. into a channel) rather than block.
type StreamCallbacks struct {
	// OnHeaders fires once, as soon as the response headers arrive.
	OnHeaders func(status string, statusCode int, proto string, headers http.Header)
	// OnChunk fires per received body chunk; the slice is the callback's own.
	OnChunk func(chunk []byte)
}

// DispatchStream executes req like Dispatch, but recognizes streaming
// responses (#1776): when the Content-Type promises incremental data, the
// headers and every body chunk are handed to cb while the connection stays
// open, and the overall deadline is swapped for an idle timeout — a fixed
// 30 s limit would kill exactly the long-lived connections streaming is for.
// A stream that is canceled (user abort), idles out or breaks mid-flight
// still returns the partial Response with a warning instead of an error, so
// what arrived stays visible and reaches the history. Non-streaming
// responses behave exactly like Dispatch, callbacks never fire.
func DispatchStream(ctx context.Context, req *httpfile.Request, opts Options, cb StreamCallbacks) (*Response, error) {
	p, err := prepare(ctx, req, opts)
	if err != nil {
		return nil, err
	}
	return p.run(ctx, req.Key(), opts, cb)
}

// Resend executes a stored request snapshot again, byte for byte (#1832): the
// method, URL, headers and body are the ones that produced the response the
// snapshot came with, not whatever the .http file says now. key labels the
// request in errors and in the returned Response. Streaming behaves exactly
// as on the original dispatch — a response that is recognized as a stream
// re-opens as one, callbacks and all — so a re-sent SSE/NDJSON endpoint is
// live again rather than excluded.
func Resend(ctx context.Context, key string, snap *RequestSnapshot, opts Options, cb StreamCallbacks) (*Response, error) {
	p, err := prepareSnapshot(ctx, key, snap, opts)
	if err != nil {
		return nil, err
	}
	return p.run(ctx, key, opts, cb)
}

// run executes a prepared request with stream recognition — the body of
// DispatchStream, shared with Resend.
func (p *prepared) run(ctx context.Context, key string, opts Options, cb StreamCallbacks) (*Response, error) {
	// The client's own Timeout would cover the whole exchange including the
	// body read; deadlines are managed here instead so a recognized stream
	// can shed the overall limit once the headers are in.
	overall := p.client.Timeout
	p.client.Timeout = 0
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	deadline := time.AfterFunc(overall, cancel)
	defer deadline.Stop()

	start := p.now()
	httpResp, err := p.client.Do(p.httpReq.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("request %s: %v", key, err)
	}
	defer httpResp.Body.Close()
	warnings := p.warnings

	if !IsStreamContentType(httpResp.Header.Get("Content-Type")) {
		// Collect mode — the overall deadline stays armed, the behavior is
		// Dispatch's to the letter.
		return p.collectBody(key, httpResp, start)
	}

	// Stream mode: the headers are the first visible result, the body arrives
	// chunk by chunk. The overall deadline is disarmed — a healthy stream may
	// run for minutes — and an idle watchdog takes over: no data for
	// StreamIdleTimeout ends the stream, keeping what arrived.
	deadline.Stop()
	if cb.OnHeaders != nil {
		cb.OnHeaders(httpResp.Status, httpResp.StatusCode, httpResp.Proto, httpResp.Header)
	}
	idle := StreamIdleTimeout
	if opts.StreamIdleTimeout > 0 {
		idle = opts.StreamIdleTimeout
	}
	var idledOut atomic.Bool
	watchdog := time.AfterFunc(idle, func() { idledOut.Store(true); cancel() })
	defer watchdog.Stop()

	var body []byte
	truncated := false
	var readErr error
	buf := make([]byte, 32<<10)
	for {
		n, err := httpResp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(idle)
			keep := n
			if room := MaxBodyBytes - len(body); keep > room {
				keep, truncated = room, true
			}
			if keep > 0 {
				body = append(body, buf[:keep]...)
				if cb.OnChunk != nil {
					cb.OnChunk(append([]byte(nil), buf[:keep]...))
				}
			}
			if truncated {
				break
			}
		}
		if err != nil {
			if err != io.EOF {
				readErr = err
			}
			break
		}
	}
	elapsed := p.now().Sub(start)
	if truncated {
		warnings = append(warnings, fmt.Sprintf("response body exceeded %d bytes and was truncated", MaxBodyBytes))
	}
	switch {
	case idledOut.Load():
		warnings = append(warnings, fmt.Sprintf("stream idle for %s — stopped, showing what arrived", idle))
	case readErr != nil && errors.Is(readErr, context.Canceled):
		warnings = append(warnings, "stream canceled — showing what arrived")
	case readErr != nil:
		warnings = append(warnings, fmt.Sprintf("stream ended early (%v) — showing what arrived", readErr))
	}

	return &Response{
		Status:     httpResp.Status,
		StatusCode: httpResp.StatusCode,
		Proto:      httpResp.Proto,
		Headers:    httpResp.Header,
		Body:       body,
		Truncated:  truncated,
		Duration:   elapsed,
		RequestKey: key,
		Warnings:   warnings,
		Request:    p.snapshot,
	}, nil
}

// applyCurlConfig maps detected .curlrc options onto the request; explicit
// request values win.
func applyCurlConfig(httpReq *http.Request, resolved *httpfile.Request, cfg *curlConfig) {
	for _, raw := range cfg.Headers {
		colon := strings.Index(raw, ":")
		if colon <= 0 {
			continue
		}
		name := strings.TrimSpace(raw[:colon])
		if _, explicit := resolved.Header(name); explicit {
			continue
		}
		httpReq.Header.Set(name, strings.TrimSpace(raw[colon+1:]))
	}
	if cfg.UserAgent != "" && httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", cfg.UserAgent)
	}
	if cfg.Referer != "" && httpReq.Header.Get("Referer") == "" {
		httpReq.Header.Set("Referer", cfg.Referer)
	}
	if cfg.User != "" && httpReq.Header.Get("Authorization") == "" {
		user, pass, _ := strings.Cut(cfg.User, ":")
		httpReq.SetBasicAuth(user, pass)
	}
}

// applyNetrc adds basic-auth credentials from .netrc when the request (and
// .curlrc) set no Authorization header. opts.NetrcPath takes precedence over
// a "netrc-file" path from .curlrc, which in turn takes precedence over the
// default $NETRC / $HOME/.netrc lookup.
func applyNetrc(httpReq *http.Request, cfg *curlConfig, opts Options) error {
	if opts.DisableConfig || httpReq.Header.Get("Authorization") != "" {
		return nil
	}
	path := opts.NetrcPath
	if path == "" {
		path = cfg.NetrcFile
	}
	if path == "" {
		path = netrcPath()
	}
	creds, err := lookupNetrc(path, httpReq.URL.Hostname())
	if err != nil || creds == nil {
		return err
	}
	httpReq.SetBasicAuth(creds.Login, creds.Password)
	return nil
}

// buildClient assembles the http.Client honoring .curlrc proxy/insecure/
// redirect/timeout options over the defaults.
func buildClient(cfg *curlConfig, opts Options) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	if cfg.Proxy != "" {
		if proxyURL, err := url.Parse(cfg.Proxy); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	if cfg.ConnectTimeout > 0 {
		transport.DialContext = (&timeoutDialer{cfg.ConnectTimeout}).DialContext
	}

	timeout := DefaultTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}
	if cfg.MaxTime > 0 {
		timeout = cfg.MaxTime
	}

	client := &http.Client{Transport: transport, Timeout: timeout}
	if cfg.FollowRedirect != nil && !*cfg.FollowRedirect {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}
