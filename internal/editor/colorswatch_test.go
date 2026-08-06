package editor

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

// TestParseColorLiterals covers the #790 detection: all four literal forms,
// invalid values yield no span, positions are rune columns.
func TestParseColorLiterals(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []colorSpan
	}{
		{`color: #ff8000;`, []colorSpan{{Start: 7, End: 14, R: 255, G: 128, B: 0}}},
		{`bg: #fff`, []colorSpan{{Start: 4, End: 8, R: 255, G: 255, B: 255}}},
		{`c = rgb(255, 0, 100)`, []colorSpan{{Start: 4, End: 20, R: 255, G: 0, B: 100}}},
		{`c = rgba(0, 128, 255, 0.5)`, []colorSpan{{Start: 4, End: 26, R: 0, G: 128, B: 255}}},
		{`c = rgb(100%, 0%, 50%)`, []colorSpan{{Start: 4, End: 22, R: 255, G: 0, B: 128}}},
		{`c = hsl(0, 100%, 50%)`, []colorSpan{{Start: 4, End: 21, R: 255, G: 0, B: 0}}},
		{`c = hsl(120, 100%, 25%)`, []colorSpan{{Start: 4, End: 23, R: 0, G: 128, B: 0}}},
		{`c = hsla(240, 100%, 50%, 0.3)`, []colorSpan{{Start: 4, End: 29, R: 0, G: 0, B: 255}}},
		// Invalid forms: out-of-range channel, wrong arity, bad hex length.
		{`c = rgb(300, 0, 0)`, nil},
		{`c = rgb(1, 2)`, nil},
		{`c = #ab`, nil},
		{`c = #abcde`, nil}, // 5 hex digits: no 3- or 6-digit boundary match
		{`plain text without colors`, nil},
	} {
		got := parseColorLiterals(tc.line, colorAll)
		if len(got) != len(tc.want) {
			t.Errorf("%q: %d spans (%v), want %d", tc.line, len(got), got, len(tc.want))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q: span %d = %+v, want %+v", tc.line, i, got[i], tc.want[i])
			}
		}
	}
}

// TestParseColorLiteralsMultiple: several literals on one line all detect.
func TestParseColorLiteralsMultiple(t *testing.T) {
	got := parseColorLiterals(`gradient: #000 to rgb(255,255,255);`, colorAll)
	if len(got) != 2 {
		t.Fatalf("spans = %d (%v), want 2", len(got), got)
	}
	if got[0].R != 0 || got[1].R != 255 {
		t.Errorf("colors = %+v", got)
	}
}

// TestSwatchContrast: luminance picks black text on light colors, white on
// dark.
func TestSwatchContrast(t *testing.T) {
	light := swatchStyle(colorSpan{R: 255, G: 255, B: 0}) // yellow
	if fg := light.GetForeground(); fg != nil {
		if s, ok := fg.(interface{ String() string }); ok && !strings.Contains(strings.ToLower(s.String()), "#000000") {
			t.Errorf("light color foreground = %v, want black", fg)
		}
	}
	dark := swatchStyle(colorSpan{R: 0, G: 0, B: 128}) // navy
	if fg := dark.GetForeground(); fg != nil {
		if s, ok := fg.(interface{ String() string }); ok && !strings.Contains(strings.ToLower(s.String()), "#ffffff") {
			t.Errorf("dark color foreground = %v, want white", fg)
		}
	}
}

// TestColorSwatchRendering: the literal's cells carry a truecolor background
// in the rendered view; the toggle switches it off.
func TestColorSwatchRendering(t *testing.T) {
	m, _ := loaded(t, "x\ncolor: #ff8000;\n")
	// Keep the cursor off the literal's line so no cursor styling interferes.
	view := m.View()
	if !strings.Contains(view, "48;2;255;128;0") {
		t.Errorf("view misses the literal's truecolor background:\n%q", view)
	}
	if !strings.Contains(stripAnsiAll(view), "#ff8000") {
		t.Error("literal text must stay visible")
	}

	m.colorPreview = false
	m.bumpRender()
	if view := m.View(); strings.Contains(view, "48;2;255;128;0") {
		t.Error("toggle off must remove the tint")
	}
}

func stripAnsiAll(s string) string { return ansiRE.ReplaceAllString(s, "") }

// TestParseColorLiteralsAlphaHex covers the #1622 additions: 4- and 8-digit
// hex parse, with the alpha component dropped (no alpha in a terminal cell).
func TestParseColorLiteralsAlphaHex(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []colorSpan
	}{
		{`c: #ff880080;`, []colorSpan{{Start: 3, End: 12, R: 255, G: 136, B: 0}}},
		{`c: #f808;`, []colorSpan{{Start: 3, End: 8, R: 255, G: 136, B: 0}}},
		// 5 and 7 digits are no color at all.
		{`c: #ff880;`, nil},
		{`c: #ff8800a;`, nil},
	} {
		got := parseColorLiterals(tc.line, colorAll)
		if len(got) != len(tc.want) {
			t.Errorf("%q: %d spans (%v), want %d", tc.line, len(got), got, len(tc.want))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q: span %d = %+v, want %+v", tc.line, i, got[i], tc.want[i])
			}
		}
	}
}

// TestParseColorLiteralsValuePolicy: outside the CSS family a literal only
// tints in a value position (#1622) — config values do, URL fragments and
// hash suffixes do not.
func TestParseColorLiteralsValuePolicy(t *testing.T) {
	for _, tc := range []struct {
		line string
		want int
	}{
		{`accent = "#ff8800"`, 1},     // TOML
		{`accent: #ff8800`, 1},        // YAML
		{`  "accent": "#ff8800",`, 1}, // JSON
		{`bg = rgb(255, 136, 0)`, 1},
		{`https://example.com/x#ff8800`, 0}, // URL fragment
		{`commit abc#ff8800`, 0},
		{`#ff8800`, 1}, // whole line is the literal
	} {
		if got := parseColorLiterals(tc.line, colorValue); len(got) != tc.want {
			t.Errorf("%q: %d spans (%v), want %d", tc.line, len(got), got, tc.want)
		}
		// Under the CSS policy every one of them tints.
		if tc.want == 0 && len(parseColorLiterals(tc.line, colorAll)) == 0 {
			t.Errorf("%q: colorAll must still detect the literal", tc.line)
		}
	}
}

// TestColorPolicyPerLanguage: CSS-family files get the always-on scope, other
// languages the value-position scope (#1622).
func TestColorPolicyPerLanguage(t *testing.T) {
	// Language plugins live under plugins/ and are not imported here, so the
	// CSS family is registered locally for this test.
	lang.Register(lang.Language{ID: "css", Extensions: []string{"css", "scss", "less"}})
	lang.Register(lang.Language{ID: "toml", Extensions: []string{"toml"}})
	for name, want := range map[string]colorPolicy{
		"a.css":  colorAll,
		"a.scss": colorAll,
		"a.less": colorAll,
		"a.toml": colorValue,
		"a.yaml": colorValue,
		"a.json": colorValue,
		"a.go":   colorValue,
	} {
		m, _ := loadedAs(t, name, "x\n")
		if got := m.colorPolicy(); got != want {
			t.Errorf("%s: policy = %v, want %v", name, got, want)
		}
	}
}

// TestColorPreviewValuePolicyRendering: a URL fragment in a .txt file gets no
// tint, a config value does.
func TestColorPreviewValuePolicyRendering(t *testing.T) {
	m, _ := loadedAs(t, "a.toml", "x\nurl = \"https://e.com/p#ff8000\"\n")
	if strings.Contains(m.View(), "48;2;255;128;0") {
		t.Error("URL fragment must not tint outside the CSS family")
	}
	m2, _ := loadedAs(t, "a.toml", "x\naccent = \"#ff8000\"\n")
	if !strings.Contains(m2.View(), "48;2;255;128;0") {
		t.Error("config value must tint")
	}
}

// TestToggleColorPreviewSticks: the per-view toggle overrides the config
// default and applyConfig no longer clobbers it (#1622).
func TestToggleColorPreviewSticks(t *testing.T) {
	m, _ := loaded(t, "color: #ff8000;\n")
	m.toggleColorPreview()
	if m.colorPreview {
		t.Fatal("toggle must switch the preview off")
	}
	m.applyConfig()
	if m.colorPreview {
		t.Error("applyConfig must not clobber the per-view override")
	}
}
