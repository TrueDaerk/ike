package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ike/internal/httpfile"
)

// capture_test.go covers the dispatch side of the capture directive (#1993):
// what a response yields, what a failed capture says, and that neither ever
// endangers the exchange itself.

// jsonServer answers every request with the given body.
func jsonServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDispatchCaptures: the directives of a request run over the response and
// their values reach the caller through Response.CapturedValues.
func TestDispatchCaptures(t *testing.T) {
	srv := jsonServer(t, "application/json", `{"task":"node-1:42","stats":{"total":7}}`)
	req := parseOne(t, "# @capture task = .task\n# @capture total = .stats.total\nGET "+srv.URL+"/reindex\n")
	resp, err := Dispatch(context.Background(), req, noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Captures) != 2 {
		t.Fatalf("captures: %+v", resp.Captures)
	}
	for _, c := range resp.Captures {
		if !c.OK() {
			t.Errorf("capture %s failed: %s", c.Name, c.Err)
		}
	}
	got := resp.CapturedValues()
	if got["task"] != "node-1:42" || got["total"] != "7" {
		t.Errorf("captured %v", got)
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("a successful capture warns about nothing, got %v", resp.Warnings)
	}
}

// TestDispatchCaptureFailureWarns: a path that matches nothing, a body that is
// not JSON and a broken expression each warn — and the response itself is
// delivered untouched, because it arrived and is worth reading.
func TestDispatchCaptureFailureWarns(t *testing.T) {
	cases := []struct {
		name, contentType, body, directive, want string
	}{
		{"no match", "application/json", `{"task":"x"}`, "# @capture id = .missing", "matched no value"},
		{"not JSON", "text/html", "<html>error</html>", "# @capture id = .id", "not JSON"},
		{"empty body", "application/json", "", "# @capture id = .id", "body is empty"},
		{"broken expression", "application/json", `{"id":1}`, "# @capture id = .id |", "invalid jq expression"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := jsonServer(t, c.contentType, c.body)
			req := parseOne(t, c.directive+"\nGET "+srv.URL+"/x\n")
			resp, err := Dispatch(context.Background(), req, noConfig)
			if err != nil {
				t.Fatalf("a failed capture must not fail the dispatch: %v", err)
			}
			if resp.StatusCode != 200 {
				t.Errorf("status %d — the response must arrive regardless", resp.StatusCode)
			}
			if len(resp.Captures) != 1 || resp.Captures[0].OK() {
				t.Fatalf("captures: %+v", resp.Captures)
			}
			if !strings.Contains(resp.Captures[0].Err, c.want) {
				t.Errorf("error %q does not mention %q", resp.Captures[0].Err, c.want)
			}
			if len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "capture id") {
				t.Errorf("warnings: %v", resp.Warnings)
			}
			if resp.CapturedValues() != nil {
				t.Errorf("a failed capture stores nothing, got %v", resp.CapturedValues())
			}
		})
	}
}

// TestDispatchCapturePartialFailure: directives are independent — the one
// that matched keeps its value while the broken one complains.
func TestDispatchCapturePartialFailure(t *testing.T) {
	srv := jsonServer(t, "application/json", `{"id":"abc"}`)
	req := parseOne(t, "# @capture id = .id\n# @capture node = .node\nGET "+srv.URL+"/x\n")
	resp, err := Dispatch(context.Background(), req, noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if v := resp.CapturedValues(); len(v) != 1 || v["id"] != "abc" {
		t.Errorf("captured %v, want only id", v)
	}
	if len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "capture node") {
		t.Errorf("warnings: %v", resp.Warnings)
	}
}

// TestDispatchWithoutCaptures: a request declaring none carries none — the
// overwhelmingly common case allocates nothing and publishes nothing.
func TestDispatchWithoutCaptures(t *testing.T) {
	srv := jsonServer(t, "application/json", `{"id":1}`)
	req := parseOne(t, "GET "+srv.URL+"/x\n")
	resp, err := Dispatch(context.Background(), req, noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Captures != nil || resp.CapturedValues() != nil {
		t.Errorf("captures: %+v", resp.Captures)
	}
}

// TestCaptureFeedsNextRequest: the whole point of the feature — a value taken
// out of one response resolves the `{{name}}` of the next request, and it
// beats an `@name=` definition of the same name (#1993 precedence).
func TestCaptureFeedsNextRequest(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"task":"node-1:42"}`))
	}))
	defer srv.Close()

	src := "@task = not-started-yet\n\n" +
		"### start\n# @capture task = .task\nPOST " + srv.URL + "/reindex\n\n" +
		"### poll\nGET " + srv.URL + "/_tasks/{{task}}\n"
	f := httpfile.Parse(src)
	if len(f.Errors) != 0 || len(f.Requests) != 2 {
		t.Fatalf("parse: errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	opts := noConfig
	opts.Vars = &httpfile.Vars{File: f.VarMap()}
	first, err := Dispatch(context.Background(), f.Requests[0], opts)
	if err != nil {
		t.Fatal(err)
	}

	opts.Vars = &httpfile.Vars{File: f.VarMap(), Captured: first.CapturedValues()}
	if _, err := Dispatch(context.Background(), f.Requests[1], opts); err != nil {
		t.Fatal(err)
	}
	if seen != "/_tasks/node-1:42" {
		t.Errorf("second request went to %q, want the captured task id", seen)
	}
}
