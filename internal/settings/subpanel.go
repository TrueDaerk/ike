package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	ov "ike/internal/overlay"
)

// subpanel.go is the settings sub-panel primitive (0420, #883): a stack of
// pushed overlay levels rendered as a bordered inner box centered over the
// panel, with a breadcrumb header ("Settings › Toolchain › New Environment"),
// esc popping exactly one level, and a clickable button row — so every
// multi-field or multi-step flow (forms, wizards, confirmations) is
// mouse-complete by construction instead of a footer-line state machine.

// SubPanel is one pushed settings overlay level.
type SubPanel interface {
	// Title is this level's breadcrumb segment.
	Title() string
	// Update receives keys while this level is topmost. Non-capturing panels
	// never see esc (the stack pops) or keys claimed by their Buttons.
	Update(key tea.KeyPressMsg) tea.Cmd
	// View renders the content area (inside the box, below the breadcrumb,
	// above the button row).
	View(width, height int) string
	// Capturing reports the panel wants every key verbatim (text inputs,
	// chord capture). A capturing panel must pop itself on esc/cancel.
	Capturing() bool
	// Buttons is the clickable action row, left to right. A button's Key
	// (optional) triggers it from the keyboard while the panel is not
	// capturing; capturing panels trigger their actions themselves.
	Buttons() []Button
}

// Button is one action in a sub-panel's button row.
type Button struct {
	Label    string
	Key      string // optional trigger key (tea key String form), shown as a hint
	Do       func() tea.Cmd
	Disabled bool
}

// SubPanelClicker is an optional SubPanel extension: presses inside the
// content area arrive at content-local coordinates ((0,0) = top-left of the
// area View rendered into).
type SubPanelClicker interface {
	Click(x, y int) tea.Cmd
}

// SubPanelWheeler is an optional SubPanel extension: wheel deltas while the
// pointer is over the sub-panel (negative = up).
type SubPanelWheeler interface {
	Wheel(delta int)
}

// SubPanelHost is what pages need to run sub-panels: push a level, pop the
// top one. The settings Model implements it; pages implementing
// SetSubPanelHost get it injected at construction.
type SubPanelHost interface {
	Push(SubPanel)
	Pop()
}

// hostAware is the injection seam: custom pages implementing it receive the
// panel as their SubPanelHost when the panel is built.
type hostAware interface {
	SetSubPanelHost(SubPanelHost)
}

// Push opens a sub-panel above the current level.
func (m *Model) Push(sp SubPanel) { m.stack = append(m.stack, sp) }

// Pop closes the top sub-panel; the level below (or the page) resumes.
func (m *Model) Pop() {
	if len(m.stack) > 0 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// SubOpen reports whether any sub-panel is open.
func (m *Model) SubOpen() bool { return len(m.stack) > 0 }

// topSub returns the topmost sub-panel, nil when none is open.
func (m *Model) topSub() SubPanel {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

// updateSub handles one key while a sub-panel is open: a capturing panel owns
// every key (it pops itself on esc); otherwise esc pops one level, a button
// key runs its action, and the rest reaches the panel.
func (m *Model) updateSub(key tea.KeyPressMsg) tea.Cmd {
	top := m.topSub()
	if top.Capturing() {
		return top.Update(key)
	}
	if key.String() == "esc" {
		m.Pop()
		return nil
	}
	for _, b := range top.Buttons() {
		if !b.Disabled && b.Key != "" && b.Key == key.String() && b.Do != nil {
			return b.Do()
		}
	}
	return top.Update(key)
}

// subRect is the sub-panel box's geometry in panel-local coordinates.
func (m *Model) subRect() (x, y, w, h int) {
	w = m.width - 6
	if w > 76 {
		w = 76
	}
	if w < 24 {
		w = m.width - 2
	}
	h = m.height - 4
	if h > 22 {
		h = 22
	}
	if h < 7 {
		h = m.height - 2
	}
	return (m.width - w) / 2, (m.height - h) / 2, w, h
}

// subContentHeight is the content-area height inside a box of outer height h:
// border rows (2) + breadcrumb (1) + button row (1).
func subContentHeight(h int) int {
	if c := h - 4; c > 0 {
		return c
	}
	return 1
}

// clickSub routes a panel-local press while a sub-panel is open: buttons and
// content are hit-tested; presses outside the box are swallowed (esc or a
// Cancel button closes, never a stray click).
func (m *Model) clickSub(px, py int) tea.Cmd {
	top := m.topSub()
	x0, y0, w, h := m.subRect()
	if px < x0 || px >= x0+w || py < y0 || py >= y0+h {
		return nil
	}
	lx, ly := px-x0-1, py-y0-1 // inner coordinates (border stripped)
	contentH := subContentHeight(h)
	switch {
	case ly == 1+contentH: // the button row
		spans := buttonSpans(top.Buttons())
		for i, s := range spans {
			b := top.Buttons()[i]
			if lx >= s.start && lx < s.end && !b.Disabled && b.Do != nil {
				return b.Do()
			}
		}
	case ly >= 1 && ly <= contentH:
		if c, ok := top.(SubPanelClicker); ok {
			return c.Click(lx, ly-1)
		}
	}
	return nil
}

// wheelSub forwards a wheel delta to a wheeling sub-panel.
func (m *Model) wheelSub(delta int) {
	if w, ok := m.topSub().(SubPanelWheeler); ok {
		w.Wheel(delta)
	}
}

// span is one button's cell range on the button row.
type span struct{ start, end int }

// buttonSpans computes each button's hit range under the same layout
// renderButtons draws: a leading space, "[ label ]" cells, two spaces
// between buttons.
func buttonSpans(buttons []Button) []span {
	out := make([]span, len(buttons))
	x := 1
	for i, b := range buttons {
		w := lipgloss.Width("[ " + b.Label + " ]")
		out[i] = span{start: x, end: x + w}
		x += w + 2
	}
	return out
}

// renderButtons draws the clickable action row.
func (m *Model) renderButtons(buttons []Button, w int) string {
	pal := m.theme()
	var parts []string
	for _, b := range buttons {
		style := lipgloss.NewStyle().Foreground(pal.Foreground).Bold(true)
		if b.Disabled {
			style = lipgloss.NewStyle().Foreground(pal.Secondary).Faint(true)
		}
		parts = append(parts, style.Render("[ "+b.Label+" ]"))
	}
	row := " " + strings.Join(parts, "  ")
	return lipgloss.NewStyle().MaxWidth(w).Render(row)
}

// breadcrumb renders "Settings › <page> › <levels…>".
func (m *Model) breadcrumb() string {
	parts := []string{"Settings"}
	if m.cat >= 0 && m.cat < len(m.pages) {
		parts = append(parts, m.pages[m.cat].Title)
	}
	for _, sp := range m.stack {
		parts = append(parts, sp.Title())
	}
	return " " + strings.Join(parts, " › ")
}

// renderSub composes the sub-panel box over the rendered panel.
func (m *Model) renderSub(base string) string {
	top := m.topSub()
	pal := m.theme()
	_, _, w, h := m.subRect()
	innerW := w - 2
	contentH := subContentHeight(h)

	crumb := lipgloss.NewStyle().Bold(true).Foreground(pal.BorderFocus).
		MaxWidth(innerW).Render(m.breadcrumb())
	content := lipgloss.NewStyle().MaxWidth(innerW).MaxHeight(contentH).
		Render(top.View(innerW, contentH))
	// Pad the content to its full height so the button row stays pinned.
	if lines := strings.Count(content, "\n") + 1; lines < contentH {
		content += strings.Repeat("\n", contentH-lines)
	}
	buttons := m.renderButtons(top.Buttons(), innerW)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Background(pal.Surface).
		Foreground(pal.Foreground).
		Width(w).
		Height(h).
		Render(lipgloss.JoinVertical(lipgloss.Left, crumb, content, buttons))
	return ov.Center(base, box, m.width, m.height)
}
