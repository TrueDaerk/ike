package config

import "testing"

// The branch-issue segment (#2544) ships off, with IKE's own branch naming as
// the pattern — and the defaults validate cleanly.
func TestBranchIssueDefaults(t *testing.T) {
	c := defaults()
	if diags := validate(c); len(diagsFor(diags, "statusline.branch_issue")) != 0 ||
		len(diagsFor(diags, "statusline.branch_issue_pattern")) != 0 {
		t.Fatalf("the defaults must validate cleanly: %v", diags)
	}
	if c.StatusLine.BranchIssue != "off" || c.StatusLine.BranchIssuePattern != DefaultBranchIssuePattern {
		t.Fatalf("defaults = %+v, want off and %q", c.StatusLine, DefaultBranchIssuePattern)
	}
}

// A pattern that cannot resolve a number falls back to the default rather than
// leaving the segment permanently blank with no explanation.
func TestValidateBranchIssuePatternFallsBack(t *testing.T) {
	for _, bad := range []string{"", "  ", `^issue/(\d+`, `^issue/\d+`} {
		c := defaults()
		c.StatusLine.BranchIssuePattern = bad
		diags := validate(c)
		if c.StatusLine.BranchIssuePattern != DefaultBranchIssuePattern {
			t.Fatalf("%q must fall back to the default, got %q", bad, c.StatusLine.BranchIssuePattern)
		}
		if len(diagsFor(diags, "statusline.branch_issue_pattern")) != 1 {
			t.Fatalf("%q must report exactly one diagnostic, got %v", bad, diags)
		}
		if msg := ValidateBranchIssuePattern(bad); msg == "" {
			t.Fatalf("ValidateBranchIssuePattern(%q) must name a reason", bad)
		}
	}
	// A repository with another naming convention keeps its own pattern.
	c := defaults()
	c.StatusLine.BranchIssuePattern = `^feature/ISSUE-(\d+)`
	if diags := validate(c); len(diagsFor(diags, "statusline.branch_issue_pattern")) != 0 {
		t.Fatalf("a valid custom pattern must survive: %v", diags)
	}
	if c.StatusLine.BranchIssuePattern != `^feature/ISSUE-(\d+)` {
		t.Fatalf("a valid custom pattern must survive, got %q", c.StatusLine.BranchIssuePattern)
	}
}

// The on/off switch takes only those two values.
func TestValidateBranchIssueSwitch(t *testing.T) {
	c := defaults()
	c.StatusLine.BranchIssue = "yes"
	diags := validate(c)
	if c.StatusLine.BranchIssue != "off" {
		t.Fatalf("an unknown value must fall back to off, got %q", c.StatusLine.BranchIssue)
	}
	if len(diagsFor(diags, "statusline.branch_issue")) != 1 {
		t.Fatalf("want one diagnostic, got %v", diags)
	}
}
