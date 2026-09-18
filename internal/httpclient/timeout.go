package httpclient

// timeout.go owns the overall deadline of one exchange (#2630): which of the
// four possible sources it comes from, and how an exchange that ends *by* it
// says so.
//
// Precedence, most specific first — the rule the package doc's "explicit
// values in the .http file always win" spells out for everything else:
//
//  1. the request's own `# @timeout 5s` directive (httpfile, #2630)
//  2. a `.curlrc` `max-time` (the local client configuration)
//  3. Options.Timeout — the `http.timeout_ms` setting the host passes down
//  4. DefaultTimeout, the built-in 30 s
//
// Streams are unaffected: once a response is recognised as a stream (#1776)
// the overall deadline is dropped for StreamIdleTimeout, because a long-lived
// SSE connection is not a hung request.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// effectiveTimeout resolves the overall deadline of one exchange from the
// four sources above. reqTimeout is the request directive's value, 0 when it
// carries none.
func effectiveTimeout(cfg *curlConfig, opts Options, reqTimeout time.Duration) time.Duration {
	timeout := DefaultTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}
	if cfg != nil && cfg.MaxTime > 0 {
		timeout = cfg.MaxTime
	}
	if reqTimeout > 0 {
		timeout = reqTimeout
	}
	return timeout
}

// TimeoutError reports that an exchange ended because its overall deadline
// ran out (#2630), rather than because the connection failed. Limit is the
// deadline that was hit — what the precedence chain resolved to — so the
// response pane can name it and point at the two ways to raise it instead of
// showing the transport's generic "context deadline exceeded".
type TimeoutError struct {
	Key   string
	Limit time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("request %s: timed out after %s", e.Key, FormatTimeout(e.Limit))
}

// Timeout marks the error as a timeout for net.Error-aware callers.
func (e *TimeoutError) Timeout() bool { return true }

// Unwrap keeps errors.Is(err, context.DeadlineExceeded) true, so a caller
// that already tests for the deadline sentinel keeps working.
func (e *TimeoutError) Unwrap() error { return context.DeadlineExceeded }

// FormatTimeout spells a deadline the way the pane and the messages do: whole
// seconds where it is whole seconds ("30 s"), the Go duration otherwise
// ("1.5s", "500ms"), so the number the user typed into the setting is the
// number they read back.
func FormatTimeout(d time.Duration) string {
	if d <= 0 {
		return "0 s"
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%d s", int64(d/time.Second))
	}
	return d.String()
}

// asTimeout turns a transport failure into a TimeoutError when the deadline
// is what ended it, and returns nil otherwise. A user cancel (#1272) is
// deliberately not a timeout: ctx is checked first, so aborting a request two
// seconds into a thirty-second budget still reads as "canceled".
func asTimeout(ctx context.Context, key string, limit time.Duration, err error) error {
	if err == nil || limit <= 0 {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if !isDeadlineErr(err) {
		return nil
	}
	return &TimeoutError{Key: key, Limit: limit}
}

// isDeadlineErr reports whether err is the deadline running out. http.Client
// reports its own Timeout as a *url.Error whose Timeout() is true and whose
// message names "Client.Timeout", while a deadline hit further down surfaces
// as context.DeadlineExceeded or os.ErrDeadlineExceeded — all three mean the
// same thing to the user.
func isDeadlineErr(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
