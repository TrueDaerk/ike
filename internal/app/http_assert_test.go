package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/explorer"
	"ike/internal/httpclient"
	"ike/internal/httpfile"
	"ike/internal/pane"
	"ike/internal/testresults"
)

// http_assert_test.go covers the editor-level half of the `# @assert`
// directive (#2546): the completion notice counting a failed run, the Test
// Results window receiving the run, the directive-line diagnostic, and the
// window's re-run action dispatching the request again.

// assertResponse is a 200 answer with the given assertion outcomes.
func assertResponse(key string, results ...httpclient.AssertResult) *httpclient.Response {
	r := sampleResponse(key)
	r.Assertions = results
	return r
}

func failedAssertion(line int, subject, op, expected, reason string) httpclient.AssertResult {
	return httpclient.AssertResult{
		Assertion: httpfile.Assertion{Subject: subject, Op: op, Expected: expected, Line: line, EndCol: 24},
		Reason:    reason,
	}
}

func passedAssertion(line int, subject, op, expected string) httpclient.AssertResult {
	return httpclient.AssertResult{
		Assertion: httpfile.Assertion{Subject: subject, Op: op, Expected: expected, Line: line, EndCol: 24},
		Pass:      true,
	}
}

// panelView renders the Test Results window at a size that shows every row.
func panelView(p *testresults.Model) string {
	p.SetSize(120, 30)
	return ansi.Strip(p.View())
}

// A 200 whose assertions failed is a failed run for the off-screen notice:
// it announces itself with the summary, and a passing run stays quiet.
func TestHTTPNotifiesFailedAssertions(t *testing.T) {
	m := httpNotifyApp(t, 0)
	m = landHTTPResponse(t, m, "GET /things", assertResponse("one",
		passedAssertion(2, "status", "==", "200"),
		failedAssertion(3, "jsonpath", "==", "42", "got 41")))
	notice := httpNotice(m)
	for _, want := range []string{"GET /things", "200 OK", "1 of 2 assertions failed"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q must mention %q", notice, want)
		}
	}

	quiet := httpNotifyApp(t, 0)
	quiet = landHTTPResponse(t, quiet, "GET /things", assertResponse("two", passedAssertion(2, "status", "==", "200")))
	if n := httpNotice(quiet); n != "" {
		t.Errorf("a passing run is not worth a notice, got %q", n)
	}
}

// A failing run opens the Test Results window (tests.auto_open) and fills it
// with one row per directive under the request; the window never takes the
// focus.
func TestHTTPAssertionsFillTestResults(t *testing.T) {
	m := httpApp(t)
	if m.testsPanel() != nil {
		t.Fatal("no tests panel yet")
	}
	m = landHTTPResponse(t, m, "GET /things", assertResponse("things",
		passedAssertion(2, "status", "==", "200"),
		failedAssertion(3, "header", "contains", "json", `got "text/html"`)))
	p := m.testsPanel()
	if p == nil {
		t.Fatal("a failing assertion run must open the Test Results window")
	}
	if p.Running() {
		t.Error("the run is finished when it lands")
	}
	if name := p.ConfigName(); !strings.Contains(name, "GET /things") {
		t.Errorf("run name = %q, want the request label", name)
	}
	// group (file) → request → two directives
	if p.Rows() != 4 {
		t.Errorf("rows = %d, want 4", p.Rows())
	}
	view := panelView(p)
	for _, want := range []string{"1 passed", "1 failed", "status == 200", "header contains json"} {
		if !strings.Contains(view, want) {
			t.Errorf("the window must show %q:\n%s", want, view)
		}
	}
	if got := m.activeWS().Panes.Focused(); got == pane.TestsKey {
		t.Error("focus moved to the Test Results window")
	}
	if m.lastTestRun != nil || m.httpAssertRun == nil || m.httpAssertRun.key != "things" {
		t.Errorf("the window's re-run must now mean the request: last=%v http=%v", m.lastTestRun, m.httpAssertRun)
	}
}

// A passing run does not push a second tool window onto the screen — the
// response pane already says so — but it fills the window when it is open.
func TestHTTPAssertionsPassingRunOnlyFillsOpenWindow(t *testing.T) {
	m := httpApp(t)
	m = landHTTPResponse(t, m, "GET /things", assertResponse("things", passedAssertion(2, "status", "==", "200")))
	if m.testsPanel() != nil {
		t.Fatal("a passing run must not open the Test Results window")
	}
	out, _ := m.Update(TestsToggleMsg{})
	m = out.(Model)
	if m.testsPanel() == nil {
		t.Fatal("toggle must open the window")
	}
	m = landHTTPResponse(t, m, "GET /things", assertResponse("things", passedAssertion(2, "status", "==", "200")))
	p := m.testsPanel()
	if p.Rows() != 3 || !strings.Contains(panelView(p), "1 passed") {
		t.Errorf("the open window must take the passing run: rows=%d\n%s", p.Rows(), panelView(p))
	}
}

// A response without directives leaves the window's last run alone.
func TestHTTPResponseWithoutAssertionsKeepsWindow(t *testing.T) {
	m := httpApp(t)
	m = landHTTPResponse(t, m, "GET /things", assertResponse("things", failedAssertion(2, "status", "==", "201", "got 200")))
	rows := m.testsPanel().Rows()
	m = landHTTPResponse(t, m, "GET /other", sampleResponse("other"))
	if m.testsPanel().Rows() != rows {
		t.Errorf("rows changed from %d to %d", rows, m.testsPanel().Rows())
	}
	if !m.activeWS().Panes.Has(pane.TestsKey) {
		t.Error("the window stays")
	}
}

// assertServer answers every request with the given JSON and counts them.
func assertServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// The acceptance case end to end: a request with directives is dispatched
// from its file, the failed directive is marked on its own line, the Test
// Results window lists it with the file location, and the window's re-run
// dispatches the request again.
func TestHTTPAssertionsFromFileMarkAndRerun(t *testing.T) {
	srv, hits := assertServer(t, `{"items":[{"id":41}]}`)
	m := httpApp(t)
	src := "### things\n" +
		"# @assert status == 200\n" +
		"# @assert jsonpath $.items[0].id == 42\n" +
		"GET " + srv.URL + "/things\n"
	path := httpVarFile(t, src, nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	m.activeEditor().SetCursor(3, 0)
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp := drainHTTPResponse(t, cmd)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	if resp.Resp.AssertionsFailed() != 1 {
		t.Fatalf("assertions: %+v", resp.Resp.Assertions)
	}
	out, _ = m.Update(resp)
	m = out.(Model)

	diags := m.httpDiagsFor(path)
	if len(diags) != 1 || diags[0].Range.Start.Line != 2 || !strings.Contains(diags[0].Message, "got 41") {
		t.Fatalf("diagnostics = %+v, want one on the failed directive's line", diags)
	}
	p := m.testsPanel()
	if p == nil {
		t.Fatal("the window must open on the failure")
	}
	if !strings.Contains(panelView(p), "jsonpath $.items[0].id == 42") {
		t.Errorf("the window must list the directive:\n%s", panelView(p))
	}

	// Re-run from the window: the request goes out again.
	before := hits.Load()
	out, cmd = m.Update(testresults.RerunMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("re-run must dispatch the request")
	}
	again := drainHTTPResponse(t, cmd)
	if again.Err != nil {
		t.Fatal(again.Err)
	}
	if hits.Load() != before+1 {
		t.Errorf("the server saw %d requests, want one more than %d", hits.Load(), before)
	}
	out, _ = m.Update(again)
	m = out.(Model)
	if m.testsPanel().Running() {
		t.Error("the re-run's answer must land in the window")
	}
}
