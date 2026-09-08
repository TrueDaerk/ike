package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor/buffer"
	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/httpfile"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
	"ike/internal/pane"
)

// http_assert.go is the editor-level half of the `# @assert` directive
// (#2546). The dispatcher evaluates the directives and the response pane
// shows the pass/fail block; this file carries the outcome to the two other
// places a failed expectation belongs:
//
//   - the Test Results tool window (#1911), as a run named after the request
//     with one row per directive — so a file of asserting requests reads like
//     a test suite, with jump-to-failure landing on the directive line and
//     `r` re-dispatching the request;
//   - the directive's own line in the .http buffer, as a warning diagnostic
//     (the capture pattern, http_capture.go), so the broken expectation sits
//     where the fix goes.

// httpAssertSource labels the diagnostics in the Problems window and in the
// popup, the way a server name would.
const httpAssertSource = "http assert"

// httpAssertRun remembers the request whose assertions filled the Test
// Results window last, so the window's re-run actions dispatch it again.
type httpAssertRun struct {
	source string
	key    string
}

// assertDiagnostics turns the failed assertions of one response into
// diagnostics anchored at their directive lines.
func assertDiagnostics(results []httpclient.AssertResult) []ilsp.Diagnostic {
	var out []ilsp.Diagnostic
	for _, a := range results {
		if a.Pass || a.Line <= 0 {
			continue
		}
		line := a.Line - 1 // diagnostics are 0-based
		out = append(out, ilsp.Diagnostic{
			Range: buffer.Range{
				Start: buffer.Position{Line: line},
				End:   buffer.Position{Line: line, Col: a.EndCol},
			},
			Severity: protocol.SeverityWarning,
			Message:  "assertion failed: " + a.Describe(),
			Source:   httpAssertSource,
			Code:     "assert",
		})
	}
	return out
}

// reportHTTPAssertions publishes the outcome of one dispatch's assertions:
// the diagnostics of the failed ones on their lines, and the whole set as a
// run in the Test Results window. A response without any directive does
// neither — a request that never asserted must not clear the markers of a
// sibling request that did, nor replace the window's last run.
func (m *Model) reportHTTPAssertions(source string, flight *httpFlightEntry, msg HTTPResponseMsg) tea.Cmd {
	resp := msg.Resp
	if resp == nil || len(resp.Assertions) == 0 {
		return nil
	}
	var cmd tea.Cmd
	if source != "" {
		cmd = m.setHTTPDiags(source, httpAssertSource, assertDiagnostics(resp.Assertions))
	}
	m.fillTestsWithAssertions(source, flight, msg)
	return cmd
}

// fillTestsWithAssertions hands the run to the Test Results window. The
// window fills whenever it is open; a *failing* run opens it when
// tests.auto_open is on, the way a captured test run does — a passing run
// stays in the response pane, which already says so, rather than pushing a
// second tool window onto the screen. Focus stays where the user was.
func (m *Model) fillTestsWithAssertions(source string, flight *httpFlightEntry, msg HTTPResponseMsg) {
	resp := msg.Resp
	if m.testsPanel() == nil {
		if resp.AssertionsFailed() == 0 || !config.Get().Tests.AutoOpen {
			return
		}
		m.ensurePanel(pane.TestsKey, func() tea.Cmd { m.openTestsPanel(); return nil })
		if back := m.panelReturnFocus[pane.TestsKey]; back != "" && m.activeWS().Panes.Has(back) {
			m.setFocus(back)
		}
	}
	p := m.testsPanel()
	if p == nil {
		return
	}
	name := msg.Request
	if flight != nil && flight.label != "" {
		name = flight.label
	}
	dir := ""
	if source != "" {
		dir = filepath.Dir(source)
	}
	// The window's re-run actions now mean "dispatch this request again";
	// the previous captured test run, if any, is no longer what `r` repeats.
	m.lastTestRun = nil
	m.httpAssertRun = &httpAssertRun{source: source, key: msg.Request}
	p.StartRun("http: "+name, dir)
	p.FinishRun(assertTestResults(source, msg.Request, resp), assertRunOutput(name, resp))
}

// assertTestResults maps the directives onto the result tree: the .http file
// is the group, the request the test, each directive a subtest named by its
// text. A "/" in a directive (a regex, a media type) would nest another level,
// so it is replaced by the division slash.
func assertTestResults(source, key string, resp *httpclient.Response) []lang.TestResult {
	group := "http"
	if source != "" {
		group = filepath.Base(source)
	}
	out := make([]lang.TestResult, 0, len(resp.Assertions))
	for i, a := range resp.Assertions {
		status := lang.TestPass
		if !a.Pass {
			status = lang.TestFail
		}
		leaf := strings.ReplaceAll(a.Assertion.String(), "/", "∕")
		r := lang.TestResult{
			Group:   group,
			Name:    key + "/" + fmt.Sprintf("%d %s", i+1, leaf),
			Status:  status,
			Output:  assertDetail(a),
			RerunID: key,
		}
		if a.Line > 0 && source != "" {
			r.File, r.Line = source, a.Line
		}
		out = append(out, r)
	}
	return out
}

// assertDetail is the selected row's detail text: the directive, the
// outcome, and what the response actually gave.
func assertDetail(a httpclient.AssertResult) string {
	lines := []string{a.Assertion.String()}
	if a.Pass {
		lines = append(lines, "passed")
	} else {
		lines = append(lines, "FAILED: "+a.Reason)
	}
	if a.Actual != "" {
		lines = append(lines, "actual: "+a.Actual)
	}
	if a.Line > 0 {
		lines = append(lines, fmt.Sprintf("directive at line %d", a.Line))
	}
	return strings.Join(lines, "\n")
}

// assertRunOutput is the run's raw output, the text `o` shows: the status
// line and one row per directive, the pass/fail block as plain text.
func assertRunOutput(name string, resp *httpclient.Response) string {
	lines := []string{fmt.Sprintf("%s → %s %s (%s)", name, resp.Proto, resp.Status, formatElapsed(resp.Duration))}
	for _, a := range resp.Assertions {
		mark := "PASS"
		if !a.Pass {
			mark = "FAIL"
		}
		lines = append(lines, mark+"  "+a.Describe())
	}
	lines = append(lines, resp.AssertionSummary())
	return strings.Join(lines, "\n")
}

// rerunHTTPAssertions is what the Test Results window's re-run actions do
// after an assertion run filled it (#2546): the request is parsed from its
// file again and dispatched, so an edited directive is picked up. There is
// no "failed only" subset — the response is one exchange, and every directive
// runs over it.
func (m *Model) rerunHTTPAssertions() tea.Cmd {
	run := m.httpAssertRun
	if run == nil {
		return nil
	}
	if run.source == "" {
		m.host.Notify(host.Info, "http: "+run.key+" has no file to re-run from")
		return nil
	}
	text := m.httpSourceText(run.source)
	if text == "" {
		m.host.Notify(host.Info, "http: "+filepath.Base(run.source)+" cannot be read")
		return nil
	}
	f := httpfile.Parse(text)
	for _, r := range f.Requests {
		if r.Key() == run.key {
			return m.dispatchHTTPRequest(run.source, f, r)
		}
	}
	m.host.Notify(host.Info, "http: "+run.key+" is no longer in "+filepath.Base(run.source))
	return nil
}
