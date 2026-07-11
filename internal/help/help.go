package help

import (
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// maxColumns caps the cheat sheet at two columns wide regardless of how much
// horizontal room the shell offers.
const maxColumns = 2

// colSlack widens each column beyond its widest cell so the pane gets some
// breathing room and the right-aligned shortcuts sit clear of the titles.
const colSlack = 8

// Help is the read-only help content: it snapshots commands and lays them out
// in width-responsive columns. It is a ui.Content provider — the floating shell
// owns the chrome, sizing, scrolling and dismissal; Help owns only the command
// snapshot, grouping, and column layout. It executes nothing.
type Help struct {
	src    CommandSource
	res    BindingResolver
	minCol int            // configured minimum column width (0 -> default)
	pal    *theme.Palette // active theme (Roadmap 0110); nil = default

	groups []Group
	extra  Group
	filter string // live typed filter (#271); "" shows everything
}

// SetFilter installs the live filter typed into the floating shell (#271);
// Filter reports it. Help implements ui.Filterable through this pair.
func (h *Help) SetFilter(s string) { h.filter = s }

// Filter implements the ui.Filterable read side.
func (h *Help) Filter() string { return h.filter }

// SetPalette threads the active theme palette in (Roadmap 0110); headings and
// shortcut keys derive their colours from its ui slots.
func (h *Help) SetPalette(p *theme.Palette) { h.pal = p }

// theme returns the active palette, defaulting when none was threaded in.
func (h *Help) theme() *theme.Palette {
	if h.pal != nil {
		return h.pal
	}
	return theme.DefaultPalette()
}

// New returns help content reading commands from src and shortcuts from res
// (res may be nil for title-only rendering). minCol is the configured minimum
// column width; 0 selects the built-in default.
func New(src CommandSource, res BindingResolver, minCol int) *Help {
	return &Help{src: src, res: res, minCol: minCol}
}

// Snapshot re-reads the registered commands that apply to contextID (global
// ones plus that context's own; empty lists every scope). It is idempotent:
// re-snapshotting picks up newly registered commands. Call it each time the
// shell is opened so the cheat sheet reflects the current registry and focus.
func (h *Help) Snapshot(contextID string) {
	h.groups = Snapshot(h.src, h.res, contextID)
	if len(h.extra.Entries) > 0 {
		h.groups = append(h.groups, h.extra)
	}
}

// SetExtra appends one caller-supplied group to every snapshot — the honest
// "blocked" section (0081/40): bindings whose command has no owner yet are
// shown with their dependency, never hidden.
func (h *Help) SetExtra(g Group) { h.extra = g }

// Title implements ui.Content; an active filter is echoed so the user sees
// what they typed.
func (h *Help) Title() string {
	if h.filter != "" {
		return "HELP — filter: " + h.filter
	}
	return "HELP — commands & shortcuts"
}

// Render implements ui.Content: it lays the snapshotted groups out into at most
// maxColumns columns that fit within width, returning the body for the shell to
// scroll and frame.
func (h *Help) Render(width int) string {
	if width < 1 {
		width = 1
	}
	groups := h.visibleGroups()
	if len(groups) == 0 && h.filter != "" {
		return "no matches for \"" + h.filter + "\"  (backspace edits, esc clears)"
	}
	colW := MinColumnWidth(h.allCells(groups), h.minCol) + colSlack
	if colW > width {
		colW = width
	}
	cols := ColumnCount(width, colW)
	if cols > maxColumns {
		cols = maxColumns
	}
	return h.renderBody(groups, colW, cols)
}

// visibleGroups applies the live filter: a case-insensitive substring match
// over title and shortcut keeps an entry; empty groups drop out.
func (h *Help) visibleGroups() []Group {
	if h.filter == "" {
		return h.groups
	}
	needle := strings.ToLower(h.filter)
	var out []Group
	for _, g := range h.groups {
		kept := Group{Label: g.Label}
		for _, e := range g.Entries {
			if strings.Contains(strings.ToLower(e.Title), needle) ||
				strings.Contains(strings.ToLower(e.Shortcut), needle) {
				kept.Entries = append(kept.Entries, e)
			}
		}
		if len(kept.Entries) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

// allCells renders every entry across the given groups at its natural width,
// used to derive a shared column width so the columns line up.
func (h *Help) allCells(groups []Group) []string {
	var cells []string
	for _, g := range groups {
		for _, e := range g.Entries {
			cells = append(cells, h.renderEntry(e, 0))
		}
	}
	return cells
}

// renderBody renders every group as a heading followed by its entries packed
// column-major into at most cols columns of width colW.
func (h *Help) renderBody(groups []Group, colW, cols int) string {
	// Headings are set apart by weight and an underline, not colour alone, so the
	// grouping reads even on monochrome terminals.
	headingStyle := lipgloss.NewStyle().Bold(true).Underline(true).Foreground(h.theme().BorderFocus)
	var blocks []string
	for _, g := range groups {
		cells := make([]string, len(g.Entries))
		for i, e := range g.Entries {
			cells[i] = h.renderEntry(e, colW)
		}
		packed := Pack(cells, cols)
		block := lipgloss.JoinVertical(
			lipgloss.Left,
			headingStyle.Render(groupTitle(g.Label)),
			renderColumns(packed, colW),
		)
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return "no commands registered"
	}
	// Separate groups (Global, Editor, Explorer, …) with a blank line so the
	// sections read as distinct clusters rather than one continuous list.
	spaced := make([]string, 0, len(blocks)*2-1)
	for i, b := range blocks {
		if i > 0 {
			spaced = append(spaced, "")
		}
		spaced = append(spaced, b)
	}
	return lipgloss.JoinVertical(lipgloss.Left, spaced...)
}

// minKeyGap is the smallest run of spaces kept between a title and its
// shortcut, so the two never touch even in a clamped column.
const minKeyGap = 2

// renderEntry formats one command row: the title left-aligned and the shortcut
// pushed to the right edge of a colW-wide cell so the keys line up as their own
// column. colW <= 0 renders at natural width (title, minimum gap, shortcut) —
// the form used to derive the shared column width. Unbound commands render
// title-only.
func (h *Help) renderEntry(e Entry, colW int) string {
	if e.Shortcut == "" {
		return e.Title
	}
	gap := colW - lipgloss.Width(e.Title) - lipgloss.Width(e.Shortcut)
	if gap < minKeyGap {
		gap = minKeyGap
	}
	keyStyle := lipgloss.NewStyle().Foreground(h.theme().Secondary)
	return e.Title + strings.Repeat(" ", gap) + keyStyle.Render(e.Shortcut)
}

// groupTitle is the human-facing heading for a scope label.
func groupTitle(label string) string {
	switch label {
	case "global":
		return "Global"
	case "editor":
		return "Editor"
	case "explorer":
		return "Explorer"
	default:
		return label
	}
}
