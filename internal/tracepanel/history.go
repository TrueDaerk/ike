package tracepanel

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/agenttrace"
	"ike/internal/theme"
	"ike/internal/ui"
)

// history.go is the pane's session history (#2860): the picker of stored
// sessions ('s', or agent.trace.history) and the read-only view of one of
// them. The host owns the store (internal/agenttrace.Store under
// .ike/agent-trace); the pane asks for the list (HistoryMsg), shows it as a
// ui.LineSearch-filtered list on the shared list building blocks
// (ui.ListNav, ui.ClampWindow, ui.RenderWindow, the list-mouse helpers),
// and asks for one session (ShowHistoryMsg). The host then hands the
// session's tree and path in through SetStored, and the header says
// `history · <date>`; esc or r (LiveMsg) return to the live session.

// HistoryMsg asks the root model for the stored sessions ('s'); the picker
// opens once they arrive (OpenPicker).
type HistoryMsg struct{}

// ShowHistoryMsg asks the root model to show the stored session ID in the
// pane (enter in the picker).
type ShowHistoryMsg struct{ ID string }

// LiveMsg asks the root model to return the pane to the live session (esc
// or r while a stored session is shown).
type LiveMsg struct{}

// HistoryItem is one row of the picker.
type HistoryItem struct {
	ID        string
	StartedAt time.Time
	EndedAt   time.Time
	Ended     bool
	Turns     int
	Files     int
	Prompt    string
	// Live marks the session the trace follows right now.
	Live bool
}

// pickerState is the open picker.
type pickerState struct {
	open   bool
	items  []HistoryItem
	search ui.LineSearch
	// rows are the item indices shown: every item without a query, the
	// search's matches with one. cursor and top index rows.
	rows   []int
	cursor int
	top    int
}

// OpenPicker shows the stored sessions, newest first as handed in, with the
// cursor on the first row. An empty list still opens: the picker says how to
// fill it.
func (m *Model) OpenPicker(items []HistoryItem) {
	p := &m.picker
	p.open = true
	p.items = items
	p.search.Reset()
	p.cursor, p.top = 0, 0
	p.rows = nil
	m.recomputePicker()
}

// ClosePicker hides the picker, keeping the session shown behind it.
func (m *Model) ClosePicker() { m.picker.open = false }

// PickerOpen reports whether the picker is shown.
func (m *Model) PickerOpen() bool { return m.picker.open }

// PickerRows returns the ids of the rows the picker shows (tests).
func (m *Model) PickerRows() []string {
	out := make([]string, 0, len(m.picker.rows))
	for _, i := range m.picker.rows {
		out = append(out, m.picker.items[i].ID)
	}
	return out
}

// PickerCurrent returns the item under the cursor, nil on an empty list.
func (m *Model) PickerCurrent() *HistoryItem {
	p := &m.picker
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return nil
	}
	return &p.items[p.rows[p.cursor]]
}

// recomputePicker re-filters the rows for the query: a smartcase substring
// over the row text (date, prompt, id), keeping the cursor on the nearest
// surviving row.
func (m *Model) recomputePicker() {
	p := &m.picker
	prev := -1
	if p.cursor >= 0 && p.cursor < len(p.rows) {
		prev = p.rows[p.cursor]
	}
	if p.search.Text == "" {
		p.rows = p.rows[:0]
		for i := range p.items {
			p.rows = append(p.rows, i)
		}
	} else {
		p.search.Recompute(len(p.items), func(i int) bool {
			return ui.SmartCaseContains(p.search.Text, m.pickerText(p.items[i]))
		})
		p.rows = append(p.rows[:0], p.search.Matches...)
	}
	p.cursor = 0
	for k, i := range p.rows {
		if i >= prev {
			p.cursor = k
			break
		}
	}
	ui.ClampWindow(&p.cursor, &p.top, len(p.rows), m.pickerHeight())
}

// pickerText is what the filter matches: the date, the prompt and the id.
func (m *Model) pickerText(it HistoryItem) string {
	return it.StartedAt.Local().Format("2006-01-02 15:04") + " " + it.Prompt + " " + it.ID
}

// pickerHeight is the rows the list lays out into: the pane minus the
// header, the filter line and the hint.
func (m *Model) pickerHeight() int {
	h := m.height - 3
	if h < 1 {
		h = 1
	}
	return h
}

// pickerKey handles one key while the picker is open; it always consumes
// the key.
func (m *Model) pickerKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &m.picker
	key := msg.String()
	if p.search.Open {
		handled, changed, action := p.search.Key(msg)
		switch action {
		case ui.SearchCancel:
			m.recomputePicker()
			return nil
		case ui.SearchAccept:
			return nil
		}
		if changed {
			m.recomputePicker()
		}
		if handled {
			return nil
		}
	}
	switch key {
	case "/":
		p.search.Start()
		return nil
	case "esc", "q":
		if p.search.Text != "" {
			p.search.Reset()
			m.recomputePicker()
			return nil
		}
		m.ClosePicker()
		return nil
	case "s":
		m.ClosePicker()
		return nil
	case "enter":
		return m.pickerActivate(p.cursor)
	}
	if ui.ListNav(key, &p.cursor, len(p.rows), m.pickerHeight(), ui.NavFull) {
		ui.ClampWindow(&p.cursor, &p.top, len(p.rows), m.pickerHeight())
	}
	return nil
}

// pickerActivate is enter on row k: the stored session is asked for; the
// live one returns to the live view.
func (m *Model) pickerActivate(k int) tea.Cmd {
	p := &m.picker
	if k < 0 || k >= len(p.rows) {
		return nil
	}
	it := p.items[p.rows[k]]
	m.ClosePicker()
	if it.Live {
		return func() tea.Msg { return LiveMsg{} }
	}
	id := it.ID
	return func() tea.Msg { return ShowHistoryMsg{ID: id} }
}

// pickerClick is the list-pane click gesture: select, double click opens.
func (m *Model) pickerClick(_, y int) tea.Cmd {
	p := &m.picker
	return m.clicks.ClickRow(y, p.top, 2, m.pickerHeight(), len(p.rows), m.now(), &p.cursor, m.pickerActivate)
}

// pickerWheel scrolls the list.
func (m *Model) pickerWheel(delta int) {
	p := &m.picker
	ui.WheelWindow(&p.top, &p.cursor, delta, len(p.rows), m.pickerHeight())
}

// pickerView draws the picker: header, filter line, the row window and the
// hint.
func (m *Model) pickerView(pal *theme.Palette) string {
	p := &m.picker
	faint := lipgloss.NewStyle().Faint(true)
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" sessions")
	header := title + faint.Render(" · "+strconv.Itoa(len(p.items))+" stored")
	second := faint.Render("/ filter")
	if p.search.Active() {
		second = p.search.LineStyled(faint, lipgloss.NewStyle().Foreground(pal.Error))
	}
	empty := faint.Render("(no stored sessions — the trace writes one per followed session; agent.trace.import reads the Claude transcripts)")
	if p.search.Miss() {
		empty = faint.Render("(no session matches)")
	}
	width := max(1, m.width)
	sel := lipgloss.NewStyle().Background(pal.SelectionMuted)
	if m.focused {
		sel = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
	}
	rows := ui.RenderWindow(p.top, m.pickerHeight(), len(p.rows), empty, func(k int) string {
		line := fitCells(m.pickerRow(p.items[p.rows[k]]), width)
		if k == p.cursor {
			return sel.Render(line + strings.Repeat(" ", max(0, width-lipgloss.Width(line))))
		}
		return line
	})
	hint := faint.Render("enter show · / filter · esc close")
	clip := lipgloss.NewStyle().MaxWidth(width)
	return ui.ListPaneView(clip.Render(header), clip.Render(second), rows, clip.Render(hint))
}

// pickerRow formats one item: the live mark, the date, the duration, the
// turns, the files changed and the first prompt.
func (m *Model) pickerRow(it HistoryItem) string {
	mark := "  "
	if it.Live {
		mark = "● "
	}
	date := "unknown date"
	if !it.StartedAt.IsZero() {
		date = it.StartedAt.Local().Format("2006-01-02 15:04")
	}
	dur := "live"
	if !it.Live || it.Ended {
		dur = formatDuration(it.EndedAt.Sub(it.StartedAt))
	}
	turns := strconv.Itoa(it.Turns) + " turn"
	if it.Turns != 1 {
		turns += "s"
	}
	files := strconv.Itoa(it.Files) + " file"
	if it.Files != 1 {
		files += "s"
	}
	prompt := it.Prompt
	if prompt == "" {
		prompt = "(no prompt)"
	}
	return mark + date + "  " + pad(dur, 6) + "  " + pad(turns, 8) + "  " + pad(files, 8) + "  " + prompt
}

func pad(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// formatDuration prints a session's span compactly: "45s", "12m", "1h05m".
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	}
	h := int(d.Hours())
	mins := int(d.Minutes()) % 60
	s := strconv.Itoa(mins)
	if mins < 10 {
		s = "0" + s
	}
	return strconv.Itoa(h) + "h" + s + "m"
}

// SetStored shows a stored session (#2860): the tree and path of a history
// record, read-only. info.History carries the date label the header shows;
// the pane neither follows the tail nor expects reads.
func (m *Model) SetStored(nodes []agenttrace.Node, stops []agenttrace.Stop, info Info) {
	m.Reset()
	info.Ended = true
	m.follow, m.graph.follow = false, false
	m.Set(nodes, info)
	m.SetPath(stops)
	// Open on the first turn: history is read from the start.
	m.tree.SetCursor(0)
	for i := range m.graph.stops {
		if m.graph.stops[i].Selectable() {
			m.graph.sel = m.graph.stops[i].Key
			break
		}
	}
	m.graph.top = 0
	m.readAt, m.changeAt, m.changeAdded = time.Time{}, time.Time{}, 0
}

// Stored reports whether a history record is shown instead of the live
// session.
func (m *Model) Stored() bool { return m.hasSession && m.info.History != "" }

// HistoryLabel is the header's date label of a stored session: the start
// date and time.
func HistoryLabel(start time.Time) string {
	if start.IsZero() {
		return "unknown date"
	}
	return start.Local().Format("2006-01-02 15:04")
}

// rewindLines is the expanded block of a rewind marker (#2860): the
// abandoned branch's stops, greyed, one per line — the prompt, each change
// with its op, the answer — capped to a dozen lines.
func (m *Model) rewindLines(st agenttrace.Stop, width int) []string {
	lines := []string{"abandoned branch · " + st.Detail}
	const maxLines = 12
	for _, b := range st.Branch {
		if len(lines) >= maxLines {
			lines = append(lines, "… "+strconv.Itoa(len(st.Branch)-(maxLines-1))+" more")
			break
		}
		_, glyph := stopStyle(b)
		line := glyph + " " + b.Label
		if b.Kind == agenttrace.StopChange && b.Ref != nil {
			line = glyph + " " + m.display(b.Ref.Path) + " · " + b.Detail
		}
		lines = append(lines, line)
	}
	lines = append(lines, "space collapse · the live path continues to the right")
	for i, l := range lines {
		lines[i] = fitCells(l, max(1, width-2))
	}
	return lines
}
