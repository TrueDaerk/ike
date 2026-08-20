package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/httpfile"
	"ike/internal/registry"
)

// freshHTTPApp is a second model over the *same* project state as httpApp's —
// it does not re-point IKE_CONFIG_DIR — which is how a test can ask what
// survives a restart.
func freshHTTPApp(t *testing.T) Model {
	t.Helper()
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

// http_capture_test.go covers the editor-level half of the capture directive
// (#1993): a captured value reaching the *next* request of the file, and a
// failed capture showing up on its own directive line.

// captureServer answers the first request with the given JSON and echoes the
// path of every later one, so a test can see what the second request sent.
func captureServer(t *testing.T, first string) (*httptest.Server, *atomic.Value) {
	t.Helper()
	var seen atomic.Value
	seen.Store("")
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if n.Add(1) == 1 {
			w.Write([]byte(first))
			return
		}
		seen.Store(r.URL.Path)
		w.Write([]byte(`{"done":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// TestHTTPCaptureFeedsLaterRequest: the acceptance case — the first request
// captures a task id, the second one substitutes it into its target, and the
// captured value beats the file's own `@task=` definition of the same name.
func TestHTTPCaptureFeedsLaterRequest(t *testing.T) {
	srv, seen := captureServer(t, `{"task":"node-1:42"}`)
	m := httpApp(t)
	src := "@task = none\n\n" +
		"### start\n" +
		"# @capture task = .task\n" +
		"POST " + srv.URL + "/_reindex\n" +
		"\n" +
		"### poll\n" +
		"GET " + srv.URL + "/_tasks/{{task}}\n"
	path := httpVarFile(t, src, nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	m.activeEditor().SetCursor(4, 0) // the "start" request
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	first := drainHTTPResponse(t, cmd)
	if first.Err != nil {
		t.Fatal(first.Err)
	}
	if v := first.Resp.CapturedValues(); v["task"] != "node-1:42" {
		t.Fatalf("captured %v", v)
	}
	out, _ = m.Update(first)
	m = out.(Model)

	m.activeEditor().SetCursor(7, 0) // the "poll" request
	out, cmd = m.Update(HTTPRunMsg{})
	m = out.(Model)
	if second := drainHTTPResponse(t, cmd); second.Err != nil {
		t.Fatal(second.Err)
	}
	if got := seen.Load().(string); got != "/_tasks/node-1:42" {
		t.Errorf("the poll request went to %q, want the captured task id", got)
	}
}

// TestHTTPCaptureSurvivesReopen: captured values live with the stored
// response, so a fresh model over the same project resolves `{{task}}`
// without dispatching the capturing request again.
func TestHTTPCaptureSurvivesReopen(t *testing.T) {
	srv, seen := captureServer(t, `{"task":"node-1:42"}`)
	m := httpApp(t)
	src := "### start\n# @capture task = .task\nPOST " + srv.URL + "/_reindex\n\n" +
		"### poll\nGET " + srv.URL + "/_tasks/{{task}}\n"
	path := httpVarFile(t, src, nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	m.activeEditor().SetCursor(2, 0)
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	out, _ = m.Update(drainHTTPResponse(t, cmd))
	m = out.(Model)

	// A new model — same project directory (IKE_CONFIG_DIR is per test), so
	// the same .ike/http/ history: nothing else carries the value over.
	fresh := freshHTTPApp(t)
	out, _ = fresh.Update(explorer.OpenFileMsg{Path: path})
	fresh = out.(Model)
	fresh.activeEditor().SetCursor(5, 0)
	out, cmd = fresh.Update(HTTPRunMsg{})
	fresh = out.(Model)
	if resp := drainHTTPResponse(t, cmd); resp.Err != nil {
		t.Fatalf("the restored capture must resolve {{task}}: %v", resp.Err)
	}
	if got := seen.Load().(string); got != "/_tasks/node-1:42" {
		t.Errorf("the poll request went to %q, want the restored task id", got)
	}
}

// TestHTTPCaptureFailureMarksDirective: a directive that matches nothing
// warns on its own line — the request itself still completes and its response
// still shows.
func TestHTTPCaptureFailureMarksDirective(t *testing.T) {
	srv, _ := captureServer(t, `{"other":1}`)
	m := httpApp(t)
	path := httpVarFile(t, "# @capture task = .task\nGET "+srv.URL+"/x\n", nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp := drainHTTPResponse(t, cmd)
	if resp.Err != nil {
		t.Fatalf("a failed capture must not fail the request: %v", resp.Err)
	}
	out, _ = m.Update(resp)
	m = out.(Model)

	p := m.httpPanel()
	if p == nil {
		t.Fatal("the response must still be shown")
	}
	if warns := strings.Join(p.Warnings(), "\n"); !strings.Contains(warns, "capture task") {
		t.Errorf("viewer warnings: %q", warns)
	}
	errs, warns := m.activeEditor().DiagnosticCounts()
	if errs != 0 || warns != 1 {
		t.Fatalf("editor diagnostics: %d errors, %d warnings; want 1 warning", errs, warns)
	}
	// On the directive's line (0-based 0), not on the request line.
	diags := m.probStore.Get(path)
	if len(diags) != 1 || diags[0].Range.Start.Line != 0 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	if !strings.Contains(diags[0].Message, "matched no value") {
		t.Errorf("message: %q", diags[0].Message)
	}
}

// TestHTTPCaptureSuccessClearsTheMarker: a re-run that works removes the
// previous complaint — the marker tracks the current state of the directive.
func TestHTTPCaptureSuccessClearsTheMarker(t *testing.T) {
	var body atomic.Value
	body.Store(`{"other":1}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body.Load().(string)))
	}))
	defer srv.Close()

	m := httpApp(t)
	path := httpVarFile(t, "# @capture task = .task\nGET "+srv.URL+"/x\n", nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	out, _ = m.Update(drainHTTPResponse(t, cmd))
	m = out.(Model)
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 1 {
		t.Fatalf("the failing capture must be marked, got %d warnings", warns)
	}

	body.Store(`{"task":"n1:42"}`)
	out, cmd = m.Update(HTTPRunMsg{})
	m = out.(Model)
	out, _ = m.Update(drainHTTPResponse(t, cmd))
	m = out.(Model)
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 0 {
		t.Errorf("a working capture must clear the marker, got %d warnings", warns)
	}
}

// TestCaptureDiagnosticsSkipSuccesses: only failures are reported, and each
// one spans its directive line.
func TestCaptureDiagnosticsSkipSuccesses(t *testing.T) {
	results := []httpclient.CaptureResult{
		{Capture: httpfile.Capture{Name: "ok", Expr: ".a", Line: 2, EndCol: 17}, Value: "1"},
		{Capture: httpfile.Capture{Name: "bad", Expr: ".b", Line: 4, EndCol: 18}, Err: "the expression matched no value"},
	}
	diags := captureDiagnostics(results)
	if len(diags) != 1 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	d := diags[0]
	if d.Range.Start.Line != 3 || d.Range.End.Line != 3 || d.Range.End.Col != 18 {
		t.Errorf("range: %+v", d.Range)
	}
	if d.Severity != 2 || d.Source != httpCaptureSource {
		t.Errorf("severity %d source %q", d.Severity, d.Source)
	}
	if !strings.Contains(d.Message, "capture bad") {
		t.Errorf("message: %q", d.Message)
	}
}
