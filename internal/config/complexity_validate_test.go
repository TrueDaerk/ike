package config

import "testing"

// The comment-density threshold (#2934) defaults to 40 %, keeps 0 as its
// off switch and falls back to the default outside 0–100.
func TestValidateComplexityCommentDensityWarn(t *testing.T) {
	if c := defaults(); c.Complexity.CommentDensityWarn != 40 {
		t.Errorf("default threshold = %d, want 40", c.Complexity.CommentDensityWarn)
	}
	for _, bad := range []int{-1, 101, 500} {
		c := defaults()
		c.Complexity.CommentDensityWarn = bad
		diags := validate(c)
		if c.Complexity.CommentDensityWarn != 40 {
			t.Errorf("threshold %d validated to %d, want the 40 fallback", bad, c.Complexity.CommentDensityWarn)
		}
		if len(diagsFor(diags, "complexity.comment_density_warn")) != 1 {
			t.Errorf("threshold %d must be reported once, got %v", bad, diags)
		}
	}
	for _, ok := range []int{0, 1, 55, 100} {
		c := defaults()
		c.Complexity.CommentDensityWarn = ok
		if diags := validate(c); len(diagsFor(diags, "complexity.comment_density_warn")) != 0 || c.Complexity.CommentDensityWarn != ok {
			t.Errorf("%d is a valid threshold: %d, %v", ok, c.Complexity.CommentDensityWarn, diags)
		}
	}
}

// The hotspot churn window (#2957) defaults to 90 days and falls back to it
// outside 1–3650.
func TestValidateComplexityChurnDays(t *testing.T) {
	if c := defaults(); c.Complexity.ChurnDays != 90 {
		t.Errorf("default window = %d, want 90", c.Complexity.ChurnDays)
	}
	for _, bad := range []int{0, -5, 3651} {
		c := defaults()
		c.Complexity.ChurnDays = bad
		diags := validate(c)
		if c.Complexity.ChurnDays != 90 || len(diagsFor(diags, "complexity.churn_days")) != 1 {
			t.Errorf("window %d validated to %d (%v), want the 90 fallback reported once", bad, c.Complexity.ChurnDays, diags)
		}
	}
	for _, ok := range []int{1, 30, 3650} {
		c := defaults()
		c.Complexity.ChurnDays = ok
		if diags := validate(c); len(diagsFor(diags, "complexity.churn_days")) != 0 || c.Complexity.ChurnDays != ok {
			t.Errorf("%d is a valid window: %d, %v", ok, c.Complexity.ChurnDays, diags)
		}
	}
}

// The trend window (#2962) defaults to 30 commits and falls back to it
// outside 1–500.
func TestValidateComplexityTrendCommits(t *testing.T) {
	if c := defaults(); c.Complexity.TrendCommits != 30 {
		t.Errorf("default window = %d, want 30", c.Complexity.TrendCommits)
	}
	for _, bad := range []int{0, -1, 501} {
		c := defaults()
		c.Complexity.TrendCommits = bad
		diags := validate(c)
		if c.Complexity.TrendCommits != 30 || len(diagsFor(diags, "complexity.trend_commits")) != 1 {
			t.Errorf("window %d validated to %d (%v), want the 30 fallback reported once", bad, c.Complexity.TrendCommits, diags)
		}
	}
	for _, ok := range []int{1, 30, 500} {
		c := defaults()
		c.Complexity.TrendCommits = ok
		if diags := validate(c); len(diagsFor(diags, "complexity.trend_commits")) != 0 || c.Complexity.TrendCommits != ok {
			t.Errorf("%d is a valid window: %d, %v", ok, c.Complexity.TrendCommits, diags)
		}
	}
}
