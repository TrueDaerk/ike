package config

import (
	"slices"
	"strings"
	"testing"
)

// The response-diff header filter (#2247) normalizes to lower case, drops
// duplicates and refuses blank entries with a diagnostic.
func TestValidateHTTPDiffIgnoreHeaders(t *testing.T) {
	c := defaults()
	c.HTTP.DiffIgnoreHeaders = []string{"Date", " X-Request-Id ", "date", "  ", "X-Amz-*"}
	diags := validate(c)
	want := []string{"date", "x-request-id", "x-amz-*"}
	if !slices.Equal(c.HTTP.DiffIgnoreHeaders, want) {
		t.Fatalf("normalized = %v, want %v", c.HTTP.DiffIgnoreHeaders, want)
	}
	if len(diagsFor(diags, "http.diff_ignore_headers")) != 1 {
		t.Fatalf("the blank entry must be reported once, got %v", diags)
	}
	// An empty list stays empty (every header compared) without complaint.
	c = defaults()
	c.HTTP.DiffIgnoreHeaders = nil
	if diags := validate(c); len(diagsFor(diags, "http.diff_ignore_headers")) != 0 {
		t.Fatalf("an empty filter is valid: %v", diags)
	}
}

// The defaults close the re-run loop by themselves (#2247): the noise filter
// is populated and the auto-diff is on.
func TestHTTPDefaults(t *testing.T) {
	c := defaults()
	if !c.HTTP.DiffAfterRerun {
		t.Error("http.diff_after_rerun must default to on")
	}
	if !slices.Contains(c.HTTP.DiffIgnoreHeaders, "date") {
		t.Errorf("the default noise filter must cover Date: %v", c.HTTP.DiffIgnoreHeaders)
	}
	for _, h := range c.HTTP.DiffIgnoreHeaders {
		if h != strings.ToLower(h) {
			t.Errorf("default header %q must be lower case", h)
		}
	}
	// Both keys are readable as dotted strings, which is what the settings
	// panel and the config viewer render.
	m := c.Flat()
	if m["http.diff_after_rerun"] != "true" {
		t.Errorf("http.diff_after_rerun = %q", m["http.diff_after_rerun"])
	}
	if !strings.Contains(m["http.diff_ignore_headers"], "date") {
		t.Errorf("http.diff_ignore_headers = %q", m["http.diff_ignore_headers"])
	}
	if m["http.highlight_limit_kb"] != "2048" {
		t.Errorf("http.highlight_limit_kb = %q", m["http.highlight_limit_kb"])
	}
}

// The response viewer's highlight cap (#2353) defaults to 2 MiB and rejects
// values that would silently disable highlighting or defeat the cap.
func TestValidateHTTPHighlightLimit(t *testing.T) {
	for _, bad := range []int{0, -5, 70000} {
		c := defaults()
		c.HTTP.HighlightLimitKB = bad
		diags := validate(c)
		if c.HTTP.HighlightLimitKB != 2048 {
			t.Errorf("limit %d validated to %d, want the 2048 fallback", bad, c.HTTP.HighlightLimitKB)
		}
		if len(diagsFor(diags, "http.highlight_limit_kb")) != 1 {
			t.Errorf("limit %d must be reported once, got %v", bad, diags)
		}
	}
	c := defaults()
	c.HTTP.HighlightLimitKB = 512
	if diags := validate(c); len(diagsFor(diags, "http.highlight_limit_kb")) != 0 || c.HTTP.HighlightLimitKB != 512 {
		t.Errorf("512 KiB is a valid limit: %d, %v", c.HTTP.HighlightLimitKB, diags)
	}
}

// The completion-notice threshold (#2364) defaults to 3 s, keeps 0 as its off
// switch and refuses values outside the accepted window.
func TestValidateHTTPNotifySlowMs(t *testing.T) {
	if c := defaults(); c.HTTP.NotifySlowMs != 3000 {
		t.Errorf("default threshold = %d, want 3000", c.HTTP.NotifySlowMs)
	}
	for _, bad := range []int{-1, -3000, 600001} {
		c := defaults()
		c.HTTP.NotifySlowMs = bad
		diags := validate(c)
		if c.HTTP.NotifySlowMs != 3000 {
			t.Errorf("threshold %d validated to %d, want the 3000 fallback", bad, c.HTTP.NotifySlowMs)
		}
		if len(diagsFor(diags, "http.notify_slow_ms")) != 1 {
			t.Errorf("threshold %d must be reported once, got %v", bad, diags)
		}
	}
	// 0 is the documented off switch and survives validation untouched.
	for _, good := range []int{0, 1, 8000, 600000} {
		c := defaults()
		c.HTTP.NotifySlowMs = good
		if diags := validate(c); len(diagsFor(diags, "http.notify_slow_ms")) != 0 || c.HTTP.NotifySlowMs != good {
			t.Errorf("threshold %d is valid: %d, %v", good, c.HTTP.NotifySlowMs, diags)
		}
	}
	// The key reads as a dotted string, which is what the settings panel and
	// the config viewer render.
	if got := defaults().Flat()["http.notify_slow_ms"]; got != "3000" {
		t.Errorf("http.notify_slow_ms = %q", got)
	}
}
