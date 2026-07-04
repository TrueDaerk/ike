package highlight

import "testing"

// Note: language detection (Lang/Supported) now lives in the internal/lang
// registry and is tested there; the real per-grammar highlighting is tested in
// each language plugin (plugins/languages/*). This file covers the engine bits
// that carry no language knowledge: the span index and the theme.

func TestIndexCaptureAt(t *testing.T) {
	ix := NewIndex([]Span{
		{Line: 0, StartCol: 0, EndCol: 4, Capture: "keyword"},
		{Line: 0, StartCol: 5, EndCol: 9, Capture: "function"},
	})
	if got := ix.CaptureAt(0, 2); got != "keyword" {
		t.Errorf("CaptureAt(0,2) = %q, want keyword", got)
	}
	if got := ix.CaptureAt(0, 4); got != "" { // half-open: 4 is excluded
		t.Errorf("CaptureAt(0,4) = %q, want empty", got)
	}
	if got := ix.CaptureAt(0, 6); got != "function" {
		t.Errorf("CaptureAt(0,6) = %q, want function", got)
	}
	if got := ix.CaptureAt(1, 0); got != "" {
		t.Errorf("CaptureAt(1,0) = %q, want empty", got)
	}
}

func TestThemeFallback(t *testing.T) {
	th := NewTheme(nil)
	// Dotted capture inherits its head colour.
	if _, ok := th.Style("function.builtin"); !ok {
		t.Error("function.builtin should resolve via function fallback")
	}
	if _, ok := th.Style("keyword"); !ok {
		t.Error("keyword should resolve")
	}
	if _, ok := th.Style("nonsense.capture.name"); ok {
		t.Error("unknown capture should not resolve")
	}
	if _, ok := th.Style(""); ok {
		t.Error("empty capture should not resolve")
	}
}

func TestThemeOverride(t *testing.T) {
	get := func(key string) (string, bool) {
		if key == "theme.captures.keyword" {
			return "red", true
		}
		return "", false
	}
	th := NewTheme(get)
	if _, ok := th.Style("keyword"); !ok {
		t.Error("overridden keyword should resolve")
	}
}
