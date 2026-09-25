package config

import "testing"

// htmlbrowser_validate_test.go pins the HTML preview browser mode's settings
// (#2746): preview.html_browser defaults to auto-detection (empty) and
// preview.html_browser_timeout_s to 20 seconds, falling back with a
// diagnostic outside 1–300.

func TestHTMLBrowserDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.Preview.HTMLBrowser != "" {
		t.Fatalf("html_browser = %q, want empty (auto-detect)", cfg.Preview.HTMLBrowser)
	}
	if cfg.Preview.HTMLBrowserTimeoutS != 20 || DefaultHTMLBrowserTimeoutS != 20 {
		t.Fatalf("html_browser_timeout_s = %d, want the documented 20", cfg.Preview.HTMLBrowserTimeoutS)
	}
	flat := cfg.Flat()
	if v, ok := flat["preview.html_browser"]; !ok || v != "" {
		t.Fatalf("flat html_browser = %q (present %v), want present and empty", v, ok)
	}
	if v := flat["preview.html_browser_timeout_s"]; v != "20" {
		t.Fatalf("flat html_browser_timeout_s = %q, want \"20\"", v)
	}
}

func TestHTMLBrowserTimeoutValidation(t *testing.T) {
	cases := []struct {
		in, want int
		diag     bool
	}{
		{in: 0, want: DefaultHTMLBrowserTimeoutS, diag: true},
		{in: -5, want: DefaultHTMLBrowserTimeoutS, diag: true},
		{in: HTMLBrowserTimeoutSMin, want: HTMLBrowserTimeoutSMin},
		{in: 45, want: 45},
		{in: HTMLBrowserTimeoutSMax, want: HTMLBrowserTimeoutSMax},
		{in: HTMLBrowserTimeoutSMax + 1, want: DefaultHTMLBrowserTimeoutS, diag: true},
	}
	for _, c := range cases {
		cfg := defaults()
		cfg.Preview.HTMLBrowserTimeoutS = c.in
		diags := validate(cfg)
		if cfg.Preview.HTMLBrowserTimeoutS != c.want {
			t.Errorf("timeout %d: got %d, want %d", c.in, cfg.Preview.HTMLBrowserTimeoutS, c.want)
		}
		found := false
		for _, d := range diags {
			if d.Field == "preview.html_browser_timeout_s" {
				found = true
			}
		}
		if found != c.diag {
			t.Errorf("timeout %d: diagnostic presence = %v, want %v", c.in, found, c.diag)
		}
	}
}
