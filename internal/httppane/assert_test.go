package httppane

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
	"ike/internal/httpfile"
)

// assert_test.go covers the pass/fail block of the assertion directive
// (#2546): the summary on the status row, the block above the body, and the
// failure colour.

func assertSample(results ...httpclient.AssertResult) *httpclient.Response {
	return &httpclient.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"id":41}`),
		Duration:   12 * time.Millisecond,
		RequestKey: "thing",
		Assertions: results,
	}
}

func passed(subject, op, expected string) httpclient.AssertResult {
	return httpclient.AssertResult{Assertion: httpfile.Assertion{Subject: subject, Op: op, Expected: expected}, Pass: true}
}

func TestAssertionsRenderAboveTheBody(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("thing", assertSample(
		passed("status", "==", "200"),
		httpclient.AssertResult{
			Assertion: httpfile.Assertion{Subject: "jsonpath", Arg: "$.id", Op: "==", Expected: "42"},
			Actual:    "41", Reason: "got 41",
		},
	))
	if n := m.AssertionsFailed(); n != 1 {
		t.Fatalf("AssertionsFailed = %d, want 1", n)
	}
	statusRow := m.RowText(0)
	if !strings.Contains(statusRow, "200 OK") || !strings.Contains(statusRow, "1 of 2 assertions failed") {
		t.Errorf("status row = %q, want the summary on it", statusRow)
	}
	passRow, failRow, bodyRow := -1, -1, -1
	for i := 0; i < m.Rows(); i++ {
		text := m.RowText(i)
		if passRow < 0 && strings.Contains(text, "✓ status == 200") {
			passRow = i
		}
		if failRow < 0 && strings.Contains(text, "✗ jsonpath $.id == 42 — got 41") {
			failRow = i
		}
		if bodyRow < 0 && strings.Contains(text, `"id"`) {
			bodyRow = i
		}
	}
	if passRow < 0 || failRow < 0 {
		t.Fatalf("the assertion rows never made it: pass %d fail %d", passRow, failRow)
	}
	if bodyRow < 0 || failRow > bodyRow || passRow > bodyRow {
		t.Errorf("assertion rows (%d, %d) must sit above the body (%d)", passRow, failRow, bodyRow)
	}
	// The status row takes the failure colour and the rows their outcomes'.
	pal := m.theme()
	st, tag := m.baseStyle(pal, 0, 0, 10)(0)
	if tag != "status" || st.GetForeground() != pal.Error {
		t.Errorf("status row colour = %v (%s), want the error colour", st.GetForeground(), tag)
	}
	if _, tag := m.baseStyle(pal, passRow, 0, 10)(0); tag != "pass" {
		t.Errorf("passed row tag = %q, want pass", tag)
	}
	if _, tag := m.baseStyle(pal, failRow, 0, 10)(0); tag != "error" {
		t.Errorf("failed row tag = %q, want error", tag)
	}
}

// All green: the summary says so, the status row keeps its accent colour.
func TestAssertionsAllPassed(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("thing", assertSample(passed("status", "==", "200"), passed("time", "<", "500ms")))
	if !strings.Contains(m.RowText(0), "✓ 2 assertions passed") {
		t.Errorf("status row = %q", m.RowText(0))
	}
	pal := m.theme()
	if st, _ := m.baseStyle(pal, 0, 0, 10)(0); st.GetForeground() != pal.Accent {
		t.Errorf("a passing run keeps the accent colour, got %v", st.GetForeground())
	}
	found := false
	for i := 0; i < m.Rows(); i++ {
		if strings.Contains(m.RowText(i), "✓ assertions: 2 assertions passed") {
			found = true
		}
	}
	if !found {
		t.Error("the block heading is missing")
	}
}

// A response without directives shows nothing of it, and a later plain
// response drops the previous block.
func TestAssertionsAbsentWithoutDirectives(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("thing", assertSample(passed("status", "==", "200")))
	m.Set("thing", assertSample())
	if m.Assertions() != nil || m.AssertionsFailed() != 0 {
		t.Errorf("stale assertions survived: %+v", m.Assertions())
	}
	for i := 0; i < m.Rows(); i++ {
		if strings.Contains(m.RowText(i), "assertion") {
			t.Errorf("row %d = %q mentions assertions", i, m.RowText(i))
		}
	}
}
