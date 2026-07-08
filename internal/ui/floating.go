// Package ui provides the reusable floating shell: a centered, content-sized
// box composited on top of the active layout that can host any content. The
// shell owns chrome (border + padding + title), content sizing, scroll-on-
// overflow, key-swallow, and dismissal; it knows nothing about what it renders.
// Help, modals, and plugin popups are all just Content plugged into it. Pure
// string compositing of the box onto the base canvas lives in internal/overlay.
package ui

import (
	"image/color"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// Content is the body a Floating shell hosts. The shell owns all chrome,
// sizing, scrolling and dismissal; content only supplies a heading and renders
// its body laid out to a width budget. Help, confirm dialogs, and plugin popups
// implement this — the shell never knows which.
type Content interface {
	// Title is the heading shown at the top of the shell.
	Title() string
	// Render returns the body laid out to fit within width columns. The shell
	// scrolls the result when it overflows the available height.
	Render(width int) string
}

// ModelContent adapts any view-only model (a View() string provider, e.g. a
// plugin.Pane built model) into Content, ignoring the width budget. It is the
// seam that lets a plugin present its pane as a floating modal for free.
type ModelContent struct {
	Heading string
	Body    func() string
}

// Title implements Content.
func (m ModelContent) Title() string { return m.Heading }

// Render implements Content; the width budget is ignored since a view-only
// model renders itself.
func (m ModelContent) Render(int) string {
	if m.Body == nil {
		return ""
	}
	return m.Body()
}

// Config tunes a Floating shell. Zero values select built-in defaults, so the
// empty Config is valid.
type Config struct {
	Margin        int      // gap to each terminal edge; <=0 selects the default
	MaxWidthFrac  float64  // pane outer width clamp as a fraction of the terminal; 0 = no clamp
	MaxHeightFrac float64  // pane outer height clamp as a fraction of the terminal; 0 = no clamp
	DismissKeys   []string // keys that close the shell; empty selects {"esc"}
	Accent        string   // border/title colour override; "" follows the theme
}

// Floating is the stateful shell. It hosts a Content child, owns the box chrome,
// open/close state, content sizing, scroll-on-overflow, and key-swallow with a
// configurable dismiss set. v1 is single-level: one floating pane at a time,
// owned by the root model.
type Floating struct {
	cfg     Config
	dismiss map[string]bool
	pal     *theme.Palette // active theme (Roadmap 0110); nil = default

	content Content
	open    bool
	width   int // terminal width
	height  int // terminal height
	scroll  scroller
}

// New returns a closed Floating configured by cfg.
func New(cfg Config) *Floating {
	keys := cfg.DismissKeys
	if len(keys) == 0 {
		keys = []string{"esc"}
	}
	dismiss := make(map[string]bool, len(keys))
	for _, k := range keys {
		dismiss[k] = true
	}
	return &Floating{cfg: cfg, dismiss: dismiss, scroll: newScroller(0, 0)}
}

// SetPalette threads the active theme palette in (Roadmap 0110); the border
// accent and dim hint colours derive from its ui slots.
func (f *Floating) SetPalette(p *theme.Palette) {
	f.pal = p
	f.scroll.dim = f.theme().Border
}

// theme returns the active palette, defaulting when none was threaded in.
func (f *Floating) theme() *theme.Palette {
	if f.pal != nil {
		return f.pal
	}
	return theme.DefaultPalette()
}

// IsOpen reports whether the shell is currently shown.
func (f *Floating) IsOpen() bool { return f.open }

// SetContent installs the child whose body the shell renders and relays out.
func (f *Floating) SetContent(c Content) {
	f.content = c
	f.relayout()
}

// Open shows the shell, resetting scroll to the top of the current content.
func (f *Floating) Open() {
	f.open = true
	f.relayout()
}

// Close hides the shell.
func (f *Floating) Close() { f.open = false }

// SetSize records the terminal size and recomputes the layout.
func (f *Floating) SetSize(width, height int) {
	f.width, f.height = width, height
	f.relayout()
}

// Update handles shell keys while open: a dismiss key closes it, every other
// key is a scroll key. It reports whether the message was consumed, so the host
// can suppress all other routing while the shell is open.
func (f *Floating) Update(msg tea.Msg) bool {
	if !f.open {
		return false
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.SetSize(msg.Width, msg.Height)
		return true
	case tea.KeyMsg:
		if f.dismiss[msg.String()] {
			f.Close()
			return true
		}
		f.scroll.Update(msg)
		return true
	}
	return true
}

// relayout sizes the pane to its content — clamped to the terminal minus the
// margin and the optional max fractions — and feeds the laid-out body to the
// scroller. Safe to call before a size or content is known (it no-ops).
func (f *Floating) relayout() {
	if f.content == nil || f.width <= 0 || f.height <= 0 {
		return
	}
	cw, ch := budget(f.width, f.height, f.margin(), f.cfg.MaxWidthFrac, f.cfg.MaxHeightFrac)
	body := f.content.Render(cw)
	bodyW := lipgloss.Width(body)
	viewH := lipgloss.Height(body)
	if viewH > ch {
		viewH = ch // content overflows -> scroll within the budget
	}
	f.scroll.SetSize(bodyW, viewH)
	f.scroll.SetContent(body)
}

// View renders the floating box, sized to its content, or empty when closed or
// before a size is known. The caller composites it centered via overlay.Center.
func (f *Floating) View() string {
	if !f.open || f.width <= 0 || f.content == nil {
		return ""
	}
	var accent color.Color = f.theme().BorderFocus
	if f.cfg.Accent != "" {
		accent = lipgloss.Color(f.cfg.Accent)
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Underline(true)
	hintStyle := lipgloss.NewStyle().Foreground(f.theme().Border)
	title := titleStyle.Render(f.content.Title()) + hintStyle.Render("   ("+f.hint()+" to close)")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(padV, padH)

	// A blank spacer row separates the heading from the body (accounted for in
	// budget via titleRows).
	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", f.scroll.View())
	return box.Render(inner)
}

// hint renders the dismiss-key hint, e.g. "esc/?/q", in a stable order.
func (f *Floating) hint() string {
	keys := f.cfg.DismissKeys
	if len(keys) == 0 {
		keys = []string{"esc"}
	}
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += "/"
		}
		out += k
	}
	return out
}

func (f *Floating) margin() int {
	if f.cfg.Margin <= 0 {
		return defaultMargin
	}
	return f.cfg.Margin
}
