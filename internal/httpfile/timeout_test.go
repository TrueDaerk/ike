package httpfile

import (
	"strings"
	"testing"
	"time"
)

// timeout_test.go covers the `# @timeout 5s` directive (#2630).

func TestTimeoutDirectiveSpellings(t *testing.T) {
	cases := []struct {
		line string
		want time.Duration
	}{
		{"# @timeout 5s", 5 * time.Second},
		{"## @timeout 1500ms", 1500 * time.Millisecond},
		{"// @timeout 2m", 2 * time.Minute},
		{"   #   @timeout   250ms   ", 250 * time.Millisecond},
		// curl's max-time spelling: bare, possibly fractional seconds.
		{"# @timeout 45", 45 * time.Second},
		{"# @timeout 0.5", 500 * time.Millisecond},
	}
	for _, c := range cases {
		d, _, err, ok := TimeoutDirective(c.line)
		if !ok || err != nil {
			t.Errorf("%q: ok=%v err=%v", c.line, ok, err)
			continue
		}
		if d != c.want {
			t.Errorf("%q = %v, want %v", c.line, d, c.want)
		}
	}
}

// `###` opens a request block and names it, so a directive spelled that way
// would be swallowed by the separator — captureRE's rule, applied here too.
func TestTimeoutDirectiveRejectsNonDirectives(t *testing.T) {
	for _, line := range []string{
		"### @timeout 5s",
		"GET http://example.test/@timeout 5s",
		"# timeout 5s",
		"# @timeouts 5s",
		"# @timeout 5s extra",
	} {
		if _, _, _, ok := TimeoutDirective(line); ok {
			t.Errorf("%q must not read as a timeout directive", line)
		}
	}
}

func TestTimeoutDirectiveRejectsBadValues(t *testing.T) {
	for _, line := range []string{"# @timeout", "# @timeout soon", "# @timeout 0", "# @timeout -3s"} {
		_, _, err, ok := TimeoutDirective(line)
		if !ok {
			t.Errorf("%q is a directive, however broken", line)
			continue
		}
		if err == nil {
			t.Errorf("%q must be rejected", line)
		}
	}
}

func TestParseAttachesTimeoutToItsRequest(t *testing.T) {
	f := Parse(strings.Join([]string{
		"### fast",
		"# @timeout 2s",
		"GET http://example.test/a",
		"",
		"### default",
		"GET http://example.test/b",
	}, "\n"))
	if len(f.Errors) != 0 {
		t.Fatalf("errors: %v", f.Errors)
	}
	if len(f.Requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(f.Requests))
	}
	if got := f.Requests[0].Timeout; got != 2*time.Second {
		t.Errorf("first request timeout = %v, want 2s", got)
	}
	if got := f.Requests[1].Timeout; got != 0 {
		t.Errorf("second request timeout = %v, want 0 (the block carries none)", got)
	}
}

// A second directive in the same block is an edit, not a second deadline.
func TestParseTimeoutLastWins(t *testing.T) {
	f := Parse("# @timeout 2s\n# @timeout 9s\nGET http://example.test/a\n")
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d", len(f.Requests))
	}
	if got := f.Requests[0].Timeout; got != 9*time.Second {
		t.Errorf("timeout = %v, want 9s", got)
	}
}

// A value that does not parse is reported on its own line rather than
// silently leaving the configured default in place.
func TestParseTimeoutReportsBadValue(t *testing.T) {
	f := Parse("GET http://example.test/a\n\n###\n# @timeout soon\nGET http://example.test/b\n")
	if len(f.Errors) != 1 {
		t.Fatalf("errors = %v, want one", f.Errors)
	}
	if f.Errors[0].Line != 4 {
		t.Errorf("error line = %d, want 4", f.Errors[0].Line)
	}
	if !strings.Contains(f.Errors[0].Msg, "@timeout") {
		t.Errorf("error %q must name the directive", f.Errors[0].Msg)
	}
	// The well-formed block still parses — the tolerant-per-block rule.
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d, want the intact one", len(f.Requests))
	}
}
