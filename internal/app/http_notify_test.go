package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/config"
	"ike/internal/httpclient"
	"ike/internal/pane"
)

// http_notify_test.go covers the completion notice (#2364): a dispatch that
// failed or flew too long reports itself through the notification channel
// while the response pane is not on screen.

// httpNotifyApp is an app with a known notify threshold and no response pane.
func httpNotifyApp(t *testing.T, slowMs int) Model {
	t.Helper()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := *old
	c.HTTP.NotifySlowMs = slowMs
	config.Set(&c)
	return httpApp(t)
}

// httpNotifyResponse is a response with a chosen status and wall clock.
func httpNotifyResponse(key string, code int, status string, took time.Duration) *httpclient.Response {
	return &httpclient.Response{
		Status: status, StatusCode: code, Proto: "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"ok":false}`),
		Duration:   took,
		RequestKey: key,
	}
}

// landHTTPResponse routes one response through the update loop with a flight
// registered for it, so the notice can name the method like the indicator did.
func landHTTPResponse(t *testing.T, m Model, label string, resp *httpclient.Response) Model {
	t.Helper()
	if m.httpFlight == nil {
		m.httpFlight = map[string]*httpFlightEntry{}
	}
	m.httpFlight[httpFlightKey("", resp.RequestKey)] = &httpFlightEntry{
		label:   label,
		request: resp.RequestKey,
		started: time.Now().Add(-resp.Duration),
		cancel:  func() {},
	}
	out, _ := m.Update(HTTPResponseMsg{Request: resp.RequestKey, Resp: resp})
	return out.(Model)
}

// httpNotice returns the newest notification mentioning "http:", or "".
func httpNotice(m Model) string {
	for _, h := range m.history {
		if strings.HasPrefix(h.text, "http: ") {
			return h.text
		}
	}
	return ""
}

// A non-2xx answer landing while no response pane is on screen announces
// itself with method, status and duration — and lands in the history the
// notifications.history command shows.
func TestHTTPNotifiesFailureWhilePaneHidden(t *testing.T) {
	m := httpNotifyApp(t, 3000)
	if m.httpResponseVisible() {
		t.Fatal("no viewer exists yet, so nothing is visible")
	}
	m = landHTTPResponse(t, m, "GET /things", httpNotifyResponse("one", 404, "404 Not Found", 120*time.Millisecond))
	notice := httpNotice(m)
	for _, want := range []string{"GET /things", "404 Not Found", "120ms"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q must mention %q", notice, want)
		}
	}
	if strings.Contains(notice, "slower than") {
		t.Errorf("a fast failure is not a slow one: %q", notice)
	}
}

// The same failure with the pane on screen says nothing: the status row it
// just filled already carries status and duration.
func TestHTTPQuietWhilePaneVisible(t *testing.T) {
	m := httpNotifyApp(t, 3000)
	// The first response opens the viewer, which is then a visible leaf.
	m = landHTTPResponse(t, m, "GET /things", sampleResponse("one"))
	if !m.httpResponseVisible() {
		t.Fatal("routing a response must leave the viewer visible")
	}
	before := len(m.history)
	m = landHTTPResponse(t, m, "GET /things", httpNotifyResponse("one", 500, "500 Internal Server Error", 9*time.Second))
	if got := len(m.history); got != before {
		t.Fatalf("a visible pane must not notify: %q", httpNotice(m))
	}
}

// A 2xx past the threshold notifies the same way; one below it does not.
func TestHTTPNotifiesSlowSuccess(t *testing.T) {
	m := httpNotifyApp(t, 3000)
	m = landHTTPResponse(t, m, "POST /search", httpNotifyResponse("slow", 200, "200 OK", 4500*time.Millisecond))
	notice := httpNotice(m)
	for _, want := range []string{"POST /search", "200 OK", "4.5s", "slower than 3.0s"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q must mention %q", notice, want)
		}
	}

	m = httpNotifyApp(t, 3000)
	m = landHTTPResponse(t, m, "POST /search", httpNotifyResponse("fast", 200, "200 OK", 200*time.Millisecond))
	if notice := httpNotice(m); notice != "" {
		t.Errorf("a fast 2xx must stay quiet: %q", notice)
	}
}

// The threshold's off value silences the slow branch, and only that branch —
// a failure still reports.
func TestHTTPNotifySlowOff(t *testing.T) {
	m := httpNotifyApp(t, 0)
	m = landHTTPResponse(t, m, "GET /slow", httpNotifyResponse("slow", 200, "200 OK", 30*time.Second))
	if notice := httpNotice(m); notice != "" {
		t.Errorf("threshold 0 turns the slow notice off: %q", notice)
	}

	m = httpNotifyApp(t, 0)
	m = landHTTPResponse(t, m, "GET /gone", httpNotifyResponse("gone", 410, "410 Gone", 30*time.Second))
	if notice := httpNotice(m); !strings.Contains(notice, "410 Gone") {
		t.Errorf("a failure notifies regardless of the threshold: %q", notice)
	}
}

// A canceled dispatch reports the abort, not a failure — the user asked for
// it, and its partial answer is not news.
func TestHTTPNoCompletionNoticeForCancel(t *testing.T) {
	m := httpNotifyApp(t, 1)
	m.httpFlight = map[string]*httpFlightEntry{
		httpFlightKey("", "x"): {label: "GET /x", request: "x", started: time.Now(),
			cancel: func() {}, canceled: true},
	}
	out, _ := m.Update(HTTPResponseMsg{Request: "x", Resp: httpNotifyResponse("x", 503, "503 Service Unavailable", time.Second)})
	m = out.(Model)
	if notice := httpNotice(m); !strings.Contains(notice, "canceled") {
		t.Fatalf("a canceled flight reports the abort: %q", notice)
	}
	for _, h := range m.history {
		if strings.Contains(h.text, "503") {
			t.Errorf("a canceled flight must not also report its status: %q", h.text)
		}
	}
}

// A transport error keeps its own error notice and adds no completion notice:
// there is no status or duration to report.
func TestHTTPNoCompletionNoticeForTransportError(t *testing.T) {
	m := httpNotifyApp(t, 1)
	out, _ := m.Update(HTTPResponseMsg{Request: "x", Err: context.DeadlineExceeded})
	m = out.(Model)
	if n := len(m.history); n != 1 {
		t.Fatalf("history = %d entries, want the single transport error: %+v", n, m.history)
	}
}

// window.hideAllTools (#1271) keeps the viewer registered but takes its leaf
// out of the layout: the response still fills it and the user still sees
// nothing, so the notice fires.
func TestHTTPNotifiesWhileViewerHidden(t *testing.T) {
	m := httpNotifyApp(t, 3000)
	m = landHTTPResponse(t, m, "GET /things", sampleResponse("one"))
	if !m.httpResponseVisible() {
		t.Fatal("routing a response must leave the viewer visible")
	}
	m.toggleToolWindows() // hides every tool window, the viewer included
	if !m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("hiding keeps the viewer registered (#1271)")
	}
	if m.httpResponseVisible() {
		t.Fatal("a viewer without a layout leaf shows nothing")
	}
	m = landHTTPResponse(t, m, "GET /things", httpNotifyResponse("one", 502, "502 Bad Gateway", 40*time.Millisecond))
	if notice := httpNotice(m); !strings.Contains(notice, "502 Bad Gateway") {
		t.Errorf("a failure landing in a hidden viewer must notify: %q", notice)
	}
}
