package theme

import (
	"image/color"
	"math"
	"testing"
)

// relativeLuminance implements the WCAG 2.x relative-luminance formula
// (https://www.w3.org/TR/WCAG21/#dfn-relative-luminance).
func relativeLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrastRatio returns the WCAG contrast ratio between two colors, in [1, 21].
func contrastRatio(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// minTextContrast is the WCAG AA threshold for normal text. Every built-in
// theme must clear it on the fg/bg slot pairs the chrome actually renders;
// new themes that ship unreadable pairs fail this test.
const minTextContrast = 4.5

// TestBuiltinThemeContrast audits every built-in theme across the semantic
// fg/bg slot pairs used by the UI chrome (issue #384).
func TestBuiltinThemeContrast(t *testing.T) {
	pairs := []struct {
		name string
		fg   func(*Palette) color.Color
		bg   func(*Palette) color.Color
	}{
		{"Foreground/Background", func(p *Palette) color.Color { return p.Foreground }, func(p *Palette) color.Color { return p.Background }},
		{"Foreground/Surface", func(p *Palette) color.Color { return p.Foreground }, func(p *Palette) color.Color { return p.Surface }},
		{"Foreground/Panel", func(p *Palette) color.Color { return p.Foreground }, func(p *Palette) color.Color { return p.Panel }},
		{"SelectionText/Selection", func(p *Palette) color.Color { return p.SelectionText }, func(p *Palette) color.Color { return p.Selection }},
		{"SelectionText/Primary", func(p *Palette) color.Color { return p.SelectionText }, func(p *Palette) color.Color { return p.Primary }},
		{"Accent/Surface", func(p *Palette) color.Color { return p.Accent }, func(p *Palette) color.Color { return p.Surface }},
		{"Secondary/Surface", func(p *Palette) color.Color { return p.Secondary }, func(p *Palette) color.Color { return p.Surface }},
		{"Secondary/Panel", func(p *Palette) color.Color { return p.Secondary }, func(p *Palette) color.Color { return p.Panel }},
		{"Success/Surface", func(p *Palette) color.Color { return p.Success }, func(p *Palette) color.Color { return p.Surface }},
		{"Warning/Surface", func(p *Palette) color.Color { return p.Warning }, func(p *Palette) color.Color { return p.Surface }},
		{"Warning/Panel", func(p *Palette) color.Color { return p.Warning }, func(p *Palette) color.Color { return p.Panel }},
		{"Error/Surface", func(p *Palette) color.Color { return p.Error }, func(p *Palette) color.Color { return p.Surface }},
		{"Error/Panel", func(p *Palette) color.Color { return p.Error }, func(p *Palette) color.Color { return p.Panel }},
		{"Info/Surface", func(p *Palette) color.Color { return p.Info }, func(p *Palette) color.Color { return p.Surface }},
		{"Info/Panel", func(p *Palette) color.Color { return p.Info }, func(p *Palette) color.Color { return p.Panel }},
		{"Hint/Surface", func(p *Palette) color.Color { return p.Hint }, func(p *Palette) color.Color { return p.Surface }},
		{"Hint/Panel", func(p *Palette) color.Color { return p.Hint }, func(p *Palette) color.Color { return p.Panel }},
	}
	for _, th := range Builtins() {
		p := NewPalette(th)
		t.Run(th.Name, func(t *testing.T) {
			for _, pair := range pairs {
				ratio := contrastRatio(pair.fg(p), pair.bg(p))
				if ratio < minTextContrast {
					t.Errorf("%s: contrast %.2f:1, want >= %.1f:1", pair.name, ratio, minTextContrast)
				}
			}
		})
	}
}

func TestContrastRatioFormula(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"#ffffff", "#000000", 21},
		{"#ffffff", "#ffffff", 1},
		{"#777777", "#ffffff", 4.48}, // canonical WCAG example just below AA
	}
	for _, c := range cases {
		got := contrastRatio(Resolve(c.a), Resolve(c.b))
		if math.Abs(got-c.want) > 0.02 {
			t.Errorf("contrastRatio(%s, %s) = %.2f, want %.2f", c.a, c.b, got, c.want)
		}
	}
}
