package config

import "testing"

// The muted-roots list of the silent-server notice (#2886) defaults empty,
// drops entries that can never match a project root with a diagnostic each,
// and matches roots cleaned.
func TestValidateWarmupNoticeMutedRoots(t *testing.T) {
	if c := defaults(); len(c.LSP.WarmupNoticeMutedRoots) != 0 {
		t.Errorf("default muted roots = %v, want empty", c.LSP.WarmupNoticeMutedRoots)
	}
	c := defaults()
	c.LSP.WarmupNoticeMutedRoots = []string{"/srv/app/", "relative", "  ", "/home/me/x"}
	diags := validate(c)
	if n := len(diagsFor(diags, "lsp.warmup_notice_muted_roots")); n != 2 {
		t.Errorf("diagnostics = %d, want 2 (relative + blank): %v", n, diags)
	}
	got := c.LSP.WarmupNoticeMutedRoots
	if len(got) != 2 || got[0] != "/srv/app" || got[1] != "/home/me/x" {
		t.Errorf("kept roots = %v, want [/srv/app /home/me/x]", got)
	}
	if !c.LSP.WarmupNoticeMuted("/srv/app") || !c.LSP.WarmupNoticeMuted("/srv/./app/") {
		t.Error("a listed root must match, cleaned")
	}
	if c.LSP.WarmupNoticeMuted("/srv") || c.LSP.WarmupNoticeMuted("") {
		t.Error("only listed roots are muted")
	}
}
