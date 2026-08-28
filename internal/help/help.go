package help

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
	"ike/internal/version"
)

// maxColumns caps the cheat sheet at two columns wide regardless of how much
// horizontal room the shell offers.
const maxColumns = 2

// colSlack widens each column beyond its widest cell so the pane gets some
// breathing room and the right-aligned shortcuts sit clear of the titles.
const colSlack = 8

// typicalCoverage is the share of entries (in percent) a column is sized to
// show in full. The remaining tail — a handful of unusually verbose titles —
// is truncated rather than allowed to widen every column (#2215).
const typicalCoverage = 90

// minTitleWidth is the shortest truncated title still worth showing. Columns
// too narrow to leave that much room keep the untruncated row and let it
// overflow, since an ellipsis alone tells the user nothing.
const minTitleWidth = 8

// Help is the read-only help content: it snapshots commands and lays them out
// in width-responsive columns. It is a ui.Content provider — the floating shell
// owns the chrome, sizing, scrolling and dismissal; Help owns only the command
// snapshot, grouping, and column layout. It executes nothing.
type Help struct {
	src    CommandSource
	res    BindingResolver
	minCol int            // configured minimum column width (0 -> default)
	pal    *theme.Palette // active theme (Roadmap 0110); nil = default

	ctxID      string  // focused pane context the snapshot was taken for
	ctxGroups  []Group // context-first ordering (#2182): focused scope, global, rest
	flatGroups []Group // the classic flat sheet: global first, then alphabetical
	essentials []Group
	extra      []Group
	filter     string // live typed filter (#271); "" shows everything
	view       view   // which of the three views is showing; tab cycles
}

// view names the three cheat-sheet views the overlay cycles through with tab.
type view int

const (
	// viewEssentials is the curated starter set (#656) — the opening view when
	// no pane context is focused.
	viewEssentials view = iota
	// viewContext is the focused pane's bindings first, then global, then the
	// remaining contexts below (#2182) — the opening view when a context is
	// focused.
	viewContext
	// viewFlat is the classic flat sheet: the focused context's commands plus
	// the global ones, global first (the behaviour before #2182).
	viewFlat
)

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
	h.ctxID = contextID
	h.ctxGroups = h.withExtraLeading(ContextSnapshot(h.src, h.res, contextID))
	h.flatGroups = h.withExtra(Snapshot(h.src, h.res, contextID))
	h.essentials = EssentialsSnapshot(h.src, h.res)
	// With a pane focused the sheet opens on its own bindings (#2182); the
	// curated Essentials set (#656) and the flat dump stay a tab away. Without
	// a focused context there is nothing to lead with, so Essentials opens —
	// degrading to the flat view when nothing curated resolved (stub
	// registries).
	switch {
	case h.hasContextView():
		h.view = viewContext
	case len(h.essentials) > 0:
		h.view = viewEssentials
	default:
		h.view = viewFlat
	}
}

// withExtra appends the caller-supplied non-empty extra groups to a snapshot.
func (h *Help) withExtra(groups []Group) []Group {
	for _, g := range h.extra {
		if len(g.Entries) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// withExtraLeading is withExtra for the context view: an extra group the caller
// flagged Focused *leads* the sheet instead of trailing it. That is how a mode
// owning the keyboard without owning a registry scope — the jq/yq playground
// (#2237) — becomes a context of its own here: its keys are the first thing
// the sheet shows, ahead of the global bindings, exactly like a focused pane's
// registered scope. Unflagged extras keep trailing.
func (h *Help) withExtraLeading(groups []Group) []Group {
	var lead []Group
	for _, g := range h.extra {
		if len(g.Entries) == 0 {
			continue
		}
		if g.Focused {
			lead = append(lead, g)
			continue
		}
		groups = append(groups, g)
	}
	return append(lead, groups...)
}

// hasContextView reports whether a focused-context view exists: a context is
// focused and it actually owns bindings, so leading with it says something.
func (h *Help) hasContextView() bool {
	if h.ctxID == "" {
		return false
	}
	for _, g := range h.ctxGroups {
		if g.Focused && len(g.Entries) > 0 {
			return true
		}
	}
	return false
}

// HandleKey implements ui.KeyHandler: tab cycles the views — the focused
// context view (#2182), the curated Essentials view (#656), and the flat list —
// skipping any view that has nothing to show. The toggle is a no-op while a
// filter is active: the filter already searches the full set, so switching
// views means nothing there.
func (h *Help) HandleKey(key string) bool {
	if key != "tab" {
		return false
	}
	if h.filter == "" {
		h.view = h.nextView()
	}
	return true
}

// nextView is the tab cycle context -> flat -> essentials -> context, with
// unavailable views skipped. It always terminates: viewFlat is always
// available.
func (h *Help) nextView() view {
	order := []view{viewContext, viewFlat, viewEssentials}
	at := 0
	for i, v := range order {
		if v == h.view {
			at = i
		}
	}
	for i := 1; i <= len(order); i++ {
		next := order[(at+i)%len(order)]
		switch next {
		case viewContext:
			if h.hasContextView() {
				return next
			}
		case viewEssentials:
			if len(h.essentials) > 0 {
				return next
			}
		default:
			return next
		}
	}
	return viewFlat
}

// SetExtra appends caller-supplied groups to every snapshot: the honest
// "blocked" section (0081/40) — bindings whose command has no owner yet are
// shown with their dependency, never hidden — and the focused pane's own
// local keys (#1267), which no command registry knows about.
func (h *Help) SetExtra(groups ...Group) { h.extra = groups }

// Title implements ui.Content; an active filter is echoed so the user sees
// what they typed. The unfiltered titles carry the version (#1214) — the help
// overlay is the one screen a user is already looking at when they need to
// quote which build they are on.
func (h *Help) Title() string {
	if h.filter != "" {
		return "HELP — filter: " + h.filter
	}
	switch h.view {
	case viewEssentials:
		return "HELP " + version.Short() + " — essentials"
	case viewContext:
		return "HELP " + version.Short() + " — " + groupTitle(h.ctxID) + " context"
	}
	return "HELP " + version.Short() + " — commands & shortcuts"
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
	cols, colW := h.columnLayout(groups, width)
	body := h.renderBody(groups, colW, cols)
	if footer := h.footer(groups); footer != "" {
		hintStyle := lipgloss.NewStyle().Foreground(h.theme().Border)
		body = lipgloss.JoinVertical(lipgloss.Left, body, "", hintStyle.Render(footer))
	}
	return body
}

// columnLayout derives the column count and column width for the given groups
// within a body budget of width cells. The width the columns aim for is the one
// that shows the *typical* entry in full, not the single longest one: the
// context view (#2182) shows every scope at once, so one verbose command title
// used to blow the column width past half the terminal and collapse the whole
// sheet into a single, endlessly tall column (#2215). Overlong rows are
// truncated by renderEntry instead.
func (h *Help) columnLayout(groups []Group, width int) (cols, colW int) {
	cells := h.allCells(groups)
	natural := MinColumnWidth(cells, h.minCol) + colSlack
	floor := TypicalColumnWidth(cells, h.minCol, typicalCoverage) + colSlack
	return ColumnLayout(width, natural, floor, maxColumns)
}

// footer renders the one-line view/filter legend under the body (#656, #2182).
// It always names the view tab leads to next, so the cycle is discoverable.
func (h *Help) footer(visible []Group) string {
	total := countEntries(h.searchGroups())
	if h.filter != "" {
		return strconv.Itoa(countEntries(visible)) + " of " + strconv.Itoa(total) + " matches · searching all commands"
	}
	next := h.nextView()
	if next == h.view {
		return ""
	}
	head := strconv.Itoa(countEntries(visible)) + " of " + strconv.Itoa(total) + " commands"
	if h.view == viewContext {
		head = groupTitle(h.ctxID) + " bindings first · " + strconv.Itoa(countEntries(visible)) + " commands"
	}
	return head + " — press tab for " + viewName(next, h.ctxID)
}

// viewName is how the footer refers to a view the user can tab to.
func viewName(v view, ctxID string) string {
	switch v {
	case viewEssentials:
		return "essentials"
	case viewContext:
		return "the " + groupTitle(ctxID) + " context"
	}
	return "the full list"
}

// countEntries totals the rows across groups.
func countEntries(groups []Group) int {
	n := 0
	for _, g := range groups {
		n += len(g.Entries)
	}
	return n
}

// visibleGroups picks the current view: the curated Essentials groups by
// default, the full snapshot after a tab toggle. A live filter always searches
// the FULL set — typing means hunting for something specific, so the curated
// subset would only hide the answer. Matching is a case-insensitive substring
// over title and shortcut; empty groups drop out.
func (h *Help) visibleGroups() []Group {
	if h.filter == "" {
		switch h.view {
		case viewEssentials:
			return h.essentials
		case viewContext:
			return h.ctxGroups
		}
		return h.flatGroups
	}
	needle := strings.ToLower(h.filter)
	var out []Group
	for _, g := range h.searchGroups() {
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

// searchGroups is the widest set the sheet knows about — the context view keeps
// every scope, so a filter typed in any view searches all of them.
func (h *Help) searchGroups() []Group {
	if len(h.ctxGroups) >= len(h.flatGroups) {
		return h.ctxGroups
	}
	return h.flatGroups
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
			headingStyle.Render(sectionTitle(g)),
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
// the form used to derive the shared column width. A title too long for the
// column is truncated with an ellipsis so the row stays one line and its
// shortcut stays visible (#2215); the shortcut itself is never cut. Unbound
// commands render title-only.
func (h *Help) renderEntry(e Entry, colW int) string {
	title := e.Title
	if colW > 0 {
		room := colW
		if e.Shortcut != "" {
			room -= lipgloss.Width(e.Shortcut) + minKeyGap
		}
		title = truncateTitle(title, room)
	}
	if e.Shortcut == "" {
		return title
	}
	gap := colW - lipgloss.Width(title) - lipgloss.Width(e.Shortcut)
	if gap < minKeyGap {
		gap = minKeyGap
	}
	keyStyle := lipgloss.NewStyle().Foreground(h.theme().Secondary)
	return title + strings.Repeat(" ", gap) + keyStyle.Render(e.Shortcut)
}

// truncateTitle shortens title to room cells with an ellipsis. A room too tight
// for minTitleWidth leaves the title alone: the row then overflows its column,
// which still says more than a lone "…".
func truncateTitle(title string, room int) string {
	if room < minTitleWidth || lipgloss.Width(title) <= room {
		return title
	}
	return ansi.Truncate(title, room, "…")
}

// sectionTitle is a group's heading: the scope title, marked as the focused
// pane's own section when it leads the context view (#2182) so it is obvious
// which context the sheet is showing first.
func sectionTitle(g Group) string {
	if g.Focused {
		return groupTitle(g.Label) + " — focused pane"
	}
	return groupTitle(g.Label)
}

// groupTitle is the human-facing heading for a scope label — the full
// per-pane context set since #1794.
func groupTitle(label string) string {
	switch label {
	case "global":
		return "Global"
	case "editor":
		return "Editor"
	case "explorer":
		return "Explorer"
	case "palette":
		return "Palette"
	case "diff":
		return "Diff"
	case "terminal":
		return "Terminal"
	case "preview":
		return "Preview"
	case "vcs":
		return "VCS"
	case "debug":
		return "Debug"
	case "problems":
		return "Problems"
	case "structure":
		return "Structure"
	case "dom":
		return "DOM Inspector"
	case "xdoctor":
		return "Xdebug Doctor"
	case "scratch":
		return "Scratch Files"
	case "usages":
		return "Usages"
	case "http":
		return "HTTP"
	case "breakpoints":
		return "Breakpoints"
	case "tests":
		return "Test Results"
	case "issues":
		return "GitHub Issues"
	case "archive":
		return "Archive"
	case "data":
		return "Data"
	default:
		return label
	}
}
