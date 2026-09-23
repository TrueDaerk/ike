package app

import (
	"context"
	"testing"
	"time"

	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/telemetry"
)

// TestTelemetryHTTPFlightCarriesRedirects: the end event says how many
// redirects the flight followed (#2716) — without it, three hops' worth of
// accumulated setup phases reads as one slow host. The count only: no URL,
// host or Location ever reaches the log.
func TestTelemetryHTTPFlightCarriesRedirects(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	send := func(ctx context.Context, source, key string, cb httpclient.WSCallbacks) (*httpclient.Response, error) {
		return nil, nil // never executed: the returned tea.Cmd is not run
	}
	if cmd := m.dispatchHTTP("a.http", "GET /x", "GET /x", false, send); cmd == nil {
		t.Fatal("dispatch refused")
	}
	tm, _ := m.Update(HTTPResponseMsg{Source: "a.http", Request: "GET /x",
		Resp: &httpclient.Response{
			Status: "200 OK", StatusCode: 200,
			Timing: &httpclient.Timing{TTFB: 210 * time.Millisecond},
			Redirects: []httpclient.Hop{
				{Method: "GET", URL: "http://example.com/a", Status: 301, Location: "https://example.com/b"},
				{Method: "GET", URL: "https://example.com/b", Status: 302, Location: "https://example.com/c"},
				{Method: "GET", URL: "https://example.com/c", Status: 200},
			},
			FinalURL: "https://example.com/c",
		}})
	m = tm.(Model)

	ops := opsOf(usageEvents(t, m), telemetry.OpHTTPFlight)
	if len(ops) != 2 {
		t.Fatalf("want start + end ops, got %v", ops)
	}
	end := ops[1]
	if end.Data["redirects"] != "2" {
		t.Fatalf("redirects = %q, want 2 (event: %v)", end.Data["redirects"], end)
	}
	for _, v := range end.Data {
		if v == "https://example.com/c" || v == "example.com" {
			t.Fatalf("the flight event leaked a URL or host: %v", end)
		}
	}
}

// TestTelemetryHTTPFlightWithoutRedirects: a direct answer adds no field, so
// its absence on v15 reads as zero rather than as a lost one (#2716).
func TestTelemetryHTTPFlightWithoutRedirects(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	send := func(ctx context.Context, source, key string, cb httpclient.WSCallbacks) (*httpclient.Response, error) {
		return nil, nil
	}
	m.dispatchHTTP("a.http", "GET /x", "GET /x", false, send)
	tm, _ := m.Update(HTTPResponseMsg{Source: "a.http", Request: "GET /x",
		Resp: &httpclient.Response{Status: "200 OK", StatusCode: 200}})
	m = tm.(Model)

	ops := eventsOf(usageEvents(t, m), telemetry.TypeOp)
	end := ops[len(ops)-1]
	if _, ok := end.Data["redirects"]; ok {
		t.Fatalf("a direct answer reported a hop count: %v", end)
	}
}
