package config

import "testing"

// htmlopenmode_validate_test.go pins preview.html_open_mode (#2766): an
// opened HTML file starts in the rendered preview by default, "source" opens
// the editor, and anything else falls back to the preview with a diagnostic.

func TestHTMLOpenModeDefault(t *testing.T) {
	cfg := defaults()
	if cfg.Preview.HTMLOpenMode != HTMLOpenPreview {
		t.Fatalf("html_open_mode = %q, want %q", cfg.Preview.HTMLOpenMode, HTMLOpenPreview)
	}
	if v := cfg.Flat()["preview.html_open_mode"]; v != "preview" {
		t.Fatalf("flat html_open_mode = %q, want \"preview\"", v)
	}
}

func TestHTMLOpenModeValidation(t *testing.T) {
	cases := []struct {
		in, want string
		diag     bool
	}{
		{in: "preview", want: "preview"},
		{in: "source", want: "source"},
		{in: "", want: "preview", diag: true},
		{in: "Source", want: "preview", diag: true},
		{in: "split", want: "preview", diag: true},
	}
	for _, c := range cases {
		cfg := defaults()
		cfg.Preview.HTMLOpenMode = c.in
		diags := validate(cfg)
		if cfg.Preview.HTMLOpenMode != c.want {
			t.Errorf("mode %q: got %q, want %q", c.in, cfg.Preview.HTMLOpenMode, c.want)
		}
		found := false
		for _, d := range diags {
			if d.Field == "preview.html_open_mode" {
				found = true
			}
		}
		if found != c.diag {
			t.Errorf("mode %q: diagnostic presence = %v, want %v", c.in, found, c.diag)
		}
	}
}
