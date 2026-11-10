package httppane

import (
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// attempts_test.go covers the `# @retry` rendering (#3116): the attempt
// indicator on the status line, the folded block of earlier attempts, and
// the attempt in the in-flight header.

func retried(n int) *httpclient.Response {
	resp := sample()
	resp.MaxAttempts = 4
	for i := 0; i < n; i++ {
		resp.Attempts = append(resp.Attempts, httpclient.Attempt{
			Status: "503 Service Unavailable", StatusCode: 503, Duration: 40 * time.Millisecond})
	}
	return resp
}

func TestAttemptBlockUnderStatusLine(t *testing.T) {
	resp := retried(1)
	resp.Attempts = append(resp.Attempts, httpclient.Attempt{Err: "connection refused", Duration: 3 * time.Millisecond})
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", resp)

	if !strings.Contains(m.rows[0].text, "· attempt 3/4") {
		t.Fatalf("status row = %q, want the attempt indicator", m.rows[0].text)
	}
	start, end, ok := m.AttemptRows()
	if !ok || start != 1 || end != 3 {
		t.Fatalf("attempt rows %d..%d ok=%v, want 1..3", start, end, ok)
	}
	text := m.AttemptsText()
	for _, want := range []string{
		"2 earlier attempts",
		"↻ #1  503 Service Unavailable  (40ms)",
		"↻ #2  error: connection refused  (3ms)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("block misses %q:\n%s", want, text)
		}
	}
	if view := m.View(); !strings.Contains(view, "attempt 3/4") || !strings.Contains(view, "2 earlier attempts") {
		t.Errorf("view misses the indicator or the block:\n%s", view)
	}
}

func TestAttemptBlockFoldsLikeTheChain(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", retried(2))
	start, end, _ := m.AttemptRows()
	if _, folded := m.folded[start]; folded {
		t.Fatal("a short block starts expanded")
	}
	m.handleKey(keyPress("enter"))
	if got, folded := m.folded[start]; !folded || got != end {
		t.Fatalf("enter did not fold the block: %v", m.folded)
	}
	// The body's highlight pass keeps the block's fold.
	m.setFolds(nil, len(m.rows))
	if _, folded := m.folded[start]; !folded {
		t.Error("a syntax pass dropped the collapsed attempt block")
	}

	// A long series composes collapsed.
	resp := retried(4)
	resp.MaxAttempts = 5
	m.Set("create", resp)
	start, _, _ = m.AttemptRows()
	if _, folded := m.folded[start]; !folded {
		t.Error("a long series must start collapsed")
	}
}

func TestNoAttemptBlockWithoutRetry(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", sample())
	if _, _, ok := m.AttemptRows(); ok || strings.Contains(m.rows[0].text, "attempt") {
		t.Fatalf("plain response grew an attempt block: %q", m.rows[0].text)
	}
	// A retried request that succeeded at once has the indicator, no block.
	m.Set("create", retried(0))
	if _, _, ok := m.AttemptRows(); ok || !strings.Contains(m.rows[0].text, "attempt 1/4") {
		t.Fatalf("first-try success: status %q", m.rows[0].text)
	}
}

func TestPendingHeaderShowsAttempt(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", sample())
	m.SetPending("create", time.Now().Add(-2*time.Second))
	m.SetPendingLimit(30 * time.Second)
	m.SetPendingAttempt(2, 4)
	if view := m.View(); !strings.Contains(view, "⟳ running create (attempt 2/4 · waiting 2.0s / 30 s)") {
		t.Fatalf("header misses the attempt:\n%s", view)
	}
	m.ClearPending()
	if m.PendingAttempt() != "" {
		t.Error("the attempt must die with the flight")
	}
}
