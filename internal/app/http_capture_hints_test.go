package app

// http_capture_hints_test.go covers the captured-value hints of #3120: the
// value of each `# @capture` line shows at its end after a dispatch and after
// a reopen, follows the newest response, masks secret-suspect names, leaves a
// failed directive to its warning, and goes away with http.capture_hints off.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"ike/internal/config"
	"ike/internal/explorer"
	"ike/internal/secret"
)

// captureHintRun opens path in m, puts the caret on line and dispatches the
// request there, delivering the response.
func captureHintRun(t *testing.T, m Model, line int) Model {
	t.Helper()
	m.activeEditor().SetCursor(line, 0)
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp := drainHTTPResponse(t, cmd)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	out, _ = m.Update(resp)
	return out.(Model)
}

func captureHintOpen(t *testing.T, m Model, path string) Model {
	t.Helper()
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return out.(Model)
}

// jsonServer answers every request with the body bodies[path] (or the
// fallback), so each request of a file can capture something different.
func jsonServer(t *testing.T, bodies map[string]*atomic.Value) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(bodies[r.URL.Path].Load().(string)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func body(s string) *atomic.Value {
	var v atomic.Value
	v.Store(s)
	return &v
}

// TestHTTPCaptureHintsAfterDispatch: two working directives show their
// values, the one matching nothing keeps its warning and shows no value; the
// hints survive a reopen through a fresh model over the same history.
func TestHTTPCaptureHintsAfterDispatch(t *testing.T) {
	srv := jsonServer(t, map[string]*atomic.Value{"/a": body(`{"task":"node-1:42","stats":{"total":7}}`)})
	m := httpApp(t)
	src := "### start\n" +
		"# @capture task = .task\n" +
		"# @capture total = .stats.total\n" +
		"# @capture gone = .missing\n" +
		"GET " + srv.URL + "/a\n"
	path := httpVarFile(t, src, nil)
	m = captureHintOpen(t, m, path)
	if m.activeEditor().HasLineNotes() {
		t.Fatal("a directive that never ran shows no hint")
	}
	m = captureHintRun(t, m, 4)

	ed := m.activeEditor()
	if got, _ := ed.LineNoteAt(1); got != "= node-1:42" {
		t.Errorf("task hint = %q", got)
	}
	if got, _ := ed.LineNoteAt(2); got != "= 7" {
		t.Errorf("total hint = %q", got)
	}
	if got, ok := ed.LineNoteAt(3); ok {
		t.Errorf("the failed directive must show no value, got %q", got)
	}
	if _, warns := ed.DiagnosticCounts(); warns != 1 {
		t.Errorf("the failed directive keeps its warning, got %d warnings", warns)
	}
	if v := plainView(m); !strings.Contains(v, "# @capture task = .task  = node-1:42") {
		t.Errorf("the hint must render at the line end:\n%s", v)
	}

	fresh := captureHintOpen(t, freshHTTPApp(t), path)
	if got, _ := fresh.activeEditor().LineNoteAt(1); got != "= node-1:42" {
		t.Errorf("after reopen the task hint = %q, want the stored value", got)
	}
}

// TestHTTPCaptureHintsNewestResponseWins: a name captured by two requests
// shows the newer response's value on both directive lines.
func TestHTTPCaptureHintsNewestResponseWins(t *testing.T) {
	srv := jsonServer(t, map[string]*atomic.Value{
		"/one": body(`{"id":"first"}`),
		"/two": body(`{"id":"second"}`),
	})
	m := httpApp(t)
	src := "### one\n# @capture id = .id\nGET " + srv.URL + "/one\n\n" +
		"### two\n# @capture id = .id\nGET " + srv.URL + "/two\n"
	path := httpVarFile(t, src, nil)
	m = captureHintOpen(t, m, path)
	m = captureHintRun(t, m, 2)
	if got, _ := m.activeEditor().LineNoteAt(1); got != "= first" {
		t.Fatalf("after the first request the hint = %q", got)
	}
	m = captureHintRun(t, m, 6)
	ed := m.activeEditor()
	for _, line := range []int{1, 5} {
		if got, _ := ed.LineNoteAt(line); got != "= second" {
			t.Errorf("line %d hint = %q, want the newer value", line, got)
		}
	}
}

// TestHTTPCaptureHintsMaskSecrets: a token shows masked, an id plain.
func TestHTTPCaptureHintsMaskSecrets(t *testing.T) {
	srv := jsonServer(t, map[string]*atomic.Value{"/a": body(`{"access_token":"s3cr3t","id":"u-1"}`)})
	m := httpApp(t)
	src := "# @capture token = .access_token\n# @capture id = .id\nGET " + srv.URL + "/a\n"
	path := httpVarFile(t, src, nil)
	m = captureHintRun(t, captureHintOpen(t, m, path), 2)
	ed := m.activeEditor()
	if got, _ := ed.LineNoteAt(0); got != "= "+secret.Mask {
		t.Errorf("token hint = %q, want masked", got)
	}
	if got, _ := ed.LineNoteAt(1); got != "= u-1" {
		t.Errorf("id hint = %q, want plain", got)
	}
	if v := plainView(m); !strings.Contains(v, "# @capture token = .access_token  = "+secret.Mask) {
		t.Errorf("the directive line must render the mask:\n%s", v)
	}
}

// TestHTTPCaptureHintsSettingOff: http.capture_hints = false removes the
// hints, and turning it back on restores them.
func TestHTTPCaptureHintsSettingOff(t *testing.T) {
	srv := jsonServer(t, map[string]*atomic.Value{"/a": body(`{"id":"u-1"}`)})
	m := httpApp(t)
	path := httpVarFile(t, "# @capture id = .id\nGET "+srv.URL+"/a\n", nil)
	m = captureHintRun(t, captureHintOpen(t, m, path), 1)
	if !m.activeEditor().HasLineNotes() {
		t.Fatal("the hint must show with the default setting")
	}
	prev := config.Get()
	t.Cleanup(func() { config.Set(prev) })
	off := *prev
	off.HTTP.CaptureHints = false
	m.reloadConfig(&off)
	if m.activeEditor().HasLineNotes() {
		t.Fatal("http.capture_hints = false must clear the hints")
	}
	m = captureHintRun(t, m, 1)
	if m.activeEditor().HasLineNotes() {
		t.Fatal("a dispatch with the setting off must not add hints")
	}
	on := *prev
	m.reloadConfig(&on)
	if got, _ := m.activeEditor().LineNoteAt(0); got != "= u-1" {
		t.Errorf("re-enabled hint = %q", got)
	}
}

// TestCaptureHintValue: long values are cut to 60 runes plus an ellipsis,
// line breaks read as ⏎, secret-suspect names mask.
func TestCaptureHintValue(t *testing.T) {
	long := strings.Repeat("ä", 200)
	if got, want := captureHintValue("id", long), strings.Repeat("ä", 60)+"…"; got != want {
		t.Errorf("long value = %q", got)
	}
	if got := captureHintValue("id", "a\nb\r\nc"); got != "a⏎b⏎c" {
		t.Errorf("multi-line value = %q", got)
	}
	if got := captureHintValue("short", "x"); got != "x" {
		t.Errorf("short value = %q", got)
	}
	for _, name := range []string{"token", "password", "api_key"} {
		if got := captureHintValue(name, "s3cr3t"); got != secret.Mask {
			t.Errorf("%s = %q, want masked", name, got)
		}
	}
}
