// Package httpclient dispatches parsed .http request blocks (#1249, epic
// #1247): it substitutes environment placeholders, applies local client
// configuration (.netrc credentials, .curlrc options) and executes the
// request, returning a Response the viewer and history layers consume.
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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
}

// requestBody returns the reader for a resolved request's body: the inline
// text, the contents of an external `< ./file` body (#1305), or — when the
// Content-Type declares a multipart boundary — the hand-written multipart
// structure normalised to CRLF with per-part `< file` directives embedded
// (#1707). Bodies are assembled up front rather than streamed so their length
// is known (no chunked encoding).
func requestBody(resolved *httpfile.Request, opts Options, lookup func(string) (string, bool)) (io.Reader, error) {
	if resolved.BodyFile != "" {
		data, err := loadBodyFile(resolved.BodyFile, resolved.BodyFileSubstitute, opts, lookup)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(data), nil
	}
	if ct, ok := resolved.Header("Content-Type"); ok {
		if boundary, ok := httpfile.MultipartBoundary(ct); ok {
			data, err := httpfile.BuildMultipartBody(resolved.Body, boundary,
				func(path string, substitute bool) ([]byte, error) {
					return loadBodyFile(path, substitute, opts, lookup)
				})
			if err != nil {
				return nil, err
			}
			return bytes.NewReader(data), nil
		}
	}
	return strings.NewReader(resolved.Body), nil
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

// Dispatch resolves placeholders in req, applies local configuration and
// executes it. Unresolved placeholders or transport failures return an
// error; HTTP error statuses are regular responses.
func Dispatch(ctx context.Context, req *httpfile.Request, opts Options) (*Response, error) {
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

	reqBody, err := requestBody(resolved, opts, lookup)
	if err != nil {
		return nil, fmt.Errorf("request %s: %v", req.Key(), err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, resolved.Method, target.String(), reqBody)
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
	if err := applyNetrc(httpReq, resolved, opts); err != nil {
		return nil, err
	}

	client := buildClient(cfg, opts)
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request %s: %v", req.Key(), err)
	}
	defer httpResp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(httpResp.Body, MaxBodyBytes+1))
	elapsed := now().Sub(start)
	if readErr != nil {
		return nil, fmt.Errorf("request %s: reading response: %v", req.Key(), readErr)
	}
	truncated := false
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
		RequestKey: req.Key(),
		Warnings:   warnings,
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
// .curlrc) set no Authorization header.
func applyNetrc(httpReq *http.Request, resolved *httpfile.Request, opts Options) error {
	if opts.DisableConfig || httpReq.Header.Get("Authorization") != "" {
		return nil
	}
	path := opts.NetrcPath
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
