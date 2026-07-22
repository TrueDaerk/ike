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
	th := NewTheme(nil, nil)
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
	th := NewTheme(nil, get)
	if _, ok := th.Style("keyword"); !ok {
		t.Error("overridden keyword should resolve")
	}
}

func TestThemePaletteDefaults(t *testing.T) {
	// Palette captures replace the built-in defaults (Roadmap 0110)…
	th := NewTheme(map[string]string{"keyword": "#bb9af7"}, nil)
	if _, ok := th.Style("keyword"); !ok {
		t.Error("palette-supplied keyword should resolve")
	}
	if _, ok := th.Style("string"); ok {
		t.Error("capture absent from palette defaults should not resolve")
	}
	// …and per-key config still wins over the palette.
	get := func(key string) (string, bool) {
		if key == "theme.captures.keyword" {
			return "red", true
		}
		return "", false
	}
	over := NewTheme(map[string]string{"keyword": "#bb9af7"}, get)
	st, ok := over.Style("keyword")
	if !ok {
		t.Fatal("keyword should resolve")
	}
	want, _ := NewTheme(map[string]string{"keyword": "red"}, nil).Style("keyword")
	if st.GetForeground() != want.GetForeground() {
		t.Error("config override should win over palette default")
	}
}

// TestRainbowThemeDerivation (#789): rainbow.N slots derive from existing
// palette captures and resolve for every cycle position.
func TestRainbowThemeDerivation(t *testing.T) {
	th := NewTheme(nil, nil)
	for i := 0; i < RainbowColors; i++ {
		if _, ok := th.Style(rainbowCapture(i)); !ok {
			t.Errorf("rainbow slot %d must resolve from the default palette", i)
		}
	}
	// Depth cycles: depth N and N+RainbowColors share a capture.
	if rainbowCapture(1) != rainbowCapture(1+RainbowColors) {
		t.Error("rainbow capture must cycle")
	}
	// A config override wins for its slot.
	th2 := NewTheme(nil, func(key string) (string, bool) {
		if key == "theme.captures.rainbow.0" {
			return "#123456", true
		}
		return "", false
	})
	if _, ok := th2.Style("rainbow.0"); !ok {
		t.Error("overridden rainbow slot must resolve")
	}
}

// TestRainbowToggle (#789): SetRainbow flips RainbowEnabled; default is on.
func TestRainbowToggle(t *testing.T) {
	if !RainbowEnabled() {
		t.Fatal("rainbow must default on")
	}
	SetRainbow(false)
	if RainbowEnabled() {
		t.Fatal("SetRainbow(false) must disable")
	}
	SetRainbow(true)
	if !RainbowEnabled() {
		t.Fatal("SetRainbow(true) must re-enable")
	}
}
