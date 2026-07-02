package highlight

import "testing"

func TestLang(t *testing.T) {
	cases := map[string]string{
		"main.go":     "go",
		"app.py":      "python",
		"stub.pyi":    "python",
		"index.php":   "php",
		"page.phtml":  "php",
		"README.md":   "",
		"noext":       "",
		"DIR/Main.GO": "go", // case-insensitive
	}
	for path, want := range cases {
		if got := Lang(path); got != want {
			t.Errorf("Lang(%q) = %q, want %q", path, got, want)
		}
	}
}

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
