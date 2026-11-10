package httpfile

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// retry_test.go covers the `# @retry` directive (#3116).

func TestRetryDirectiveForms(t *testing.T) {
	cases := []struct {
		line  string
		count int
		delay time.Duration
		conds []string
	}{
		{"# @retry 3", 3, 0, DefaultRetryConditions},
		{"# @retry 3 2s", 3, 2 * time.Second, DefaultRetryConditions},
		{"# @retry 3 on 503,timeout", 3, 0, []string{"503", "timeout"}},
		{"# @retry 3 2s on 5xx,error", 3, 2 * time.Second, []string{"5xx", "error"}},
		{"// @retry 1 500ms", 1, 500 * time.Millisecond, DefaultRetryConditions},
		{"## @retry 2 1.5", 2, 1500 * time.Millisecond, DefaultRetryConditions},
		// Case-insensitive, blanks after the commas tolerated.
		{"# @retry 2 ON 5XX, Timeout", 2, 0, []string{"5xx", "timeout"}},
	}
	for _, c := range cases {
		r, ok := RetryDirective(c.line)
		if !ok || r.Err != nil {
			t.Errorf("%q: ok=%v err=%v", c.line, ok, r.Err)
			continue
		}
		if r.Count != c.count || r.Delay != c.delay || !reflect.DeepEqual(r.Conditions, c.conds) {
			t.Errorf("%q = %d %v %v, want %d %v %v", c.line, r.Count, r.Delay, r.Conditions, c.count, c.delay, c.conds)
		}
		if r.Attempts() != c.count+1 {
			t.Errorf("%q: Attempts() = %d, want %d", c.line, r.Attempts(), c.count+1)
		}
	}
}

func TestRetryDirectiveErrors(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{"# @retry", "needs a count"},
		{"# @retry 0", "positive"},
		{"# @retry -2", "positive"},
		{"# @retry many", "positive"},
		{"# @retry 3 soon", "delay"},
		{"# @retry 3 -1s", "negative"},
		{"# @retry 3 on 503,sometimes", `"sometimes"`},
		{"# @retry 3 on 6xx", `"6xx"`},
		{"# @retry 3 on", "at least one"},
		{"# @retry 3 2s 503", "follow `on`"},
	} {
		r, ok := RetryDirective(c.line)
		if !ok {
			t.Errorf("%q is a directive, however broken", c.line)
			continue
		}
		if r.Err == nil || !strings.Contains(r.Err.Error(), c.want) {
			t.Errorf("%q: err = %v, want it to mention %q", c.line, r.Err, c.want)
		}
	}
}

func TestRetryDirectiveRejectsNonDirectives(t *testing.T) {
	for _, line := range []string{"### @retry 3", "# retry 3", "# @retrying 3", "GET http://x.test/@retry"} {
		if _, ok := RetryDirective(line); ok {
			t.Errorf("%q must not read as a retry directive", line)
		}
	}
}

func TestRetryConditionsMatch(t *testing.T) {
	r, _ := RetryDirective("# @retry 2 on 404,5xx,timeout")
	for code, want := range map[int]bool{404: true, 500: true, 503: true, 599: true, 400: false, 200: false} {
		if got := r.OnStatus(code); got != want {
			t.Errorf("OnStatus(%d) = %v, want %v", code, got, want)
		}
	}
	if !r.OnTimeout() || r.OnError() {
		t.Errorf("timeout/error = %v/%v, want true/false", r.OnTimeout(), r.OnError())
	}
	var none *Retry
	if none.OnStatus(503) || none.OnError() || none.Attempts() != 1 {
		t.Error("a nil spec retries nothing and allows one attempt")
	}
}

func TestParseAttachesRetry(t *testing.T) {
	f := Parse("# @retry 1\n# @retry 3 2s on 503\nGET http://example.test/\n# @timeout 5s\n")
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("errors %v, requests %d", f.Errors, len(f.Requests))
	}
	r := f.Requests[0].Retry
	if r == nil || r.Count != 3 || r.Delay != 2*time.Second || !reflect.DeepEqual(r.Conditions, []string{"503"}) {
		t.Fatalf("Retry = %+v, want the last directive (3, 2s, 503)", r)
	}
	if r.Line != 2 {
		t.Errorf("Line = %d, want 2", r.Line)
	}
	if f.Requests[0].Timeout != 5*time.Second {
		t.Errorf("the timeout next to it still applies, got %v", f.Requests[0].Timeout)
	}
	if got := Parse("GET http://example.test/\n").Requests[0].Retry; got != nil {
		t.Errorf("a block without the directive has Retry %+v, want nil", got)
	}
}

// A broken directive drops the block, like a bad @timeout: a request meant
// to be retried must not quietly go out once.
func TestParseBadRetryDropsBlock(t *testing.T) {
	f := Parse("### a\n# @retry 3 on sometimes\nGET http://example.test/a\n\n### b\nGET http://example.test/b\n")
	if len(f.Requests) != 1 || f.Requests[0].Name != "b" {
		t.Fatalf("requests = %d, want only b", len(f.Requests))
	}
	if len(f.Errors) != 1 || f.Errors[0].Line != 2 {
		t.Fatalf("errors = %v, want one on line 2", f.Errors)
	}
}

// `### @retry 3` is a separator naming the block, not a directive.
func TestParseTripleHashRetryIsAName(t *testing.T) {
	f := Parse("### @retry 3\nGET http://example.test/\n")
	if len(f.Requests) != 1 || f.Requests[0].Retry != nil || f.Requests[0].Name != "@retry 3" {
		t.Fatalf("got %+v", f.Requests)
	}
}
