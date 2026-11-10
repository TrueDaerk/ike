package app

// http_retry.go is the host side of the `# @retry` directive (#3116): the
// dispatcher runs the attempts (httpclient/retry.go); the app restarts the
// in-flight countdown per attempt — the deadline applies per attempt, so a
// countdown over the whole series would read as overdue from the second
// attempt on — and names the attempt in the pane's header and in a failure.

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
)

// HTTPRetryAttemptMsg announces that attempt N of Max of a retried dispatch
// went out (#3116); events is the dispatch's event channel the loop keeps
// reading from, like the stream messages carry it.
type HTTPRetryAttemptMsg struct {
	Source  string
	Request string
	N, Max  int
	events  chan tea.Msg
}

// beginHTTPAttempt restarts the flight's countdown for the attempt that just
// went out and mirrors it into the pane. A flight that is already gone
// (canceled meanwhile) has nothing to restart.
func (m *Model) beginHTTPAttempt(msg HTTPRetryAttemptMsg) {
	e, ok := m.httpFlight[httpFlightKey(msg.Source, msg.Request)]
	if !ok {
		return
	}
	e.attemptSince = time.Now()
	e.attempt, e.maxAttempts = msg.N, msg.Max
	m.markHTTPPending()
}

// waitingSince is when the flight's current attempt went out: the countdown
// in the pane's header counts from here, while the statusline keeps the
// series' total elapsed time.
func (e *httpFlightEntry) waitingSince() time.Time {
	if !e.attemptSince.IsZero() {
		return e.attemptSince
	}
	return e.started
}

// httpAttemptSuffix is the " (attempt 4/4)" a failed series' message ends
// with, "" for a dispatch without the directive.
func httpAttemptSuffix(err error) string {
	var re *httpclient.RetryError
	if errors.As(err, &re) {
		return " (" + re.Label() + ")"
	}
	return ""
}
