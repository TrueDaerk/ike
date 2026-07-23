// context.go implements the right-click context menu (#1020): a floating
// dropdown anchored at the click cell instead of the bar row. It shares the
// bar's vocabulary — entries are Items referencing registered command ids,
// resolved through the same InfoFunc — and its rendering, so both discovery
// surfaces look identical. Like the bar, it dispatches RunMsg and never runs
// anything itself.
package menu

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// Context is a floating right-click menu: one flat item list anchored at a
// screen cell, clamped to the terminal bounds at open time.
type Context struct {
	items []Item
	info  InfoFunc
	pal   *theme.Palette

	open bool
	sel  int
	x, y int // clamped top-left of the rendered box
}

// NewContext builds a closed context menu resolving command ids through info.
func NewContext(info InfoFunc) *Context { return &Context{info: info} }

// SetPalette threads the active theme palette.
func (c *Context) SetPalette(p *theme.Palette) { c.pal = p }

// IsOpen reports whether the menu is open (it owns keyboard and mouse then).
// Nil-safe so models built without NewWith (tests) never nil-check.
func (c *Context) IsOpen() bool { return c != nil && c.open }

// Close dismisses the menu.
func (c *Context) Close() { c.open = false }

// Open shows items anchored at cell (x, y), shifting the box left/up so it
// stays inside the tw×th terminal. Opening with no items is a no-op.
func (c *Context) Open(items []Item, x, y, tw, th int) {
	if len(items) == 0 {
		return
	}
	c.items = items
	c.open = true
	c.sel = c.firstRunnable()
	w, h := listWidth(items, c.info)+2, len(items)+2
	if x > tw-w {
		x = tw - w
	}
	if x < 0 {
		x = 0
	}
	if y > th-h {
		y = th - h
	}
	if y < 0 {
		y = 0
	}
	c.x, c.y = x, y
}

// Pos returns the clamped top-left cell of the rendered box.
func (c *Context) Pos() (int, int) { return c.x, c.y }

// Update handles one key while the menu is open. The returned command is
// non-nil when an entry was invoked.
func (c *Context) Update(key tea.KeyPressMsg) tea.Cmd {
	if !c.open {
		return nil
	}
	switch key.String() {
	case "esc":
		c.Close()
	case "up":
		c.move(-1)
	case "down":
		c.move(1)
	case "enter":
		return c.invoke(c.sel)
	}
	return nil
}

// move advances the selection by dir, skipping disabled entries (wrapping).
func (c *Context) move(dir int) {
	sel := c.sel
	for i := 0; i < len(c.items); i++ {
		sel = (sel + dir + len(c.items)) % len(c.items)
		if c.info(c.items[sel].Command).Runnable {
			c.sel = sel
			return
		}
	}
}

// firstRunnable returns the first enabled entry (0 when none is).
func (c *Context) firstRunnable() int {
	for idx, it := range c.items {
		if c.info(it.Command).Runnable {
			return idx
		}
	}
	return 0
}

// ItemAt hit-tests the open menu at absolute cell (x, y): entry rows sit one
// cell inside the border.
func (c *Context) ItemAt(x, y int) (int, bool) {
	if !c.open {
		return 0, false
	}
	row := y - (c.y + 1)
	if row < 0 || row >= len(c.items) {
		return 0, false
	}
	if x < c.x+1 || x >= c.x+1+listWidth(c.items, c.info) {
		return 0, false
	}
	return row, true
}

// Hover moves the selection to entry idx (the mouse-motion path); disabled
// entries are ignored so hover mirrors keyboard navigation.
func (c *Context) Hover(idx int) {
	if idx < 0 || idx >= len(c.items) {
		return
	}
	if c.info(c.items[idx].Command).Runnable {
		c.sel = idx
	}
}

// Invoke runs the entry at idx (the mouse-click path).
func (c *Context) Invoke(idx int) tea.Cmd { return c.invoke(idx) }

// invoke closes the menu and dispatches the selected entry's command, if it
// is runnable.
func (c *Context) invoke(idx int) tea.Cmd {
	if idx < 0 || idx >= len(c.items) {
		return nil
	}
	it := c.items[idx]
	if !c.info(it.Command).Runnable {
		return nil
	}
	c.Close()
	return func() tea.Msg { return RunMsg{Command: it.Command} }
}

// theme returns the active palette, defaulting when none was threaded in.
func (c *Context) theme() *theme.Palette {
	if c.pal != nil {
		return c.pal
	}
	return theme.DefaultPalette()
}

// View renders the open menu — the bar dropdown's frame and row styling over
// the anchored item list.
func (c *Context) View() string {
	if !c.open {
		return ""
	}
	w := listWidth(c.items, c.info)
	lines := make([]string, len(c.items))
	for i := range c.items {
		lines[i] = listLine(c.items, c.info, c.theme(), c.sel, i, w)
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c.theme().BorderFocus)
	return box.Render(strings.Join(lines, "\n"))
}
