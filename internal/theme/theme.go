// Package theme defines IKE's named color schemes (Roadmap 0110). A Theme
// bundles the three color groups — semantic ui chrome slots, syntax capture
// defaults, and explorer file-color defaults — so selecting one name recolors
// the whole IDE coherently. The package is leaf-level (lipgloss only) so
// highlight, explorer, app, and editor can all import it without a cycle.
//
// Naming caution: internal/palette is the command palette (Roadmap 0070); the
// resolved color set here is theme.Palette.
package theme

import (
	"image/color"
	"sync"
)

// UI is the flat set of semantic chrome slots, following the Textual / sqlit
// theme model. Values are color tokens (name, hex, or ANSI index) resolved by
// Resolve. A slot left empty falls back to the default palette's value when
// the theme is resolved into a Palette.
type UI struct {
	Background      string // app-wide background: dividers, gaps
	Foreground      string // default text
	Surface         string // pane body background
	Panel           string // raised surfaces: status bar, popups, hover rows
	Border          string // blurred pane borders, dividers, scrollbar track
	BorderFocus     string // focused pane border
	Selection       string // selected-row background
	SelectionText   string // text on Selection
	SelectionMuted  string // low-emphasis selection (editor visual range)
	OccurrenceRead  string // symbol-occurrence mark, read access (LSP document highlight)
	OccurrenceWrite string // symbol-occurrence mark, write access
	InlayHint       string // inline LSP inlay-hint text (dimmed parameter/type hints, #171)
	Accent          string // emphasis foreground (explorer active entry)
	Primary         string // primary action background (completion selected row)
	Secondary       string // secondary emphasis foreground (help shortcut keys)
	Success         string
	Warning         string // diagnostic warning
	Error           string // diagnostic error
	Info            string // diagnostic info
	Hint            string // diagnostic hint
	MoveSource      string // pane-move source border
	DropTarget      string // pane-move drop-target border
	Ghost           string // pane-move ghost preview
	ScrollbarTrack  string
	ScrollbarThumb  string
}

// Theme is one named color scheme: ui chrome slots plus the default sources
// for the two existing color models (highlight captures, explorer file colors).
// Per-key config (theme.captures.*, [explorer.colors]) still overrides on top.
type Theme struct {
	Name     string
	Dark     bool
	UI       UI
	Captures map[string]string // capture name -> color token (internal/highlight defaults)
	Files    map[string]string // glob|ext -> color token (internal/explorer defaults)
}

// Palette is a resolved Theme: every ui slot resolved to a concrete color,
// empty slots backfilled from the default palette so consumers never see a
// zero color. Captures/Files stay token maps because their consumers layer
// config on top before resolving.
type Palette struct {
	Name     string
	Dark     bool
	Captures map[string]string
	Files    map[string]string

	Background      color.Color
	Foreground      color.Color
	Surface         color.Color
	Panel           color.Color
	Border          color.Color
	BorderFocus     color.Color
	Selection       color.Color
	SelectionText   color.Color
	SelectionMuted  color.Color
	OccurrenceRead  color.Color
	OccurrenceWrite color.Color
	InlayHint       color.Color
	Accent          color.Color
	Primary         color.Color
	Secondary       color.Color
	Success         color.Color
	Warning         color.Color
	Error           color.Color
	Info            color.Color
	Hint            color.Color
	MoveSource      color.Color
	DropTarget      color.Color
	Ghost           color.Color
	ScrollbarTrack  color.Color
	ScrollbarThumb  color.Color
}

// firstNonEmpty returns the first non-empty token, for slot fallback chains.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// DefaultPalette returns the resolved default theme, cached. Renderers use it
// as the fallback when no palette has been threaded in (tests, zero values).
var DefaultPalette = sync.OnceValue(func() *Palette { return NewPalette(Default()) })

// NewPalette resolves t into concrete colors. Empty ui slots and missing
// capture/file maps fall back to the default theme, so a sparse third-party
// theme still yields a complete palette.
func NewPalette(t Theme) *Palette {
	def := Default()
	slot := func(v, fallback string) color.Color {
		if v == "" {
			v = fallback
		}
		return Resolve(v)
	}
	captures := t.Captures
	if captures == nil {
		captures = def.Captures
	}
	files := t.Files
	if files == nil {
		files = def.Files
	}
	return &Palette{
		Name:     t.Name,
		Dark:     t.Dark,
		Captures: captures,
		Files:    files,

		Background:     slot(t.UI.Background, def.UI.Background),
		Foreground:     slot(t.UI.Foreground, def.UI.Foreground),
		Surface:        slot(t.UI.Surface, def.UI.Surface),
		Panel:          slot(t.UI.Panel, def.UI.Panel),
		Border:         slot(t.UI.Border, def.UI.Border),
		BorderFocus:    slot(t.UI.BorderFocus, def.UI.BorderFocus),
		Selection:      slot(t.UI.Selection, def.UI.Selection),
		SelectionText:  slot(t.UI.SelectionText, def.UI.SelectionText),
		SelectionMuted: slot(t.UI.SelectionMuted, def.UI.SelectionMuted),
		// Occurrence marks fall back to the theme's own muted selection (then
		// the default's), so a theme without the slots still marks subtly in
		// its own colors instead of inheriting the default theme's.
		OccurrenceRead:  slot(t.UI.OccurrenceRead, firstNonEmpty(t.UI.SelectionMuted, def.UI.SelectionMuted)),
		OccurrenceWrite: slot(t.UI.OccurrenceWrite, firstNonEmpty(t.UI.SelectionMuted, def.UI.SelectionMuted)),
		// Inlay-hint text falls back to the theme's own border tone: already a
		// legible-but-dim foreground in every theme, which is exactly what a
		// hint should be.
		InlayHint:      slot(t.UI.InlayHint, firstNonEmpty(t.UI.Border, def.UI.Border)),
		Accent:         slot(t.UI.Accent, def.UI.Accent),
		Primary:        slot(t.UI.Primary, def.UI.Primary),
		Secondary:      slot(t.UI.Secondary, def.UI.Secondary),
		Success:        slot(t.UI.Success, def.UI.Success),
		Warning:        slot(t.UI.Warning, def.UI.Warning),
		Error:          slot(t.UI.Error, def.UI.Error),
		Info:           slot(t.UI.Info, def.UI.Info),
		Hint:           slot(t.UI.Hint, def.UI.Hint),
		MoveSource:     slot(t.UI.MoveSource, def.UI.MoveSource),
		DropTarget:     slot(t.UI.DropTarget, def.UI.DropTarget),
		Ghost:          slot(t.UI.Ghost, def.UI.Ghost),
		ScrollbarTrack: slot(t.UI.ScrollbarTrack, def.UI.ScrollbarTrack),
		ScrollbarThumb: slot(t.UI.ScrollbarThumb, def.UI.ScrollbarThumb),
	}
}
