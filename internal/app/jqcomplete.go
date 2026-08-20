package app

// jqcomplete.go is the completion popup over the jq playground's query line
// (#1979): object keys read off the parsed snapshot after a `.`, jq builtins
// on a bare identifier — the candidate logic is internal/jqplay's Complete,
// this file owns the popup state, its keys and its rendering.
//
// The popup follows the editor completion pattern deliberately (keys in
// keys_insert.go's completionKey, look in lsp_state.go's CompletionView):
// down/ctrl+n and up/ctrl+p step, pgup/pgdown page, enter or a plain tab
// accepts, esc dismisses, and typing re-filters — the muscle memory is the
// same one popup to the other. While it is open it shadows the query line's
// own enter/tab/esc/↑/↓, which come back the moment it closes.

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
	"ike/internal/overlay"
	"ike/internal/ui"
)

// jqCompMaxRows caps the popup's visible rows, matching the editor's
// completion popup.
const jqCompMaxRows = 8

// jqCompHint is the popup's accept-affordance row, verbatim the editor
// completion popup's (#308): the two lists must read as the same control.
const jqCompHint = "↹/⏎ accept · esc close"

// jqQueryPrefixCells is the query row's fixed label width ("> jq: ") the
// popup anchor offsets by; jqQueryRow keeps both prefixes at this width.
const jqQueryPrefixCells = 6

// jqCompState is the open completion popup: the candidates, the selection
// and the rune index of the partial an accept replaces (also the popup's
// anchor column).
type jqCompState struct {
	items []jqplay.Candidate
	sel   int
	start int
}

// jqCompletionKey routes a key while the popup is open, reporting whether it
// was consumed. Navigation, accept and dismiss mirror the editor's
// completionKey; typing and backspace fall through to the query line so the
// list re-filters on the changed program.
func (m *Model) jqCompletionKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	s := m.jqPlay
	c := s.comp
	switch msg.String() {
	case "down", "ctrl+n":
		c.sel = ui.StepIndex(c.sel, 1, len(c.items))
		return true, nil
	case "up", "ctrl+p":
		c.sel = ui.StepIndex(c.sel, -1, len(c.items))
		return true, nil
	case "pgdown":
		c.sel = ui.PageIndex(c.sel, 1, len(c.items), jqCompMaxRows)
		return true, nil
	case "pgup":
		c.sel = ui.PageIndex(c.sel, -1, len(c.items), jqCompMaxRows)
		return true, nil
	case "enter", "tab":
		return true, m.acceptJQCompletion()
	case "esc":
		s.comp = nil
		return true, nil
	}
	return false, nil
}

// acceptJQCompletion writes the selected candidate over the partial and
// re-evaluates — the program changed like any typed edit.
func (m *Model) acceptJQCompletion() tea.Cmd {
	s := m.jqPlay
	c := s.comp
	s.comp = nil
	if c == nil || len(c.items) == 0 {
		return nil
	}
	it := c.items[ui.ClampIndex(c.sel, len(c.items))]
	r := []rune(s.program)
	start, pos := ui.ClampIndex(c.start, len(r)+1), ui.ClampIndex(s.pos, len(r)+1)
	if start > pos {
		start = pos
	}
	s.program = string(r[:start]) + it.Insert + string(r[pos:])
	s.pos = start + len([]rune(it.Insert))
	s.histIdx = -1
	return m.scheduleJQEval()
}

// refreshJQCompletion recomputes the popup after a program change. An open
// popup follows the new partial and closes when nothing matches (the
// editor's after-typing rule); a closed one opens only when the change typed
// a trigger rune — a `.` or an identifier character — or on the explicit
// ctrl+space request, which also opens the full builtin list on an empty
// partial. The selection resets to the top: the list under it changed.
func (m *Model) refreshJQCompletion(typed string, manual bool) {
	s := m.jqPlay
	if s.comp == nil && !manual && !jqCompletionTrigger(typed) {
		return
	}
	items, start := jqplay.Complete(s.program, s.pos, s.input, manual)
	if len(items) == 0 {
		s.comp = nil
		return
	}
	s.comp = &jqCompState{items: items, start: start}
}

// jqCompletionTrigger reports whether the typed text is a rune that opens
// the popup on its own: the `.` of a path, or an identifier rune starting a
// builtin name.
func jqCompletionTrigger(typed string) bool {
	r := []rune(typed)
	if len(r) != 1 {
		return false
	}
	c := r[0]
	return c == '.' || c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// jqCompView renders the popup box in the editor completion popup's shape: a
// window of rows around the selection, the accept-hint row, the selected
// builtin's one-line doc dimmed below, the shared rounded overlay frame.
func (m Model) jqCompView() string {
	c := m.jqPlay.comp
	items := c.items
	if len(items) == 0 {
		return ""
	}
	sel := ui.ClampIndex(c.sel, len(items))
	start := 0
	if sel >= jqCompMaxRows {
		start = sel - jqCompMaxRows + 1
	}
	end := start + jqCompMaxRows
	if end > len(items) {
		end = len(items)
	}

	width := lipgloss.Width(jqCompHint)
	for _, it := range items[start:end] {
		if l := lipgloss.Width(jqCompLabel(it)); l > width {
			width = l
		}
	}
	if width > 40 {
		width = 40
	}
	pal := m.pal()
	normal := lipgloss.NewStyle().Background(pal.Panel).Foreground(pal.Foreground)
	selected := lipgloss.NewStyle().Background(pal.Primary).Foreground(pal.SelectionText)
	dim := lipgloss.NewStyle().Background(pal.Panel).Foreground(pal.Border)
	var rows []string
	for i := start; i < end; i++ {
		st := normal
		if i == sel {
			st = selected
		}
		rows = append(rows, st.Width(width).Render(ansi.Truncate(jqCompLabel(items[i]), width, "…")))
	}
	rows = append(rows, dim.Width(width).Render(ansi.Truncate(jqCompHint, width, "…")))
	if doc := items[sel].Doc; doc != "" {
		rows = append(rows, dim.Width(width).Render(ansi.Truncate(doc, width, "…")))
	}
	frame := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		BorderBackground(pal.Panel).
		Background(pal.Panel).
		Foreground(pal.Foreground)
	return frame.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// jqCompLabel is one row's text: the label plus the detail trailer (a key's
// value type, a builtin's arities).
func jqCompLabel(it jqplay.Candidate) string {
	if it.Detail != "" {
		return it.Label + " " + it.Detail
	}
	return it.Label
}

// compositeJQCompletion overlays the popup at the partial's cell on the
// query row — the compositeLSPPopups analogue for the inline playground:
// only the mode knows the anchor, only the app the absolute geometry. The
// box may overflow the hosting pane like the editor popups (#316), shifting
// left at the screen edge and flipping above the query line when it would
// cross the bottom.
func (m Model) compositeJQCompletion(base string) string {
	s := m.jqPlay
	if s == nil || s.comp == nil || s.bufFocus || !m.jqPlayFocused() {
		return base
	}
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return base
	}
	view := m.jqCompView()
	if view == "" {
		return base
	}
	w, h := lipgloss.Width(view), lipgloss.Height(view)
	queryY := r.Y + paneContentY // breadcrumbs are suppressed while the mode owns the pane
	x := r.X + paneContentX + jqQueryPrefixCells + m.jqCompAnchorCol(r.W-paneChromeW)
	y := queryY + 1
	if maxX := m.width - w; x > maxX {
		x = maxX
	}
	if x < 0 {
		x = 0
	}
	if y+h > m.height {
		y = queryY - h
	}
	if y < 0 {
		y = 0
	}
	return overlay.Place(base, view, x, y, m.width, m.height)
}

// jqCompAnchorCol is the partial's column within the rendered query row,
// mirroring jqHighlighted's cursor-window math (the window start and its
// leading ellipsis cell).
func (m Model) jqCompAnchorCol(paneW int) int {
	s := m.jqPlay
	avail := paneW - 8
	if avail < 10 {
		avail = 10
	}
	ws := 0
	if s.pos >= avail {
		ws = s.pos - avail + 1
	}
	col := s.comp.start - ws
	if ws > 0 {
		col++
	}
	if col < 0 {
		col = 0
	}
	return col
}
