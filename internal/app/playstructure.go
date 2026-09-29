package app

// playstructure.go is the playground's structure strip (#2793): a narrow
// column on the right edge of the result listing the result's depth-1 nodes —
// the top-level keys of an object, the indices of an array, the values of a
// stream (jqplay.Result.Outline) — with the part of the result on screen
// highlighted. A click on an entry, or enter on it with the strip focused,
// jumps the result caret to that node. Large results are otherwise navigated
// by scrolling and search; the strip makes orientation instant.
//
// The strip is a session toggle (playground.structure, default ctrl+alt+g),
// shared by all three dialects. It hides on its own below playStripMinPane
// cells and when the result has nothing to list (a scalar, raw or compact
// output, xmq notation) — the result then gets the whole width back.

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
	"ike/internal/theme"
	"ike/internal/ui"
)

// TogglePlayStructureMsg is playground.structure: show and focus the strip,
// focus it when it is up, hide it when it already has the keyboard — the
// JetBrains tool-window toggle.
type TogglePlayStructureMsg struct{}

const (
	// playStripMaxW bounds the strip's width in cells, separator included.
	playStripMaxW = 16
	// playStripMinW is the narrowest strip: `│ [0]` plus a spare cell.
	playStripMinW = 6
	// playStripMinPane is the pane interior width below which the strip
	// hides: the result keeps at least ~44 cells of text beside it.
	playStripMinPane = 60
)

// setOutline installs the depth-1 nodes of a freshly installed result and the
// strip width their labels ask for. The strip's selection and window restart:
// they indexed the previous result's list.
func (s *playState) setOutline(items []jqplay.OutlineItem) {
	s.outline, s.outlineW = items, 0
	for _, it := range items {
		if w := ansi.StringWidth(it.Label); w > s.outlineW {
			s.outlineW = w
			if w >= playStripMaxW {
				break
			}
		}
	}
	s.stripSel, s.stripTop = 0, 0
	if s.stripFocus && len(items) == 0 {
		s.setBufFocus(true) // nothing left to navigate: the result takes the keys
	}
}

// playStripW is the strip's width on a result pane width cells wide, 0 when it
// is not shown: toggled off, nothing to list, or the pane too narrow.
func (m Model) playStripW(width int) int {
	s := m.play
	if s == nil || !m.playStructure || len(s.outline) == 0 || width < playStripMinPane {
		return 0
	}
	w := s.outlineW + 2 // the separator and a leading space
	if w < playStripMinW {
		w = playStripMinW
	}
	if w > playStripMaxW {
		w = playStripMaxW
	}
	return w
}

// playPaneStripW is playStripW for the hosting pane as laid out right now.
func (m Model) playPaneStripW() (strip, width int) {
	s := m.play
	if s == nil {
		return 0, 0
	}
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return 0, 0
	}
	width = paneInterior(r.W, paneChromeW)
	return m.playStripW(width), width
}

// playOutlineAt is the index of the outline entry whose node holds result
// line — the last entry starting at or above it, 0 above the first.
func playOutlineAt(items []jqplay.OutlineItem, line int) int {
	i := sort.Search(len(items), func(i int) bool { return items[i].Line > line }) - 1
	if i < 0 {
		return 0
	}
	return i
}

// playStripInView is the range of outline entries whose nodes the result
// viewport shows, inclusive.
func (s *playState) playStripInView() (lo, hi int) {
	first, last := s.resultEd.VisibleLines()
	return playOutlineAt(s.outline, first), playOutlineAt(s.outline, last)
}

// togglePlayStructure is playground.structure. A strip that cannot show
// (nothing to list, pane too narrow) still toggles the setting, and the
// status says why nothing appeared.
func (m *Model) togglePlayStructure() {
	s := m.play
	if s == nil {
		return
	}
	if m.playStructure {
		// Up and focused, or on but unable to show: either way the chord
		// means "away with it".
		if sw, _ := m.playPaneStripW(); s.stripFocus || sw == 0 {
			m.playStructure = false
			if s.stripFocus {
				s.setBufFocus(s.stripFrom)
			}
			s.status, s.statusWarn = "structure strip off", false
			m.sizePlayResult()
			return
		}
		m.focusPlayStrip()
		return
	}
	m.playStructure = true
	m.sizePlayResult()
	if sw, _ := m.playPaneStripW(); sw == 0 {
		// It stays on and appears once there is something to show.
		s.status, s.statusWarn = "structure strip: the pane is too narrow to show it", true
		if len(s.outline) == 0 {
			s.status = "structure strip: this result has no keys or items to list"
		}
		return
	}
	m.focusPlayStrip()
}

// focusPlayStrip gives the strip the keyboard with its selection on the node
// the result caret is in, remembering where the keys came from for esc.
func (m *Model) focusPlayStrip() {
	s := m.play
	from := s.bufFocus
	s.setBufFocus(true)
	s.resultEd.SetFocused(false) // the caret cell belongs to the strip now
	s.stripFocus, s.stripFrom = true, from
	line, _ := s.resultEd.CursorPos()
	s.stripSel = playOutlineAt(s.outline, line)
	s.status, s.statusWarn = "structure: ↑/↓ choose · enter jumps · esc returns", false
}

// updatePlayStripKey routes a key while the strip has the keyboard: the list
// motions move its selection, enter jumps to the selected node, esc and tab
// hand the keyboard back to where it came from. Modified chords resolve
// against the Global scope like in the result buffer, so the toggle chord
// itself (and every IDE-level one) keeps working.
func (m Model) updatePlayStripKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.play
	key := msg.String()
	_, rows := m.playStripRows()
	if ui.ListNav(key, &s.stripSel, len(s.outline), rows, ui.NavFull) {
		return m, nil
	}
	switch key {
	case "enter":
		m.jumpPlayStrip(s.stripSel)
		return m, nil
	case "esc", "tab":
		s.setBufFocus(s.stripFrom)
		return m, nil
	}
	if handled, cmd := m.playGlobalChord(msg); handled {
		return m, cmd
	}
	m.recordPlayUnbound(msg)
	return m, nil
}

// jumpPlayStrip puts the result caret on the first cell of outline entry i,
// framing it like any navigation jump, and gives the result the keyboard.
func (m *Model) jumpPlayStrip(i int) {
	s := m.play
	if i < 0 || i >= len(s.outline) {
		return
	}
	it := s.outline[i]
	s.stripSel = i
	s.setBufFocus(true)
	s.resultEd.JumpTo(it.Line, 0)
	s.resultEd.MoveToFirstNonBlank()
	s.status, s.statusWarn = "→ "+it.Label, false
}

// clickPlayStrip handles a left press at content-local x/y, reporting whether
// it landed on the strip; a press on an entry jumps to its node.
func (m *Model) clickPlayStrip(x, y int) bool {
	sw, width := m.playPaneStripW()
	if sw == 0 || x < width-sw || y < 0 {
		return false
	}
	if i := m.play.stripTop + y; i < len(m.play.outline) {
		m.jumpPlayStrip(i)
	}
	return true
}

// playStripRows is the strip's row count — the result editor's height — and
// the window top that keeps the entries of interest in it: the selection
// while the strip has the keyboard, else the nodes the viewport shows.
func (m Model) playStripRows() (top, rows int) {
	s := m.play
	rows = s.resultEd.Height()
	top = s.stripTop
	n := len(s.outline)
	if s.stripFocus {
		ui.ClampWindow(&s.stripSel, &top, n, rows)
	} else {
		lo, hi := s.playStripInView()
		top = ui.ScrollToShow(top, hi, rows, n)
		top = ui.ScrollToShow(top, lo, rows, n)
	}
	s.stripTop = top
	return top, rows
}

// playStripView renders the strip as rows lines of sw cells: a separator,
// then one entry per row. Entries whose nodes are on screen are drawn in the
// foreground on the surface colour, the rest as hints; the selection of a
// focused strip is the accent chip.
func (m Model) playStripView(sw int) []string {
	s := m.play
	top, rows := m.playStripRows()
	pal := m.pal()
	sep := lipgloss.NewStyle().Foreground(pal.Border).Render("│")
	off := lipgloss.NewStyle().Foreground(pal.Hint)
	in := lipgloss.NewStyle().Foreground(pal.Foreground).Background(pal.Surface)
	fg := theme.Readable(pal.Accent, pal.Background, pal.Surface, pal.Foreground)
	sel := lipgloss.NewStyle().Bold(true).Foreground(fg).Background(pal.Accent)
	lo, hi := s.playStripInView()
	out := make([]string, rows)
	blank := strings.Repeat(" ", sw-1)
	for r := range out {
		i := top + r
		if i >= len(s.outline) {
			out[r] = sep + blank
			continue
		}
		label := " " + ansi.Truncate(s.outline[i].Label, sw-2, "…")
		if w := ansi.StringWidth(label); w < sw-1 {
			label += strings.Repeat(" ", sw-1-w)
		}
		st := off
		switch {
		case s.stripFocus && i == s.stripSel:
			st = sel
		case i >= lo && i <= hi:
			st = in
		}
		out[r] = sep + st.Render(label)
	}
	return out
}

// playResultView is the result editor's view with the strip, when shown,
// joined on the right of every row. The editor was sized width-sw wide by
// sizePlayResult, so each of its rows is padded to that and the strip starts
// in the same column on every row.
func (m Model) playResultView(width int) string {
	view := m.play.resultEd.View()
	sw := m.playStripW(width)
	if sw == 0 {
		return view
	}
	edW := width - sw
	lines := strings.Split(view, "\n")
	strip := m.playStripView(sw)
	for i, l := range lines {
		if w := ansi.StringWidth(l); w < edW {
			l += strings.Repeat(" ", edW-w)
		} else if w > edW {
			l = ansi.Truncate(l, edW, "")
		}
		if i < len(strip) {
			l += strip[i]
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}
