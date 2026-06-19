package help

import (
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

// Help is the read-only overlay model. It snapshots commands on open, lays them
// out in width-responsive columns, and scrolls vertically when the content
// overflows. It executes nothing: the only thing it emits is its own dismissal,
// signalled through Open().
type Help struct {
	src    CommandSource
	res    BindingResolver
	minCol int // configured minimum column width (0 -> default)

	open   bool
	ctxID  string // pane context captured at open time
	groups []Group
	width  int
	height int
	scroll scroller
}

// New returns a closed overlay reading commands from src and shortcuts from res
// (res may be nil for title-only rendering). minCol is the configured minimum
// column width; 0 selects the built-in default.
func New(src CommandSource, res BindingResolver, minCol int) *Help {
	return &Help{src: src, res: res, minCol: minCol, scroll: newScroller(0, 0)}
}

// IsOpen reports whether the overlay is currently shown.
func (h *Help) IsOpen() bool { return h.open }

// Open snapshots the registry for pane context ctxID and shows the overlay. It
// is idempotent: re-opening re-snapshots so newly registered commands appear.
func (h *Help) Open(ctxID string) {
	h.open = true
	h.ctxID = ctxID
	h.groups = Snapshot(h.src, h.res, ctxID)
	h.relayout()
}

// Close hides the overlay.
func (h *Help) Close() { h.open = false }

// SetSize records the available terminal size and recomputes the layout.
func (h *Help) SetSize(width, height int) {
	h.width, h.height = width, height
	h.relayout()
}

// Update handles overlay keys while open: esc/?/q dismiss, everything else is a
// scroll key. It returns whether the message was consumed by the overlay, so
// the root model can suppress further routing.
func (h *Help) Update(msg tea.Msg) bool {
	if !h.open {
		return false
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.SetSize(msg.Width, msg.Height)
		return true
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "?", "q":
			h.Close()
			return true
		default:
			h.scroll.Update(msg)
			return true
		}
	}
	return true
}

// Overlay chrome and floating-pane bounds.
const (
	// maxColumns caps the cheat sheet at two columns wide regardless of how much
	// horizontal room the terminal offers.
	maxColumns = 2
	// margin is the minimum gap kept between the floating pane and each terminal
	// edge, so the pane never bleeds to the very border.
	margin = 2
	// frameH/frameV are the box chrome: border (2) + padding (2+2 horizontal,
	// 1+1 vertical).
	frameH = 6
	frameV = 4
)

// relayout sizes the floating pane to its content — at most maxColumns wide and
// never larger than the terminal minus a margin — and feeds the laid-out body
// to the scroller. Safe to call before a size is known (it no-ops).
func (h *Help) relayout() {
	if h.width <= 0 || h.height <= 0 {
		return
	}
	// Budget for the content area, reserving the margin, box chrome, and the
	// title line.
	availW := h.width - 2*margin - frameH
	availH := h.height - 2*margin - frameV - 1
	if availW < 1 {
		availW = 1
	}
	if availH < 1 {
		availH = 1
	}

	colW := MinColumnWidth(h.allCells(), h.minCol)
	if colW > availW {
		colW = availW
	}
	cols := ColumnCount(availW, colW)
	if cols > maxColumns {
		cols = maxColumns
	}

	body := h.renderBody(colW, cols)
	bodyW := lipgloss.Width(body)
	viewH := lipgloss.Height(body)
	if viewH > availH {
		viewH = availH // content overflows -> scroll within the budget
	}

	h.scroll.SetSize(bodyW, viewH)
	h.scroll.SetContent(body)
}

// allCells renders every entry across all groups, used to derive a shared
// column width so the floating pane's columns line up.
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
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

// View renders the floating pane, sized to its content, or empty when closed.
// The caller composites it centered on top of the active layout.
func (h *Help) View() string {
	if !h.open || h.width <= 0 {
		return ""
	}
	titleStyle := lipgloss.NewStyle().Bold(true)
	title := titleStyle.Render("HELP — commands & shortcuts") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("   (esc/?/q to close)")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("69")).
		Padding(1, 3)

	content := lipgloss.JoinVertical(lipgloss.Left, title, h.scroll.View())
	return box.Render(content)
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
