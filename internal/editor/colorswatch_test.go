package editor

import (
	"strings"
	"testing"
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
		got := parseColorLiterals(tc.line)
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
	got := parseColorLiterals(`gradient: #000 to rgb(255,255,255);`)
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
