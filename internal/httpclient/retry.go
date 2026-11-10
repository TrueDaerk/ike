package httpclient

// retry.go runs the attempts of a request carrying a `# @retry` directive
// (#3116). A .http request used to go out exactly once: a flapping service, a
// cold start or a rate limit made the user re-run it by hand, which also lost
// the one-key re-run diff. The directive (parsed in httpfile/retry.go) names
// how often to try again, the fixed pause in between and which outcomes count
// as failed; this file is the loop around one exchange.
//
// Rules:
//   - the deadline (`@timeout`, .curlrc max-time, http.timeout_ms) applies per
//     attempt, so a series may take count × timeout plus the pauses;
//   - a cancel (the dispatch context) ends the series at once, during an
//     attempt or during a pause, with no further attempt;
//   - the returned Response is the final attempt's; the earlier ones are kept
//     as one Attempt each (outcome and elapsed time) for the pane's block;
//   - captures and assertions run once, on the final response — the callers
//     apply them after the loop.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"ike/internal/httpfile"
)

// Attempt is the outcome of one earlier attempt of a retried dispatch
// (#3116): the status that came back, or the error that ended it without a
// response, and how long it took. The final attempt is the Response itself.
type Attempt struct {
	Status     string        `json:"status,omitempty"`
	StatusCode int           `json:"statusCode,omitempty"`
	Err        string        `json:"error,omitempty"`
	TimedOut   bool          `json:"timedOut,omitempty"`
	Duration   time.Duration `json:"duration"`
}

// Outcome is the attempt's result in one word or phrase: the status line, or
// "timed out" / the error text.
func (a Attempt) Outcome() string {
	switch {
	case a.TimedOut:
		return "timed out"
	case a.Err != "":
		return "error: " + a.Err
	case a.Status != "":
		return a.Status
	}
	return fmt.Sprintf("%d", a.StatusCode)
}

// AttemptLabel is the indicator the pane and the history show next to the
// status line — "attempt 3/4" — or "" for a dispatch without a retry
// directive.
func (r *Response) AttemptLabel() string {
	if r == nil || r.MaxAttempts <= 1 {
		return ""
	}
	return fmt.Sprintf("attempt %d/%d", len(r.Attempts)+1, r.MaxAttempts)
}

// RetryError is the failure of a retried dispatch whose final attempt ended
// without a response (#3116). It wraps that attempt's error — a TimeoutError
// stays one for errors.As — and adds where in the series it happened.
type RetryError struct {
	Err      error
	Attempts []Attempt // the earlier attempts
	Max      int
}

func (e *RetryError) Error() string {
	return fmt.Sprintf("%v (%s)", e.Err, e.Label())
}

// Label is the attempt indicator of the failed series, "attempt 4/4".
func (e *RetryError) Label() string {
	return fmt.Sprintf("attempt %d/%d", len(e.Attempts)+1, e.Max)
}

func (e *RetryError) Unwrap() error { return e.Err }

// withRetries runs once up to spec.Attempts() times. once performs one
// exchange; its argument is the status predicate a streaming exchange
// consults before it commits to stream mode — nil on the final attempt,
// since there is nothing left to retry for. onAttempt (may be nil) is told
// when attempt n of max goes out, from the second on, so the host can restart
// its countdown.
func withRetries(ctx context.Context, spec *httpfile.Retry, onAttempt func(n, max int),
	once func(retryOn func(int) bool) (*Response, error)) (*Response, error) {
	max := spec.Attempts()
	if max <= 1 {
		return once(nil)
	}
	var earlier []Attempt
	for n := 1; ; n++ {
		last := n == max
		var retryOn func(int) bool
		if !last {
			retryOn = spec.OnStatus
		}
		if n > 1 && onAttempt != nil {
			onAttempt(n, max)
		}
		started := time.Now()
		resp, err := once(retryOn)
		if ctx.Err() != nil {
			// A cancel ends the series where it stands: whatever this attempt
			// produced (a partial stream, the cancel error) is the answer.
			return finishSeries(resp, err, earlier, max)
		}
		if last || !retryable(spec, resp, err) {
			return finishSeries(resp, err, earlier, max)
		}
		earlier = append(earlier, attemptOf(resp, err, time.Since(started)))
		if resp != nil {
			resp.discard()
		}
		if spec.Delay > 0 {
			t := time.NewTimer(spec.Delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			case <-t.C:
			}
		}
	}
}

// retryable reports whether one attempt's outcome is a failure the spec
// retries on.
func retryable(spec *httpfile.Retry, resp *Response, err error) bool {
	if err == nil {
		return resp != nil && spec.OnStatus(resp.StatusCode)
	}
	var te *TimeoutError
	if errors.As(err, &te) {
		return spec.OnTimeout()
	}
	return spec.OnError()
}

// attemptOf records a finished, retried attempt. elapsed is the fallback for
// an error, which carries no Duration of its own.
func attemptOf(resp *Response, err error, elapsed time.Duration) Attempt {
	if err != nil {
		var te *TimeoutError
		if errors.As(err, &te) {
			return Attempt{TimedOut: true, Duration: elapsed}
		}
		return Attempt{Err: ErrorText(err), Duration: elapsed}
	}
	return Attempt{Status: resp.Status, StatusCode: resp.StatusCode, Duration: resp.Duration}
}

// finishSeries stamps the final outcome with the series it ended.
func finishSeries(resp *Response, err error, earlier []Attempt, max int) (*Response, error) {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return resp, err
		}
		return resp, &RetryError{Err: err, Attempts: earlier, Max: max}
	}
	if resp != nil {
		resp.Attempts, resp.MaxAttempts = earlier, max
	}
	return resp, nil
}

// discard releases what a retried attempt's response holds on disk: a
// spooled body (#2157) of an answer nobody will see.
func (r *Response) discard() {
	if r.SpoolPath != "" {
		_ = os.Remove(r.SpoolPath)
	}
}

// ErrorText is a transport error in its shortest readable form: the
// innermost cause, without the "request x: Get …" wrapping — what an attempt
// row has room for.
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err.Error()
		}
		err = next
	}
}
