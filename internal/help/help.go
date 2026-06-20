package help

import (
	"github.com/charmbracelet/lipgloss"
)

// maxColumns caps the cheat sheet at two columns wide regardless of how much
// horizontal room the shell offers.
const maxColumns = 2

// Help is the read-only help content: it snapshots commands and lays them out
// in width-responsive columns. It is a ui.Content provider — the floating shell
// owns the chrome, sizing, scrolling and dismissal; Help owns only the command
// snapshot, grouping, and column layout. It executes nothing.
type Help struct {
	src    CommandSource
	res    BindingResolver
	minCol int // configured minimum column width (0 -> default)

	groups []Group
}

// New returns help content reading commands from src and shortcuts from res
// (res may be nil for title-only rendering). minCol is the configured minimum
// column width; 0 selects the built-in default.
func New(src CommandSource, res BindingResolver, minCol int) *Help {
	return &Help{src: src, res: res, minCol: minCol}
}

// Snapshot re-reads every registered command. It is idempotent: re-snapshotting
// picks up newly registered commands. Call it each time the shell is opened so
// the cheat sheet reflects the current registry.
func (h *Help) Snapshot() {
	h.groups = Snapshot(h.src, h.res)
}

// Title implements ui.Content.
func (h *Help) Title() string { return "HELP — commands & shortcuts" }

// Render implements ui.Content: it lays the snapshotted groups out into at most
// maxColumns columns that fit within width, returning the body for the shell to
// scroll and frame.
func (h *Help) Render(width int) string {
	if width < 1 {
		width = 1
	}
	colW := MinColumnWidth(h.allCells(), h.minCol)
	if colW > width {
		colW = width
	}
	cols := ColumnCount(width, colW)
	if cols > maxColumns {
		cols = maxColumns
	}
	return h.renderBody(colW, cols)
}

// allCells renders every entry across all groups, used to derive a shared
// column width so the columns line up.
func (h *Help) allCells() []string {
	var cells []string
	for _, g := range h.groups {
		for _, e := range g.Entries {
			cells = append(cells, renderEntry(e))
		}
	}
	return cells
}

// renderBody renders every group as a heading followed by its entries packed
// column-major into at most cols columns of width colW.
func (h *Help) renderBody(colW, cols int) string {
	// Headings are set apart by weight and an underline, not colour alone, so the
	// grouping reads even on monochrome terminals.
	headingStyle := lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("69"))
	var blocks []string
	for _, g := range h.groups {
		cells := make([]string, len(g.Entries))
		for i, e := range g.Entries {
			cells[i] = renderEntry(e)
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

// renderEntry formats one command row: "title … shortcut", or just the title
// when unbound.
func renderEntry(e Entry) string {
	if e.Shortcut == "" {
		return e.Title
	}
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("215"))
	return e.Title + "  " + keyStyle.Render(e.Shortcut)
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
