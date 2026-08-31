package settings

import (
	"strings"
	"testing"
)

// The HTTP Client page exposes both re-run/compare settings (#2247) — a
// config-file-only setting is not a setting.
func TestHTTPClientPageEntries(t *testing.T) {
	var entries []Entry
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		if p.Title == "HTTP Client" {
			entries = p.Entries
		}
	}
	if len(entries) == 0 {
		t.Fatal("no HTTP Client page")
	}
	byKey := map[string]Entry{}
	for _, e := range entries {
		byKey[e.Key] = e
	}
	if e, ok := byKey["http.diff_after_rerun"]; !ok || e.Type != Bool {
		t.Errorf("http.diff_after_rerun entry = %+v", e)
	}
	e, ok := byKey["http.diff_ignore_headers"]
	if !ok || e.Type != List {
		t.Fatalf("http.diff_ignore_headers entry = %+v", e)
	}
	if e.ValidateEntry == nil {
		t.Fatal("the header list must validate its elements")
	}
	// The highlight cap (#2353) is an Int with the same bounds the config
	// validation enforces, so the form rejects what the loader would reset.
	h, ok := byKey["http.highlight_limit_kb"]
	if !ok || h.Type != Int {
		t.Fatalf("http.highlight_limit_kb entry = %+v", h)
	}
	if h.Min != 1 || h.Max != 65536 {
		t.Errorf("http.highlight_limit_kb bounds = %d–%d, want 1–65536", h.Min, h.Max)
	}
	// The completion-notice threshold (#2364) is editable here too, with 0
	// reachable as the off value the config validation accepts.
	n, ok := byKey["http.notify_slow_ms"]
	if !ok || n.Type != Int {
		t.Fatalf("http.notify_slow_ms entry = %+v", n)
	}
	if n.Min != 0 || n.Max != 600000 {
		t.Errorf("http.notify_slow_ms bounds = %d–%d, want 0–600000", n.Min, n.Max)
	}
	if !strings.Contains(n.Description, "0 turns") {
		t.Errorf("the description must document the off value: %q", n.Description)
	}
}

// The header-list element check (#2247) accepts header names and wildcards,
// and refuses what would silently never match — or would match everything.
func TestHTTPIgnoreHeaderValidate(t *testing.T) {
	ok := []string{"Date", "x-request-id", "X-Amz-*", " etag "}
	for _, v := range ok {
		if msg := httpIgnoreHeaderValidate(nil, v); msg != "" {
			t.Errorf("%q rejected: %s", v, msg)
		}
	}
	bad := []string{"", "   ", "*", "x request id", "x:y"}
	for _, v := range bad {
		if msg := httpIgnoreHeaderValidate(nil, v); msg == "" {
			t.Errorf("%q must be rejected", v)
		}
	}
}
