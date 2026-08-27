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
}
