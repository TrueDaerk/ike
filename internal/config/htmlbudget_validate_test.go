package config

import "testing"

// htmlbudget_validate_test.go pins the preview.html_render_budget_kb default
// and bounds (#2745): the HTML preview renders 2 MiB of a page out of the
// box, and a value outside 64 KiB–64 MiB falls back with a diagnostic.

func TestHTMLRenderBudgetDefault(t *testing.T) {
	cfg := defaults()
	if cfg.Preview.HTMLRenderBudgetKB != 2048 || DefaultHTMLRenderBudgetKB != 2048 {
		t.Fatalf("html_render_budget_kb = %d, want the documented 2048", cfg.Preview.HTMLRenderBudgetKB)
	}
	if v := cfg.Flat()["preview.html_render_budget_kb"]; v != "2048" {
		t.Fatalf("flat key = %q, want \"2048\"", v)
	}
}

func TestHTMLRenderBudgetValidation(t *testing.T) {
	cases := []struct {
		in, want int
		diag     bool
	}{
		{in: 0, want: DefaultHTMLRenderBudgetKB, diag: true},
		{in: HTMLRenderBudgetKBMin - 1, want: DefaultHTMLRenderBudgetKB, diag: true},
		{in: HTMLRenderBudgetKBMin, want: HTMLRenderBudgetKBMin},
		{in: 512, want: 512},
		{in: HTMLRenderBudgetKBMax, want: HTMLRenderBudgetKBMax},
		{in: HTMLRenderBudgetKBMax + 1, want: DefaultHTMLRenderBudgetKB, diag: true},
	}
	for _, c := range cases {
		cfg := defaults()
		cfg.Preview.HTMLRenderBudgetKB = c.in
		diags := validate(cfg)
		if cfg.Preview.HTMLRenderBudgetKB != c.want {
			t.Errorf("budget %d: got %d, want %d", c.in, cfg.Preview.HTMLRenderBudgetKB, c.want)
		}
		found := false
		for _, d := range diags {
			if d.Field == "preview.html_render_budget_kb" {
				found = true
			}
		}
		if found != c.diag {
			t.Errorf("budget %d: diagnostic presence = %v, want %v", c.in, found, c.diag)
		}
	}
}
