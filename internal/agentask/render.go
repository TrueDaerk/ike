package agentask

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"

	"ike/internal/theme"
	"ike/internal/ui"
)

// RenderMarkdown renders an answer through a width- and theme-bound glamour
// renderer — the GitHub issues pane's pattern — with hanging indents on
// wrapped list items. A render failure falls back to the raw text, so the
// overlay always shows the answer.
func RenderMarkdown(src string, wrap int, pal *theme.Palette) string {
	wrap = max(10, wrap)
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(styleConfig(pal)),
		glamour.WithWordWrap(wrap),
	)
	if err != nil {
		return src
	}
	out, err := r.Render(src)
	if err != nil {
		return src
	}
	return strings.TrimRight(ui.HangingIndent(out, wrap), "\n")
}

// styleConfig picks the stock glamour style off the palette's dark flag and
// maps the heading and link colors onto the active palette.
func styleConfig(pal *theme.Palette) gansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	if pal != nil && !pal.Dark {
		cfg = styles.LightStyleConfig
	}
	if pal == nil {
		return cfg
	}
	accent := hexColor(pal.Accent)
	link := hexColor(pal.Info)
	if accent != "" {
		cfg.Heading.Color = &accent
		cfg.LinkText.Color = &accent
	}
	if link != "" {
		cfg.Link.Color = &link
	}
	return cfg
}

// hexColor formats a palette color as the #rrggbb string glamour styles take.
func hexColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
