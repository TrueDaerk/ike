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
	"math"
	"sync"
)

// UI is the flat set of semantic chrome slots, following the Textual / sqlit
// theme model. Values are color tokens (name, hex, or ANSI index) resolved by
// Resolve. A slot left empty falls back to the default palette's value when
// the theme is resolved into a Palette.
type UI struct {
	Background         string // app-wide background: dividers, gaps
	Foreground         string // default text
	Surface            string // pane body background
	Panel              string // raised surfaces: status bar, popups, hover rows
	Border             string // blurred pane borders, dividers, scrollbar track
	BorderFocus        string // focused pane border
	Selection          string // selected-row background
	SelectionText      string // text on Selection
	SelectionMuted     string // low-emphasis selection (editor visual range)
	OccurrenceRead     string // symbol-occurrence mark, read access (LSP document highlight)
	OccurrenceWrite    string // symbol-occurrence mark, write access
	InlayHint          string // inline LSP inlay-hint text (dimmed parameter/type hints, #171)
	Whitespace         string // visible whitespace glyphs (· / →, #64)
	IndentGuide        string // vertical indent-guide lines (#64)
	Ruler              string // column-ruler background tint (#64)
	Accent             string // emphasis foreground (explorer active entry)
	Primary            string // primary action background (completion selected row)
	Secondary          string // secondary emphasis foreground (help shortcut keys)
	Success            string
	Warning            string // diagnostic warning
	Error              string // diagnostic error
	Info               string // diagnostic info
	Hint               string // diagnostic hint
	PaneBadge          string // pane-number badge: focused pane's pill background (#2496)
	PaneBadgeText      string // digit on PaneBadge
	PaneBadgeMuted     string // pane-number badge: unfocused pane's pill background
	PaneBadgeMutedText string // digit on PaneBadgeMuted
	MoveSource         string // pane-move source border
	DropTarget         string // pane-move drop-target border
	Ghost              string // pane-move ghost preview
	ScrollbarTrack     string
	ScrollbarThumb     string
	DiffAdded          string // diff viewer: added-line background (#60)
	DiffRemoved        string // diff viewer: removed-line background
	DiffChanged        string // diff viewer: intra-line changed-range background
	DiffAddedEmph      string // diff viewer: intra-line changed range inside an added line (#2170)
	DiffRemovedEmph    string // diff viewer: intra-line changed range inside a removed line (#2170)
	DiffMarker         string // diff viewer: current-hunk gutter marker and targeted collapsed gap (#2494)
	VCSModified        string // vcs status foreground: modified/renamed files (Roadmap 0320)
	VCSAdded           string // vcs status foreground: added files
	VCSUntracked       string // vcs status foreground: untracked files
	VCSDeleted         string // vcs status foreground: deleted files
	VCSConflicted      string // vcs status foreground: merge-conflicted files
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
	// Terminal is the integrated terminal's 16-color ANSI palette plus its
	// default foreground/background (#1363). Optional: empty entries derive
	// from the theme's own colors (see ansi.go).
	Terminal Terminal
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

	Background         color.Color
	Foreground         color.Color
	Surface            color.Color
	Panel              color.Color
	Border             color.Color
	BorderFocus        color.Color
	Selection          color.Color
	SelectionText      color.Color
	SelectionMuted     color.Color
	OccurrenceRead     color.Color
	OccurrenceWrite    color.Color
	InlayHint          color.Color
	Whitespace         color.Color
	IndentGuide        color.Color
	Ruler              color.Color
	Accent             color.Color
	Primary            color.Color
	Secondary          color.Color
	Success            color.Color
	Warning            color.Color
	Error              color.Color
	Info               color.Color
	Hint               color.Color
	PaneBadge          color.Color
	PaneBadgeText      color.Color
	PaneBadgeMuted     color.Color
	PaneBadgeMutedText color.Color
	MoveSource         color.Color
	DropTarget         color.Color
	Ghost              color.Color
	ScrollbarTrack     color.Color
	ScrollbarThumb     color.Color
	DiffAdded          color.Color
	DiffRemoved        color.Color
	DiffChanged        color.Color
	DiffAddedEmph      color.Color
	DiffRemovedEmph    color.Color
	DiffMarker         color.Color
	VCSModified        color.Color
	VCSAdded           color.Color
	VCSUntracked       color.Color
	VCSDeleted         color.Color
	VCSConflicted      color.Color

	// ANSI is the resolved 16-color terminal palette; TerminalFg/TerminalBg
	// are the terminal's default foreground/background (#1363). Indexed
	// colors in the integrated terminal's grid resolve against these instead
	// of the outer terminal's own palette.
	ANSI       [ANSICount]color.Color
	TerminalFg color.Color
	TerminalBg color.Color
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
	p := &Palette{
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
		InlayHint: slot(t.UI.InlayHint, firstNonEmpty(t.UI.Border, def.UI.Border)),
		// Whitespace glyphs and indent guides fall back to the theme's own
		// border tone (a legible-but-dim foreground in every theme); the
		// ruler tint falls back to the theme's panel surface, one step above
		// the pane body so the column reads as a subtle stripe.
		Whitespace:     slot(t.UI.Whitespace, firstNonEmpty(t.UI.Border, def.UI.Border)),
		IndentGuide:    slot(t.UI.IndentGuide, firstNonEmpty(t.UI.Border, def.UI.Border)),
		Ruler:          slot(t.UI.Ruler, firstNonEmpty(t.UI.Panel, def.UI.Panel)),
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
	// Diff backgrounds (#60) default to the theme's own semantic hues tinted
	// toward its surface, so every theme — including sparse third-party ones —
	// gets readable diff colors without declaring the slots.
	p.DiffAdded = slotOrMix(t.UI.DiffAdded, p.Success, p.Surface, 0.22)
	p.DiffRemoved = slotOrMix(t.UI.DiffRemoved, p.Error, p.Surface, 0.22)
	p.DiffChanged = slotOrMix(t.UI.DiffChanged, p.Warning, p.Surface, 0.42)
	// The intra-line emphasis backgrounds (#2170) are the *same hue* as the
	// line they sit in, pushed further away from Surface: a changed range
	// then reads as a stronger patch of its own side's colour instead of
	// borrowing a third one, which kept added and removed indistinguishable
	// inside a changed pair. Sparse themes derive them from the line
	// backgrounds resolved just above, so the pair always agrees.
	p.DiffAddedEmph = emphSlot(t.UI.DiffAddedEmph, p.Success, p.DiffAdded, p.Surface)
	p.DiffRemovedEmph = emphSlot(t.UI.DiffRemovedEmph, p.Error, p.DiffRemoved, p.Surface)
	// The current-hunk gutter marker and the targeted collapsed-context gap
	// (#2494) fall back to the theme's own accent: a foreground mark, so the
	// accent's legibility guarantees carry over without a declared slot.
	p.DiffMarker = slot(t.UI.DiffMarker, firstNonEmpty(t.UI.Accent, def.UI.Accent))
	// The pane-number badge (#2496) is an inverted pill, so it needs a
	// background of its own on both sides of the focus split: the accent for
	// the focused pane, and for the rest a tone mixed from the theme's own
	// foreground over its surface — muted, but a filled chip rather than the
	// dim border colour the digits used to disappear into. The two
	// foregrounds are picked for contrast against the pill they sit on
	// (paneBadgeFg), never hard-coded, so light and dark palettes both land
	// on a legible digit without declaring a slot.
	p.PaneBadge = slot(t.UI.PaneBadge, firstNonEmpty(t.UI.Accent, def.UI.Accent))
	p.PaneBadgeMuted = slotOrMix(t.UI.PaneBadgeMuted, p.Foreground, p.Surface, paneBadgeMutedMix)
	p.PaneBadgeText = paneBadgeFg(t.UI.PaneBadgeText, p.PaneBadge, p)
	p.PaneBadgeMutedText = paneBadgeFg(t.UI.PaneBadgeMutedText, p.PaneBadgeMuted, p)
	// VCS status foregrounds (Roadmap 0320) follow the git workflow (#1868):
	// modified→info blue, added→success green, conflicted→error red. The two
	// remaining roles have no semantic slot of their own, so sparse themes
	// derive them from the ones they do have: untracked takes the hue halfway
	// between error and info — a violet that no other status occupies, so
	// "not in git yet" never reads like a warning or an open file — and
	// deleted a red muted halfway toward the dim border tone.
	p.VCSModified = slot(t.UI.VCSModified, firstNonEmpty(t.UI.Info, def.UI.Info))
	p.VCSAdded = slot(t.UI.VCSAdded, firstNonEmpty(t.UI.Success, def.UI.Success))
	p.VCSConflicted = slot(t.UI.VCSConflicted, firstNonEmpty(t.UI.Error, def.UI.Error))
	p.VCSUntracked = slotOrMix(t.UI.VCSUntracked, p.VCSConflicted, p.VCSModified, 0.5)
	p.VCSDeleted = slotOrMix(t.UI.VCSDeleted,
		p.VCSConflicted, Resolve(firstNonEmpty(t.UI.Border, def.UI.Border)), 0.5)
	// The terminal palette resolves last: entries the theme omits derive from
	// the semantic slots filled in above (#1363).
	p.resolveTerminal(t.Terminal)
	return p
}

const (
	// emphMix is how far an intra-line emphasis background moves from its
	// line background toward the side's semantic hue (#2170).
	emphMix = 0.12
	// emphHeadroom is how much further than its own line background an
	// emphasis background may drift from Surface. The line backgrounds sit
	// in the readability envelope every overlay lives in (contrast_test's
	// overlay cap), so scaling *that* theme's own drift keeps strict themes
	// strict and relaxed themes readable — a changed range stays a
	// background, never a highlighter. The renderer carries the rest of the
	// distinction as bold + underline, which every theme shows equally.
	emphHeadroom = 1.10
)

// emphSlot resolves an intra-line emphasis background: the explicit token, or
// the line background pushed toward hue and then pulled back toward surface
// until it sits inside the line background's own envelope (emphHeadroom).
// Deriving rather
// than hand-tuning keeps every theme — built-in or third-party, light or dark
// — in the same envelope automatically.
func emphSlot(token string, hue, base, surface color.Color) color.Color {
	if token != "" {
		return Resolve(token)
	}
	maxRatio := ContrastRatio(base, surface) * emphHeadroom
	c := Mix(hue, base, emphMix)
	for frac := 1.0; frac > 0.05; frac -= 0.05 {
		cand := Mix(c, surface, frac)
		if ContrastRatio(cand, surface) <= maxRatio {
			return cand
		}
	}
	return base
}

const (
	// paneBadgeMutedMix is how far the unfocused pane-number pill moves from
	// the pane surface toward the theme's foreground. Far enough that the
	// chip is unmistakably filled at a glance (that is the whole point of
	// the badge), near enough that eight quiet panes do not shout.
	paneBadgeMutedMix = 0.42
	// paneBadgeMinContrast is the contrast a pill's own palette foreground
	// must clear before paneBadgeFg settles for black or white.
	paneBadgeMinContrast = 4.5
)

// paneBadgeFg picks the digit colour for a pane-number pill: the explicit
// token, else the first of the theme's own extremes that reads well enough on
// the pill, else whichever of black/white does. Any background has one of
// those two at >= 4.58:1, so even a sparse third-party theme cannot end up
// with an unreadable badge.
func paneBadgeFg(token string, bg color.Color, p *Palette) color.Color {
	if token != "" {
		return Resolve(token)
	}
	for _, c := range []color.Color{p.Background, p.Surface, p.Foreground} {
		if ContrastRatio(c, bg) >= paneBadgeMinContrast {
			return c
		}
	}
	return Readable(bg, color.RGBA{A: 0xff}, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
}

// slotOrMix resolves a slot token, falling back to fg mixed over bg by frac
// when the slot is empty.
func slotOrMix(token string, fg, bg color.Color, frac float64) color.Color {
	if token != "" {
		return Resolve(token)
	}
	return Mix(fg, bg, frac)
}

// Luminance is c's WCAG 2.x relative luminance in [0, 1]
// (https://www.w3.org/TR/WCAG21/#dfn-relative-luminance).
func Luminance(c color.Color) float64 {
	if c == nil {
		return 0
	}
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

// ContrastRatio is the WCAG contrast ratio between a and b, in [1, 21].
func ContrastRatio(a, b color.Color) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Readable picks the option that reads best on bg. Renderers that paint text
// on a semantic colour (a mode badge on its mode colour, #1323) can't know
// whether that colour came out light or dark in the active theme, so they let
// the contrast decide instead of hard-coding a foreground.
func Readable(bg color.Color, options ...color.Color) color.Color {
	var best color.Color
	bestRatio := -1.0
	for _, o := range options {
		if o == nil {
			continue
		}
		if r := ContrastRatio(o, bg); r > bestRatio {
			best, bestRatio = o, r
		}
	}
	return best
}

// Mix returns fg blended over bg: frac of fg, the rest bg. It backs the
// derived diff backgrounds and is exported for renderers needing the same
// tinting (a span emphasis over a line background, say).
func Mix(fg, bg color.Color, frac float64) color.Color {
	if fg == nil || bg == nil {
		if fg != nil {
			return fg
		}
		return bg
	}
	fr, fg16, fb, _ := fg.RGBA()
	br, bg16, bb, _ := bg.RGBA()
	mix := func(f, b uint32) uint8 {
		return uint8((float64(f)*frac + float64(b)*(1-frac)) / 257)
	}
	return color.RGBA{R: mix(fr, br), G: mix(fg16, bg16), B: mix(fb, bb), A: 0xff}
}
