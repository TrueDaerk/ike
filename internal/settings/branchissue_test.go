package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// branchissue_test.go covers the branch-issue segment's settings (#2544): the
// Forge page carries the on/off switch and the branch pattern, and the
// pattern is checked in the form — a regexp that does not compile, or one
// that captures nothing, is refused where it is typed instead of silently
// leaving the segment blank.

// branchIssueEntry finds one of the two entries on the real Forge page.
func branchIssueEntry(t *testing.T, key string) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == key {
				if p.Title != "Forge" {
					t.Fatalf("%s lives on the %q page, want Forge", key, p.Title)
				}
				return e
			}
		}
	}
	t.Fatalf("the settings schema has no entry for %s", key)
	return Entry{}
}

func TestBranchIssueSegmentSetting(t *testing.T) {
	e := branchIssueEntry(t, "statusline.branch_issue")
	if e.Type != Enum {
		t.Errorf("type = %v, want Enum", e.Type)
	}
	if strings.Join(e.Options, ",") != "off,on" {
		t.Errorf("options = %v, want off,on", e.Options)
	}
	if !strings.Contains(e.Description, "Issues") {
		t.Errorf("the description must say where a click lands: %q", e.Description)
	}
}

func TestBranchIssuePatternSetting(t *testing.T) {
	e := branchIssueEntry(t, "statusline.branch_issue_pattern")
	if e.Type != String {
		t.Errorf("type = %v, want String", e.Type)
	}
	if e.ValidateString == nil {
		t.Fatal("the pattern entry needs the form check")
	}
	if msg := e.ValidateString(config.DefaultBranchIssuePattern); msg != "" {
		t.Errorf("the default pattern was rejected with %q", msg)
	}
	if msg := e.ValidateString(`^feature/ISSUE-(\d+)`); msg != "" {
		t.Errorf("a valid custom pattern was rejected with %q", msg)
	}
	for _, bad := range []string{"", `^issue/(\d+`, `^issue/\d+`} {
		if msg := e.ValidateString(bad); msg == "" {
			t.Errorf("%q must be rejected in the form", bad)
		}
	}
}
