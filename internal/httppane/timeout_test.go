package httppane

import (
	"strings"
	"testing"
	"time"
)

// timeout_test.go covers the visible wait (#2630): the in-flight header
// counts the elapsed time against the deadline the dispatch runs under, and a
// dispatch that ends by that deadline says so where the answer would be.

// TestPendingHeaderShowsElapsedAgainstLimit: while a request is out, the
// header names both numbers plus the chord that aborts it, so the wait has a
// visible end and a way out.
func TestPendingHeaderShowsElapsedAgainstLimit(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("create", sample())
	m.SetCancelChord("ctrl+.")
	m.SetPending("create", time.Now().Add(-12*time.Second))
	m.SetPendingLimit(30 * time.Second)

	view := m.View()
	if !strings.Contains(view, "⟳ running create (waiting 12.0s / 30 s)") {
		t.Fatalf("header must count the wait against the limit:\n%s", view)
	}
	if !strings.Contains(view, "x / ctrl+. cancels") {
		t.Fatalf("header must name the cancel chord:\n%s", view)
	}
}

// Without a known limit the header keeps its older elapsed-only wording
// rather than inventing a deadline.
func TestPendingHeaderWithoutLimit(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.SetPending("create", time.Now().Add(-2*time.Second))
	if got := m.waitedFor(); !strings.HasPrefix(got, "2.0s") {
		t.Fatalf("wait text = %q, want the elapsed time alone", got)
	}
}

// The limit dies with the flight: a finished dispatch must not leave a
// countdown standing.
func TestClearPendingDropsLimit(t *testing.T) {
	m := New(nil)
	m.SetPending("create", time.Now())
	m.SetPendingLimit(5 * time.Second)
	m.ClearPending()
	if got := m.PendingLimit(); got != 0 {
		t.Errorf("limit = %v, want 0 once the flight ended", got)
	}
}

// TestSetFailureReplacesTheResponse: a timed-out dispatch says so in the
// header and in the rows, and the previous response stops being presented as
// the current answer.
func TestSetFailureReplacesTheResponse(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", sample())
	m.SetPending("create", time.Now())
	m.SetPendingLimit(time.Second)

	const text = "timed out after 1 s — raise http.timeout_ms or add a `# @timeout 2s` directive to the request"
	m.SetFailure("create", text)

	if got := m.Failure(); got != text {
		t.Errorf("Failure() = %q", got)
	}
	if m.Pending() != "" || m.PendingLimit() != 0 {
		t.Error("the failure must end the flight display")
	}
	if m.CurrentResponse() != nil {
		t.Error("the previous response must not stay on show as the current answer")
	}
	view := m.View()
	if !strings.Contains(view, "timed out after 1 s") {
		t.Fatalf("view must say it timed out:\n%s", view)
	}
	if !strings.Contains(view, "raise http.timeout_ms") {
		t.Fatalf("view must name the setting:\n%s", view)
	}
}

// A new response clears the failure — the pane shows the answer, not the
// reason the run before it had none.
func TestResponseClearsFailure(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	m.SetFailure("create", "timed out after 1 s")
	m.Set("create", sample())
	if got := m.Failure(); got != "" {
		t.Errorf("failure survived a response: %q", got)
	}
}
