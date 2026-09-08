package httpclient

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/httpfile"
)

// assert_test.go covers the dispatch side of the assertion directive (#2546):
// which subject yields what, how the operators compare, and that a failed
// assertion marks the run without touching the exchange.

// assertResponse is a response with a chosen status, headers, body and wall
// clock, evaluated against one directive.
func assertOne(t *testing.T, line string, resp *Response) AssertResult {
	t.Helper()
	a, ok := httpfile.AssertDirective(line)
	if !ok {
		t.Fatalf("%q is not a directive", line)
	}
	return evalAssertion(a, resp, resp.Body)
}

func sampleResponse() *Response {
	return &Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers:  http.Header{"Content-Type": {"application/json; charset=utf-8"}, "X-Count": {"3"}},
		Body:     []byte(`{"items":[{"id":42,"name":"first"},{"id":7}],"ok":true,"total":2}`),
		Duration: 120 * time.Millisecond,
	}
}

func TestAssertionsPass(t *testing.T) {
	resp := sampleResponse()
	for _, line := range []string{
		"# @assert status == 200",
		"# @assert status != 404",
		"# @assert status < 300",
		"# @assert status >= 200",
		"# @assert status matches ^2\\d\\d$",
		"# @assert header Content-Type contains json",
		"# @assert header content-type == application/json; charset=utf-8",
		"# @assert header X-Count > 2",
		"# @assert header X-Count exists",
		"# @assert jsonpath $.items[0].id == 42",
		"# @assert jsonpath $.items[1].id != 42",
		"# @assert jsonpath $.items[0].name == first",
		"# @assert jsonpath $.total <= 2",
		"# @assert jsonpath $.ok == true",
		"# @assert jsonpath $.items exists",
		"# @assert jsonpath $ contains \"total\"",
		"# @assert jsonpath items[0].id == 42",
		"# @assert body contains \"ok\":true",
		`# @assert body matches /"total":\s*2/`,
		"# @assert time < 500ms",
		"# @assert time < 500",
		"# @assert time >= 0.1s",
	} {
		res := assertOne(t, line, resp)
		if !res.Pass {
			t.Errorf("%q failed: %s (actual %q)", line, res.Reason, res.Actual)
		}
	}
}

func TestAssertionsFailWithReason(t *testing.T) {
	resp := sampleResponse()
	cases := []struct{ line, reason string }{
		{"# @assert status == 201", "got 200"},
		{"# @assert status > 200", "got 200"},
		{"# @assert header Content-Type contains xml", `got "application/json; charset=utf-8"`},
		{"# @assert header X-Missing exists", "header X-Missing is not present"},
		{"# @assert header X-Missing == 1", "header X-Missing is not present"},
		{"# @assert jsonpath $.items[0].id == 41", "got 42"},
		{"# @assert jsonpath $.nope exists", "$.nope matched no value"},
		{"# @assert jsonpath $.nope == 1", "$.nope matched no value"},
		{"# @assert jsonpath $.items[0].name < 3", "needs a number"},
		{"# @assert body matches /(unclosed/", "invalid regex"},
		{"# @assert time < 50ms", "got 120ms"},
		{"# @assert time < soon", "compares with a duration"},
		{"# @assert status equals 200", `unknown operator "equals"`},
	}
	for _, c := range cases {
		res := assertOne(t, c.line, resp)
		if res.Pass {
			t.Errorf("%q passed, want a failure", c.line)
			continue
		}
		if !strings.Contains(res.Reason, c.reason) {
			t.Errorf("%q: reason %q, want it to mention %q", c.line, res.Reason, c.reason)
		}
		if !strings.Contains(res.Describe(), c.reason) {
			t.Errorf("%q: Describe() = %q must carry the reason", c.line, res.Describe())
		}
	}
}

// A body that is not JSON fails a JSONPath assertion with the reason, and
// an empty body says so instead of pretending the path matched nothing.
func TestAssertJSONPathOverNonJSON(t *testing.T) {
	resp := sampleResponse()
	resp.Body = []byte("<html>")
	if res := assertOne(t, "# @assert jsonpath $.a == 1", resp); res.Pass || !strings.Contains(res.Reason, "not JSON") {
		t.Errorf("non-JSON body: %+v", res)
	}
	resp.Body = nil
	if res := assertOne(t, "# @assert jsonpath $.a exists", resp); res.Pass || !strings.Contains(res.Reason, "empty") {
		t.Errorf("empty body: %+v", res)
	}
	// A body assertion over an empty body still compares.
	if res := assertOne(t, "# @assert body == ''", resp); !res.Pass {
		t.Errorf("empty body equality: %+v", res)
	}
}

func TestJSONPathToJQ(t *testing.T) {
	cases := map[string]string{
		"$.items[0].id": ".items[0].id",
		"$[0]":          ".[0]",
		"$":             ".",
		".a.b":          ".a.b",
		"a.b":           ".a.b",
		"$.a | length":  ".a | length",
	}
	for in, want := range cases {
		if got := jsonPathToJQ(in); got != want {
			t.Errorf("jsonPathToJQ(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssertionSummary(t *testing.T) {
	resp := &Response{}
	if s := resp.AssertionSummary(); s != "" {
		t.Errorf("no directives: %q", s)
	}
	resp.Assertions = []AssertResult{{Pass: true}, {Pass: true}, {Pass: true}}
	if s := resp.AssertionSummary(); s != "3 assertions passed" {
		t.Errorf("all passed: %q", s)
	}
	resp.Assertions[1].Pass = false
	if s := resp.AssertionSummary(); s != "1 of 3 assertions failed" || resp.AssertionsFailed() != 1 {
		t.Errorf("one failed: %q (%d)", s, resp.AssertionsFailed())
	}
	resp.Assertions = resp.Assertions[:1]
	if s := resp.AssertionSummary(); s != "1 assertion passed" {
		t.Errorf("single: %q", s)
	}
}

// A failure message never carries a whole body: the actual value is clipped.
func TestAssertClipsActual(t *testing.T) {
	resp := sampleResponse()
	resp.Body = []byte(strings.Repeat("x", 1000) + "\nsecond line")
	res := assertOne(t, "# @assert body contains nope", resp)
	if res.Pass || len(res.Reason) > 200 || strings.Contains(res.Reason, "\n") {
		t.Errorf("reason = %d bytes %q", len(res.Reason), res.Reason)
	}
}

// TestDispatchAssertions: the directives of a request run over the response,
// and a failure marks the run while the response itself stays untouched.
func TestDispatchAssertions(t *testing.T) {
	srv := jsonServer(t, "application/json", `{"task":"node-1:42","stats":{"total":7}}`)
	req := parseOne(t, strings.Join([]string{
		"# @assert status == 200",
		"# @assert jsonpath $.stats.total == 7",
		"# @assert jsonpath $.task == other",
		"# @capture task = .task",
		"GET " + srv.URL + "/reindex",
	}, "\n"))
	resp, err := Dispatch(context.Background(), req, noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Assertions) != 3 {
		t.Fatalf("assertions: %+v", resp.Assertions)
	}
	if !resp.Assertions[0].Pass || !resp.Assertions[1].Pass || resp.Assertions[2].Pass {
		t.Errorf("outcomes: %+v", resp.Assertions)
	}
	if resp.AssertionsFailed() != 1 || resp.AssertionSummary() != "1 of 3 assertions failed" {
		t.Errorf("summary %q", resp.AssertionSummary())
	}
	if resp.StatusCode != 200 || !strings.Contains(string(resp.Body), "node-1:42") {
		t.Errorf("the exchange itself must be untouched: %d %q", resp.StatusCode, resp.Body)
	}
	if got := resp.CapturedValues(); got["task"] != "node-1:42" {
		t.Errorf("captures still run alongside: %v", got)
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("a failed assertion is not a warning row, got %v", resp.Warnings)
	}
}

// A request without directives yields nil, so consumers can test the field
// for "this request asserts anything".
func TestDispatchWithoutAssertions(t *testing.T) {
	srv := jsonServer(t, "application/json", `{}`)
	resp, err := Dispatch(context.Background(), parseOne(t, "GET "+srv.URL+"/\n"), noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Assertions != nil {
		t.Errorf("assertions = %+v, want nil", resp.Assertions)
	}
}
