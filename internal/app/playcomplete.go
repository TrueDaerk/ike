package app

// playcomplete.go is the completion popup over the playground's query line
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

// playCompMaxRows caps the popup's visible rows, matching the editor's
// completion popup.
const playCompMaxRows = 8

// playCompHint is the popup's accept-affordance row, verbatim the editor
// completion popup's (#308): the two lists must read as the same control.
const playCompHint = "↹/⏎ accept · esc close"

// playQueryPrefixCells is the query row's fixed label width ("> jq: ") the
// popup anchor offsets by; playQueryRow keeps both prefixes at this width.
const playQueryPrefixCells = 6

// playCompState is the open completion popup: the candidates, the selection
// and the rune index of the partial an accept replaces (also the popup's
// anchor column).
type playCompState struct {
	items []jqplay.Candidate
	sel   int
	start int
}

// playCompletionKey routes a key while the popup is open, reporting whether it
// was consumed. Navigation, accept and dismiss mirror the editor's
// completionKey; typing and backspace fall through to the query line so the
// list re-filters on the changed program.
func (m *Model) playCompletionKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	s := m.play
	c := s.comp
	switch msg.String() {
	case "down", "ctrl+n":
		c.sel = ui.StepIndex(c.sel, 1, len(c.items))
		return true, nil
	case "up", "ctrl+p":
		c.sel = ui.StepIndex(c.sel, -1, len(c.items))
		return true, nil
	case "pgdown":
		c.sel = ui.PageIndex(c.sel, 1, len(c.items), playCompMaxRows)
		return true, nil
	case "pgup":
		c.sel = ui.PageIndex(c.sel, -1, len(c.items), playCompMaxRows)
		return true, nil
	case "enter", "tab":
		return true, m.acceptPlayCompletion()
	case "esc":
		s.comp = nil
		return true, nil
	}
	return false, nil
}

// acceptPlayCompletion writes the selected candidate over the partial and
// re-evaluates — the program changed like any typed edit.
func (m *Model) acceptPlayCompletion() tea.Cmd {
	s := m.play
	c := s.comp
	s.comp = nil
	if c == nil || len(c.items) == 0 {
		return nil
	}
	it := c.items[ui.ClampIndex(c.sel, len(c.items))]
	r := s.program.Runes()
	start, pos := ui.ClampIndex(c.start, len(r)+1), ui.ClampIndex(s.program.Cur, len(r)+1)
	if start > pos {
		start = pos
	}
	s.program.Text = string(r[:start]) + it.Insert + string(r[pos:])
	s.program.Cur = start + len([]rune(it.Insert))
	s.histIdx = -1
	return m.schedulePlayEval()
}

// refreshPlayCompletion recomputes the popup after a program change. An open
// popup follows the new partial and closes when nothing matches (the
// editor's after-typing rule); a closed one opens only when the change typed
// a trigger rune — a `.` or an identifier character — or on the explicit
// ctrl+space request, which also opens the full builtin list on an empty
// partial. The selection resets to the top: the list under it changed.
func (m *Model) refreshPlayCompletion(typed string, manual bool) {
	s := m.play
	if s.comp == nil && !manual && !playCompletionTrigger(typed) {
		return
	}
	items, start := jqplay.Complete(s.program.Text, s.program.Cur, s.input, manual)
	if len(items) == 0 {
		s.comp = nil
		return
	}
	s.comp = &playCompState{items: items, start: start}
}

// playCompletionTrigger reports whether the typed text is a rune that opens
// the popup on its own: the `.` of a path, or an identifier rune starting a
// builtin name.
func playCompletionTrigger(typed string) bool {
	r := []rune(typed)
	if len(r) != 1 {
		return false
	}
	c := r[0]
	return c == '.' || c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// playCompView renders the popup box in the editor completion popup's shape: a
// window of rows around the selection, the accept-hint row, the selected
// builtin's one-line doc dimmed below, the shared rounded overlay frame.
func (m Model) playCompView() string {
	c := m.play.comp
	items := c.items
	if len(items) == 0 {
		return ""
	}
	sel := ui.ClampIndex(c.sel, len(items))
	start := 0
	if sel >= playCompMaxRows {
		start = sel - playCompMaxRows + 1
	}
	end := start + playCompMaxRows
	if end > len(items) {
		end = len(items)
	}

	width := lipgloss.Width(playCompHint)
	for _, it := range items[start:end] {
		if l := lipgloss.Width(playCompLabel(it)); l > width {
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
		rows = append(rows, st.Width(width).Render(ansi.Truncate(playCompLabel(items[i]), width, "…")))
	}
	rows = append(rows, dim.Width(width).Render(ansi.Truncate(playCompHint, width, "…")))
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

// playCompLabel is one row's text: the label plus the detail trailer (a key's
// value type, a builtin's arities).
func playCompLabel(it jqplay.Candidate) string {
	if it.Detail != "" {
		return it.Label + " " + it.Detail
	}
	return it.Label
}

// compositePlayCompletion overlays the popup at the partial's cell on the
// query row — the compositeLSPPopups analogue for the inline playground:
// only the mode knows the anchor, only the app the absolute geometry. The
// box may overflow the hosting pane like the editor popups (#316), shifting
// left at the screen edge and flipping above the query line when it would
// cross the bottom.
func (m Model) compositePlayCompletion(base string) string {
	s := m.play
	if s == nil || s.comp == nil || s.bufFocus || !m.playFocused() {
		return base
	}
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return base
	}
	view := m.playCompView()
	if view == "" {
		return base
	}
	w, h := lipgloss.Width(view), lipgloss.Height(view)
	queryY := r.Y + paneContentY // breadcrumbs are suppressed while the mode owns the pane
	col, row := m.playCompAnchor(r.W - paneChromeW)
	x := r.X + paneContentX + playQueryPrefixCells + col
	y := queryY + row + 1
	if maxX := m.width - w; x > maxX {
		x = maxX
	}
	if x < 0 {
		x = 0
	}
	if y+h > m.height {
		y = queryY + row - h
	}
	if y < 0 {
		y = 0
	}
	return overlay.Place(base, view, x, y, m.width, m.height)
}

// playCompAnchor is where the popup hangs: the partial's column within the
// rendered query row, and that row's index in the query header. The one-line
// view mirrors playHighlighted's cursor-window math (the window start and its
// leading ellipsis cell) on row 0; the multi-line view (#2038) anchors on the
// **cursor's** row instead, so the list still opens under the word it
// completes when the program is several rows deep.
func (m Model) playCompAnchor(paneW int) (col, row int) {
	s := m.play
	avail := m.playQueryWidth(paneW)
	lines, rows, start := m.playQueryWindow(paneW)
	if rows <= 1 {
		ws := 0
		if s.program.Cur >= avail {
			ws = s.program.Cur - avail + 1
		}
		col = s.comp.start - ws
		if ws > 0 {
			col++
		}
		return max(col, 0), 0
	}
	cur, _ := jqplay.RowCol(lines, s.program.Cur)
	col = s.comp.start - lines[cur].Start
	if cur == start && start > 0 {
		col++ // the windowed first row opens on the `…` marker
	}
	return max(col, 0), cur - start
}
