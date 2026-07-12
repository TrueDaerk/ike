package market

// fetch.go is the marketplace's HTTP layer (Roadmap 0310, #444): a small
// client with a hard timeout and a body-size cap that fetches and parses the
// catalog index. The transport is injectable so tests never touch the
// network; production uses http.DefaultTransport.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// fetchTimeout bounds one catalog or artifact request end to end.
const fetchTimeout = 15 * time.Second

// maxIndexBytes caps the catalog document; anything larger is rejected as
// malformed rather than buffered.
const maxIndexBytes = 4 << 20 // 4 MiB

// Client fetches marketplace resources over HTTPS.
type Client struct {
	http *http.Client
}

// NewClient returns a Client with the default transport and timeout.
func NewClient() *Client {
	return NewClientWith(http.DefaultTransport)
}

// NewClientWith returns a Client using the given transport; tests inject a
// fake RoundTripper here.
func NewClientWith(rt http.RoundTripper) *Client {
	return &Client{http: &http.Client{Transport: rt, Timeout: fetchTimeout}}
}

// FetchIndex downloads and parses the catalog at url. It returns the
// validated index, per-entry diagnostics, and an error when the fetch or the
// document as a whole fails.
func (c *Client) FetchIndex(ctx context.Context, url string) (Index, []string, error) {
	data, err := c.get(ctx, url, maxIndexBytes)
	if err != nil {
		return Index{}, nil, fmt.Errorf("catalog: %w", err)
	}
	return ParseIndex(data)
}

// get performs one HTTPS GET with the size cap applied to the body.
func (c *Client) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	if err := checkHTTPS(url); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("GET %s: response exceeds %d bytes", url, limit)
	}
	return data, nil
}
