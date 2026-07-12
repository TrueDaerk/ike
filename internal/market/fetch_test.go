package market

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeTransport serves canned responses by URL without touching the network.
type fakeTransport struct {
	responses map[string]fakeResponse
	requests  []string
}

type fakeResponse struct {
	status int
	body   []byte
}

func (t *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, req.URL.String())
	r, ok := t.responses[req.URL.String()]
	if !ok {
		r = fakeResponse{status: http.StatusNotFound}
	}
	return &http.Response{
		StatusCode: r.status,
		Status:     http.StatusText(r.status),
		Body:       io.NopCloser(bytes.NewReader(r.body)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func TestFetchIndex(t *testing.T) {
	ft := &fakeTransport{responses: map[string]fakeResponse{
		"https://cat.example/index.json": {status: 200, body: []byte(index(goodEntry))},
	}}
	idx, diags, err := NewClientWith(ft).FetchIndex(context.Background(), "https://cat.example/index.json")
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if len(diags) != 0 || len(idx.Plugins) != 1 {
		t.Fatalf("plugins=%d diags=%v", len(idx.Plugins), diags)
	}
}

func TestFetchIndexRejectsHTTP(t *testing.T) {
	ft := &fakeTransport{responses: map[string]fakeResponse{}}
	if _, _, err := NewClientWith(ft).FetchIndex(context.Background(), "http://cat.example/index.json"); err == nil {
		t.Fatal("want error for non-https URL")
	}
	if len(ft.requests) != 0 {
		t.Fatalf("request went out despite scheme rejection: %v", ft.requests)
	}
}

func TestFetchIndexHTTPError(t *testing.T) {
	ft := &fakeTransport{responses: map[string]fakeResponse{
		"https://cat.example/index.json": {status: 500},
	}}
	if _, _, err := NewClientWith(ft).FetchIndex(context.Background(), "https://cat.example/index.json"); err == nil {
		t.Fatal("want error for HTTP 500")
	}
}

func TestFetchIndexSizeCap(t *testing.T) {
	huge := `{"version": 1, "plugins": [], "pad": "` + strings.Repeat("x", maxIndexBytes) + `"}`
	ft := &fakeTransport{responses: map[string]fakeResponse{
		"https://cat.example/index.json": {status: 200, body: []byte(huge)},
	}}
	if _, _, err := NewClientWith(ft).FetchIndex(context.Background(), "https://cat.example/index.json"); err == nil {
		t.Fatal("want error for oversized index")
	}
}
