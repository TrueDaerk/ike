package settings

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// themepreview.go renders the theme enums' inline palette swatches (#1664):
// each option row of theme.name / theme.light / theme.dark carries a strip of
// cells from that theme's own resolved palette — background, foreground,
// accent and two syntax colors — so themes compare at a glance without being
// applied one by one.

// swatchCell is one swatch color's glyph; two block cells read as a solid
// chip at every cell aspect ratio.
const swatchCell = "██"

// themePreviewFn builds the Entry.Preview for the theme enums, memoized per
// name: resolving a palette walks every UI slot, and the option list renders
// each visible row every frame. extra is the registry's plugin theme list.
func themePreviewFn(extra []theme.Theme) func(string) string {
	cache := map[string]string{}
	return func(name string) string {
		if s, ok := cache[name]; ok {
			return s
		}
		s := themeSwatch(name, extra)
		cache[name] = s
		return s
	}
}

// themeSwatch renders one theme's swatch strip, or "" when the name matches
// no registered theme (an unknown name must not preview as the default).
func themeSwatch(name string, extra []theme.Theme) string {
	t, found := theme.Select(name, extra)
	if !found {
		return ""
	}
	p := theme.NewPalette(t)
	colors := []color.Color{
		p.Background,
		p.Foreground,
		p.Accent,
		captureColor(p, "keyword", p.Primary),
		captureColor(p, "string", p.Success),
	}
	var out string
	for _, c := range colors {
		out += lipgloss.NewStyle().Foreground(c).Render(swatchCell)
	}
	return out
}

// themeRowColorsFn builds the Entry.RowColors for the theme enums, memoized
// per name for the same reason as themePreviewFn: every visible row asks for
// its colors on every frame.
func themeRowColorsFn(extra []theme.Theme) func(string) (color.Color, color.Color, bool) {
	type pair struct {
		fg, bg color.Color
		ok     bool
	}
	cache := map[string]pair{}
	return func(name string) (color.Color, color.Color, bool) {
		p, hit := cache[name]
		if !hit {
			p.fg, p.bg, p.ok = themeRowColors(name, extra)
			cache[name] = p
		}
		return p.fg, p.bg, p.ok
	}
}

// themeRowColors resolves the foreground/background an option row is painted
// in (#1688). An unknown name reports !ok, so it keeps the panel's own colors
// instead of previewing as the default theme.
func themeRowColors(name string, extra []theme.Theme) (color.Color, color.Color, bool) {
	t, found := theme.Select(name, extra)
	if !found {
		return nil, nil, false
	}
	p := theme.NewPalette(t)
	return p.Foreground, p.Background, true
}

// captureColor resolves a syntax capture's token from the palette's capture
// map, falling back to a UI slot when the theme does not define it.
func captureColor(p *theme.Palette, capture string, fallback color.Color) color.Color {
	if token, ok := p.Captures[capture]; ok && token != "" {
		return theme.Resolve(token)
	}
	return fallback
}
