package config

import "testing"

// The post-switch warm-up notice threshold (#2629) defaults to 15 s, keeps 0
// as its off switch and refuses values outside the accepted window.
func TestValidateLSPWarmupNoticeMs(t *testing.T) {
	if c := defaults(); c.LSP.WarmupNoticeMs != 15000 {
		t.Errorf("default threshold = %d, want 15000", c.LSP.WarmupNoticeMs)
	}
	for _, bad := range []int{-1, -15000, 600001} {
		c := defaults()
		c.LSP.WarmupNoticeMs = bad
		diags := validate(c)
		if c.LSP.WarmupNoticeMs != 15000 {
			t.Errorf("threshold %d validated to %d, want the 15000 fallback", bad, c.LSP.WarmupNoticeMs)
		}
		if len(diagsFor(diags, "lsp.warmup_notice_ms")) != 1 {
			t.Errorf("threshold %d must be reported once, got %v", bad, diags)
		}
	}
	// 0 is the documented off switch and survives validation untouched.
	for _, good := range []int{0, 1, 15000, 600000} {
		c := defaults()
		c.LSP.WarmupNoticeMs = good
		if diags := validate(c); len(diagsFor(diags, "lsp.warmup_notice_ms")) != 0 || c.LSP.WarmupNoticeMs != good {
			t.Errorf("threshold %d is valid: %d, %v", good, c.LSP.WarmupNoticeMs, diags)
		}
	}
	// The key reads as a dotted string, which is what the settings panel and
	// the config viewer render.
	if got := defaults().Flat()["lsp.warmup_notice_ms"]; got != "15000" {
		t.Errorf("lsp.warmup_notice_ms = %q", got)
	}
}
