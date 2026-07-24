// Package menu implements the menu bar (Roadmap 0160, #90): a JetBrains-style
// top row (File · Edit · View · …) whose dropdowns are a discovery surface over
// the command registry. Menus are data — every entry references a registered
// command id and shows the same shortcut the cheatsheet shows; ids that are not
// registered (blocked ledger, future roadmaps) render disabled with their
// unblocking dependency as a hint. The menu dispatches RunMsg; it never runs
// anything itself.
package menu

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// Item is one dropdown entry referencing a registered command id.
type Item struct {
	Title   string
	Command string
}

// Menu is one titled dropdown.
type Menu struct {
	Title string
	Items []Item
}

// Info is what the menu needs to know about a command id, provided by the app
// from the registry, the keymap resolver and the blocked ledger.
type Info struct {
	Runnable bool   // the id resolves to a registered command
	Shortcut string // chord shown right-aligned (cheatsheet source)
	Hint     string // for disabled entries: the unblocking dependency
}

// InfoFunc resolves one command id.
type InfoFunc func(commandID string) Info

// RunMsg asks the root model to run the referenced registry command.
type RunMsg struct{ Command string }

// Model is the menu-bar state: which menu is open and which entry is selected.
type Model struct {
	menus []Menu
	info  InfoFunc
	pal   *theme.Palette

	width  int
	open   bool
	active int
	sel    int

	// Bar cache (#1101): the bar string only changes with width, open state,
	// active menu or palette — not per frame.
	barCache  string
	barWidth  int
	barOpen   bool
	barActive int
	barPal    *theme.Palette
	barValid  bool
}

// New builds a closed menu bar over menus, resolving command ids through info.
func New(menus []Menu, info InfoFunc) *Model {
	return &Model{menus: menus, info: info}
}

// SetPalette threads the active theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetWidth sets the bar's render width.
func (m *Model) SetWidth(w int) { m.width = w }

// IsOpen reports whether a dropdown is open (the menu owns the keyboard then).
func (m *Model) IsOpen() bool { return m.open }

// Toggle opens the first menu, or closes an open one (the F10 behavior).
func (m *Model) Toggle() {
	if m.open {
		m.Close()
		return
	}
	m.OpenMenu(0)
}

// OpenMenu opens the dropdown for menu index i.
func (m *Model) OpenMenu(i int) {
	if len(m.menus) == 0 {
		return
	}
	if i < 0 || i >= len(m.menus) {
		i = 0
	}
	m.open = true
	m.active = i
	m.sel = m.firstRunnable(i)
}

// Close closes any open dropdown.
func (m *Model) Close() { m.open = false }

// Update handles one key while a dropdown is open. The returned command is
// non-nil when an entry was invoked.
func (m *Model) Update(key tea.KeyPressMsg) tea.Cmd {
	if !m.open {
		return nil
	}
	switch key.String() {
	case "esc":
		m.Close()
	case "left":
		m.OpenMenu((m.active + len(m.menus) - 1) % len(m.menus))
	case "right":
		m.OpenMenu((m.active + 1) % len(m.menus))
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	case "enter":
		return m.invoke(m.sel)
	default:
		// A menu title's first letter jumps to (and opens) that menu — the
		// underlined hints in the bar. Duplicate letters cycle forward.
		if i, ok := m.menuForLetter(key.String()); ok {
			m.OpenMenu(i)
		}
	}
	return nil
}

// menuForLetter finds the next menu (searching forward from the active one,
// wrapping) whose title starts with the given letter, case-insensitively.
func (m *Model) menuForLetter(s string) (int, bool) {
	r := []rune(strings.ToLower(s))
	if len(r) != 1 || !unicode.IsLetter(r[0]) {
		return 0, false
	}
	for off := 1; off <= len(m.menus); off++ {
		i := (m.active + off) % len(m.menus)
		t := []rune(strings.ToLower(m.menus[i].Title))
		if len(t) > 0 && t[0] == r[0] {
			return i, true
		}
	}
	return 0, false
}

// move advances the selection by dir, skipping disabled entries (wrapping).
func (m *Model) move(dir int) {
	items := m.menus[m.active].Items
	if len(items) == 0 {
		return
	}
	sel := m.sel
	for i := 0; i < len(items); i++ {
		sel = (sel + dir + len(items)) % len(items)
		if m.info(items[sel].Command).Runnable {
			m.sel = sel
			return
		}
	}
}

// firstRunnable returns the first enabled entry of menu i (0 when none is).
func (m *Model) firstRunnable(i int) int {
	for idx, it := range m.menus[i].Items {
		if m.info(it.Command).Runnable {
			return idx
		}
	}
	return 0
}

// invoke closes the menu and dispatches the selected entry's command, if it is
// runnable.
func (m *Model) invoke(idx int) tea.Cmd {
	items := m.menus[m.active].Items
	if idx < 0 || idx >= len(items) {
		return nil
	}
	it := items[idx]
	if !m.info(it.Command).Runnable {
		return nil
	}
	m.Close()
	return func() tea.Msg { return RunMsg{Command: it.Command} }
}

// TitleAt hit-tests the bar row: it returns the menu index under column x.
func (m *Model) TitleAt(x int) (int, bool) {
	for i := range m.menus {
		start, end := m.titleSpan(i)
		if x >= start && x < end {
			return i, true
		}
	}
	return 0, false
}

// ItemAt hit-tests the open dropdown at absolute cell (x, y), with the bar on
// row 0, the dropdown's top border on row 1, and its first entry on row 2. The
// entry cells sit one column inside the border.
func (m *Model) ItemAt(x, y int) (int, bool) {
	if !m.open {
		return 0, false
	}
	x0 := m.DropdownX() + 1
	row := y - 2
	if row < 0 || row >= len(m.menus[m.active].Items) {
		return 0, false
	}
	if x < x0 || x >= x0+lipgloss.Width(m.dropdownLine(row)) {
		return 0, false
	}
	return row, true
}

// Invoke runs the entry at idx of the open menu (the mouse-click path).
func (m *Model) Invoke(idx int) tea.Cmd { return m.invoke(idx) }

// Hover moves the selection to entry idx (the mouse-motion path); disabled
// entries are ignored so hover mirrors keyboard navigation.
func (m *Model) Hover(idx int) {
	items := m.menus[m.active].Items
	if idx < 0 || idx >= len(items) {
		return
	}
	if m.info(items[idx].Command).Runnable {
		m.sel = idx
	}
}

// titleSpan returns the [start, end) columns of menu i's title cell in the bar.
func (m *Model) titleSpan(i int) (int, int) {
	x := 0
	for j := 0; j <= i; j++ {
		w := lipgloss.Width(m.titleCell(j))
		if j == i {
			return x, x + w
		}
		x += w
	}
	return 0, 0
}

// titleCell is one padded bar segment (" File ").
func (m *Model) titleCell(i int) string { return " " + m.menus[i].Title + " " }

// DropdownX is the column the open dropdown aligns to (its menu title).
func (m *Model) DropdownX() int {
	start, _ := m.titleSpan(m.active)
	return start
}

// theme returns the active palette, defaulting when none was threaded in.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// Bar renders the top row: every menu title, the active one highlighted while
// its dropdown is open. While open, each title's first letter is underlined as
// a hint that pressing it jumps to that menu.
func (m *Model) Bar() string {
	pal := m.theme()
	if m.barValid && m.barWidth == m.width && m.barOpen == m.open &&
		m.barActive == m.active && m.barPal == m.pal {
		return m.barCache
	}
	bar := lipgloss.NewStyle().Background(pal.Panel).Foreground(pal.Foreground)
	activeStyle := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.Foreground).Bold(true)
	var b strings.Builder
	for i := range m.menus {
		style := bar
		if m.open && i == m.active {
			style = activeStyle
		}
		b.WriteString(m.renderTitleCell(style, m.menus[i].Title))
	}
	out := bar.Width(m.width).Render(b.String())
	m.barCache, m.barWidth, m.barOpen, m.barActive, m.barPal, m.barValid =
		out, m.width, m.open, m.active, m.pal, true
	return out
}

// renderTitleCell renders one padded bar segment (" File "), underlining the
// title's first letter while a dropdown is open (the letter-jump hint).
func (m *Model) renderTitleCell(style lipgloss.Style, title string) string {
	if !m.open || title == "" {
		return style.Render(" " + title + " ")
	}
	r := []rune(title)
	return style.Render(" ") +
		style.Underline(true).Render(string(r[0])) +
		style.Render(string(r[1:])+" ")
}

// Dropdown renders the open menu's entry list — one entry per row, shortcuts
// right-aligned, disabled entries dimmed with their hint — framed by a rounded
// border so the dropdown separates from whatever it floats over.
func (m *Model) Dropdown() string {
	if !m.open {
		return ""
	}
	lines := make([]string, len(m.menus[m.active].Items))
	for i := range m.menus[m.active].Items {
		lines[i] = m.dropdownLine(i)
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme().BorderFocus)
	return box.Render(strings.Join(lines, "\n"))
}

// dropdownLine renders one entry row at the dropdown's shared width.
func (m *Model) dropdownLine(idx int) string {
	items := m.menus[m.active].Items
	return listLine(items, m.info, m.theme(), m.sel, idx, m.dropdownWidth())
}

// dropdownWidth is the shared row width of the open dropdown.
func (m *Model) dropdownWidth() int {
	return listWidth(m.menus[m.active].Items, m.info)
}

// listLine renders one entry row of an item list at the shared width w:
// shortcut right-aligned, disabled entries dimmed with their hint, the
// selected entry highlighted. Shared by the bar dropdown and the context menu.
func listLine(items []Item, info InfoFunc, pal *theme.Palette, sel, idx, w int) string {
	it := items[idx]
	in := info(it.Command)

	label := it.Title
	right := in.Shortcut
	if !in.Runnable {
		right = in.Hint
	}
	gap := w - lipgloss.Width(label) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	line := " " + label + strings.Repeat(" ", gap) + right + " "

	style := lipgloss.NewStyle().Background(pal.Surface).Foreground(pal.Foreground)
	switch {
	case !in.Runnable:
		style = style.Foreground(pal.Secondary).Faint(true)
	case idx == sel:
		style = style.Background(pal.Selection).Bold(true)
	}
	return style.Render(line)
}

// listWidth is the shared row width of an item list.
func listWidth(items []Item, info InfoFunc) int {
	w := 0
	for _, it := range items {
		in := info(it.Command)
		right := in.Shortcut
		if !in.Runnable {
			right = in.Hint
		}
		if n := lipgloss.Width(it.Title) + lipgloss.Width(right) + 4; n > w {
			w = n
		}
	}
	return w
}
