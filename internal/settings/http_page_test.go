package settings

import "testing"

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
