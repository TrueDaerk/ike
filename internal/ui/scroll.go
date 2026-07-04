package ui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// indicatorColor is the dim foreground used for the scroll position bar.
const indicatorColor = "#585858"

// scroller wraps bubbles/viewport to scroll body content vertically when it is
// taller than the visible area. It adds g/G (top/bottom) on top of the
// viewport's built-in ↑/↓, pgup/pgdn, and ctrl+u/ctrl+d bindings, and renders a
// position indicator so the user knows there is more off-screen. It is the
// reusable scroller shared by every Floating shell (generalised from the help
// overlay's one-off scroller).
type scroller struct {
	vp viewport.Model
}

// newScroller returns a scroller sized to width x height.
func newScroller(width, height int) scroller {
	return scroller{vp: viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))}
}

// SetSize resizes the viewport, clamping the offset to the new bounds.
func (s *scroller) SetSize(width, height int) {
	s.vp.SetWidth(width)
	s.vp.SetHeight(height)
	s.vp.SetYOffset(s.vp.YOffset()) // re-clamp against the new max offset
}

// SetContent replaces the scrolled text and resets to the top.
func (s *scroller) SetContent(content string) {
	s.vp.SetContent(content)
	s.vp.GotoTop()
}

// Update routes a scroll key. g/G jump to the extremes; every other key is
// delegated to the viewport's own key map.
func (s *scroller) Update(msg tea.Msg) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "g", "home":
			s.vp.GotoTop()
			return
		case "G", "end":
			s.vp.GotoBottom()
			return
		}
	}
	s.vp, _ = s.vp.Update(msg)
}

// scrollable reports whether the content overflows the viewport.
func (s *scroller) scrollable() bool { return s.vp.TotalLineCount() > s.vp.Height() }

// View renders the visible slice with a trailing position indicator line when
// the content overflows.
func (s *scroller) View() string {
	body := s.vp.View()
	if !s.scrollable() {
		return body
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, s.indicator())
}

// indicator is a one-line position bar: "▲ … ▼  NN%". The arrows show which
// directions have hidden content; the percentage reflects scroll progress.
func (s *scroller) indicator() string {
	up, down := " ", " "
	if !s.vp.AtTop() {
		up = "▲"
	}
	if !s.vp.AtBottom() {
		down = "▼"
	}
	pct := strconv.Itoa(int(s.vp.ScrollPercent()*100)) + "%"
	dashes := strings.Repeat("─", maxInt(s.vp.Width()-len(pct)-5, 0))
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(indicatorColor))
	return style.Render(up + " " + dashes + " " + down + " " + pct)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
