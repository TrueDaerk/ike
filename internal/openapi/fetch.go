package openapi

// fetch.go is the URL half of the OpenAPI import (#2009). The spec of a
// running service usually lives behind a URL, not on disk, so the import
// prompt takes a URL as well as a path: either one that points straight at the
// document (`https://api.example.com/v3/api-docs`) or the plain base URL of
// the service (`https://api.example.com`), whose well-known locations are then
// probed in order until one answers with a parseable OpenAPI 3.x document.
//
// Discovery is deliberately *sequential* with a short per-request timeout: a
// dead host must not hold the dialog for the sum of every probe, and a live
// host should not be hit with a burst of parallel requests just because the
// user typed its origin. A transport failure (no DNS, refused, timed out)
// therefore aborts the whole probe run at the first path — the host is gone,
// the remaining probes would fail identically.
//
// Note the asymmetry with the reader (spec.go), which never fetches an
// external `$ref`: reaching the network here is the user's explicit request
// ("import this URL"), while a `$ref` would be the document's.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// ProbePaths are the well-known spec locations tried under a base URL, in
// order: the OpenAPI conventions first, then Swagger's, then springdoc's
// (`/v3/api-docs`) and the common `/api` prefix. The order decides which
// document a service exporting several of them is imported from, so it is
// fixed and documented (wiki/architecture/http-client.md) rather than guessed
// per host.
var ProbePaths = []string{
	"/openapi.json",
	"/openapi.yaml",
	"/openapi.yml",
	"/swagger.json",
	"/swagger.yaml",
	"/v3/api-docs",
	"/api-docs",
	"/api/openapi.json",
	"/api/openapi.yaml",
}

const (
	// ProbeTimeout bounds a single request, so a base URL costs at most
	// len(ProbePaths)*ProbeTimeout in the worst case — and much less in
	// practice, since an unreachable host ends the run at the first probe.
	ProbeTimeout = 5 * time.Second
	// maxSpecBytes caps what a fetch reads; a document larger than this is
	// not a spec anyone wants generated into a request file.
	maxSpecBytes = 32 << 20
)

// Discovery is a resolved spec document: where it was found, its bytes, and
// the parsed model. The importer generates from Data without fetching again,
// so what the dialog validated is exactly what gets imported.
type Discovery struct {
	// URL is the document's own URL — for a base URL, the probe that hit.
	URL string
	// Data is the fetched document.
	Data []byte
	// Spec is the parsed model.
	Spec *Spec
	// Probed lists the URLs tried before this one answered; empty when the
	// first request already carried the document.
	Probed []string
}

// Name is the file name the document is imported under, e.g. `openapi.json`
// for `https://api.example.com/openapi.json` and the host when the URL has no
// usable last path segment (`https://api.example.com/` → `api.example.com`).
func (d *Discovery) Name() string {
	u, err := url.Parse(d.URL)
	if err != nil {
		return "openapi"
	}
	base := path.Base(strings.TrimSuffix(u.Path, "/"))
	if base == "" || base == "." || base == "/" {
		base = u.Hostname()
	}
	if base == "" {
		base = "openapi"
	}
	return base
}

// IsURL reports whether s addresses the network rather than the filesystem —
// the switch between the prompt's URL flow and its path flow. Only http(s)
// counts: everything else, `file:` included, is a path.
func IsURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// DefaultClient returns the client discovery uses when none is supplied: the
// process transport (system proxy, system roots, HTTP/2) with the probe
// timeout applied as the client timeout — the dispatcher's convention of
// cloning http.DefaultTransport rather than inventing a TLS setup
// (internal/httpclient/dispatch.go).
func DefaultClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{Transport: transport, Timeout: ProbeTimeout}
}

// Discover resolves raw to a parseable OpenAPI 3.x document.
//
// A URL whose path names something — anything but "" or "/" — is fetched
// directly; when that path carries no spec file extension and the fetch does
// not yield a spec, the path becomes the prefix of a probe run, so
// `https://api.example.com/api` still finds `/api/openapi.json`. A bare origin
// goes straight to the probe run over ProbePaths.
//
// The error names what actually went wrong — the transport failure, the HTTP
// status, the parse error, or that every probed path came up empty — since
// that message is all the import dialog can show.
func Discover(ctx context.Context, client *http.Client, raw string) (*Discovery, error) {
	if client == nil {
		client = DefaultClient()
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("not a valid URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%s: only http and https URLs can be imported", raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%s: the URL names no host", raw)
	}

	trimmed := strings.TrimSuffix(u.Path, "/")
	if trimmed == "" {
		return probe(ctx, client, *u, "")
	}
	d, err := fetchSpec(ctx, client, u.String())
	if err == nil {
		return d, nil
	}
	if hasSpecExt(trimmed) || isTransportErr(err) {
		return nil, err // an explicit document, or a host that is not there
	}
	probed, perr := probe(ctx, client, *u, trimmed)
	if perr != nil {
		return nil, err // the named path is the more specific failure
	}
	return probed, nil
}

// probe walks ProbePaths under prefix, returning the first parseable
// document. A transport failure ends the run immediately — the host is
// unreachable, not merely missing that one path.
func probe(ctx context.Context, client *http.Client, base url.URL, prefix string) (*Discovery, error) {
	var tried []string
	var parseErr error
	for _, p := range ProbePaths {
		next := base
		next.Path = prefix + p
		next.RawPath, next.RawQuery, next.Fragment = "", "", ""
		target := next.String()
		d, err := fetchSpec(ctx, client, target)
		switch {
		case err == nil:
			d.Probed = tried
			return d, nil
		case isTransportErr(err):
			return nil, err
		case isParseErr(err) && parseErr == nil:
			parseErr = err // a document answered, it is just not a spec
		}
		tried = append(tried, target)
	}
	if parseErr != nil {
		return nil, parseErr
	}
	origin := base
	origin.Path, origin.RawPath, origin.RawQuery, origin.Fragment = prefix, "", "", ""
	return nil, fmt.Errorf("no OpenAPI document at %s — probed %s",
		strings.TrimSuffix(origin.String(), "/"), strings.Join(ProbePaths, ", "))
}

// fetchSpec GETs one URL and parses the body, bounding the request by
// ProbeTimeout so a hanging host costs one timeout, not the dialog.
func fetchSpec(ctx context.Context, client *http.Client, target string) (*Discovery, error) {
	reqCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, &transportError{url: target, err: err}
	}
	req.Header.Set("Accept", "application/json, application/yaml, text/yaml;q=0.9, */*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, &transportError{url: target, err: unwrapURLErr(err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &statusError{url: target, status: resp.Status}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSpecBytes))
	if err != nil {
		return nil, &transportError{url: target, err: err}
	}
	spec, err := Parse(data)
	if err != nil {
		return nil, &parseError{url: target, err: err}
	}
	return &Discovery{URL: target, Data: data, Spec: spec}, nil
}

// transportError is the network refusing to answer at all: no DNS, refused
// connection, TLS failure, timeout. It is fatal to a probe run.
type transportError struct {
	url string
	err error
}

func (e *transportError) Error() string { return e.url + ": " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

// statusError is a server's non-2xx reply.
type statusError struct {
	url    string
	status string
}

func (e *statusError) Error() string { return fmt.Sprintf("%s: HTTP %s", e.url, e.status) }

// parseError marks a document that was fetched but is not OpenAPI 3.x, so a
// probe run can report it instead of the generic "nothing found".
type parseError struct {
	url string
	err error
}

func (e *parseError) Error() string { return e.url + ": " + e.err.Error() }
func (e *parseError) Unwrap() error { return e.err }

func isParseErr(err error) bool {
	var p *parseError
	return errors.As(err, &p)
}

func isTransportErr(err error) bool {
	var t *transportError
	return errors.As(err, &t)
}

// unwrapURLErr strips the *url.Error wrapper so the message reads as the
// cause ("dial tcp …: connection refused") instead of repeating the URL.
func unwrapURLErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// hasSpecExt reports whether a URL path names a document file rather than a
// route to probe under.
func hasSpecExt(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".json", ".yaml", ".yml":
		return true
	}
	return false
}
