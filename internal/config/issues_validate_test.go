package config

import "testing"

// issues.default_filter is dropped whole when it does not parse (#2115): a
// pane seeded from half an expression would show a listing nobody asked for.
func TestValidateIssuesDefaultFilter(t *testing.T) {
	c := defaults()
	c.Issues.DefaultFilter = "is:open label:bug crash"
	if diags := validate(c); c.Issues.DefaultFilter == "" || len(diagsFor(diags, "issues.default_filter")) != 0 {
		t.Fatalf("a valid expression must survive: %v", diags)
	}

	c = defaults()
	c.Issues.DefaultFilter = "is:merged"
	diags := validate(c)
	if c.Issues.DefaultFilter != "" {
		t.Fatalf("a broken expression must be dropped, got %q", c.Issues.DefaultFilter)
	}
	if len(diagsFor(diags, "issues.default_filter")) != 1 {
		t.Fatalf("want one diagnostic for the broken filter, got %v", diags)
	}
}

// A saved filter that does not parse — or repeats a name — is dropped on its
// own; the rest of the list keeps working.
func TestValidateIssuesSavedFilters(t *testing.T) {
	c := defaults()
	c.Issues.SavedFilters = []string{
		"triage=is:open label:bug",
		"broken",          // no "="
		"stale=author:me", // unknown qualifier
		"triage=is:all",   // duplicate name
		"mine=is:all me",
	}
	diags := validate(c)
	want := []string{"triage=is:open label:bug", "mine=is:all me"}
	if len(c.Issues.SavedFilters) != len(want) {
		t.Fatalf("saved filters = %v, want %v", c.Issues.SavedFilters, want)
	}
	for i, w := range want {
		if c.Issues.SavedFilters[i] != w {
			t.Fatalf("saved filters = %v, want %v", c.Issues.SavedFilters, want)
		}
	}
	if got := len(diagsFor(diags, "issues.saved_filters")); got != 3 {
		t.Fatalf("want a diagnostic per dropped entry (3), got %d: %v", got, diags)
	}
}

// The defaults open the pane unfiltered and offer no saved filter.
func TestIssuesFilterDefaultsAreEmpty(t *testing.T) {
	c := defaults()
	if diags := validate(c); len(diagsFor(diags, "issues.default_filter")) != 0 ||
		len(diagsFor(diags, "issues.saved_filters")) != 0 {
		t.Fatalf("the defaults must validate cleanly: %v", diags)
	}
	if c.Issues.DefaultFilter != "" || len(c.Issues.SavedFilters) != 0 {
		t.Fatalf("defaults = %+v, want an empty filter and no saved filters", c.Issues)
	}
}
