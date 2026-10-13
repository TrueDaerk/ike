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
