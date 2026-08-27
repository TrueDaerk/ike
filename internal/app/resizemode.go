package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
)

// resizemode.go implements pane.resizeMode (#2150): a sticky keyboard mode for
// resizing the focused pane. Entering it once arms the mode; h/j/k/l and the
// arrow keys then move the pane's edge one cell per press through the very
// same split-ratio operation the mouse drag uses (layout.Divider), so geometry
// truth stays in one place. esc/enter (or q) leave the mode and persist the
// layout like a drag release does; every other key is inert, so a stray
// keystroke can never edit a buffer while the mode owns the keyboard.

// resizeModeStep is how many cells one key press moves the edge. One cell
// keeps the mode precise — key repeat covers the long distances, which is the
// point of a sticky mode.
const resizeModeStep = 1

// enterResizeMode arms the mode for the focused pane. A pane with no split to
// resize (single-pane workspace) or a maximized one — whose layout has no
// dividers at all — refuses with a hint instead of arming a mode where every
// key would be a no-op.
func (m *Model) enterResizeMode() {
	if m.resizeMode {
		return
	}
	key := m.activeWS().Panes.Focused()
	if key == "" {
		m.host.Notify(host.Info, "resize mode needs a focused pane")
		return
	}
	if m.zoomed != "" {
		m.host.Notify(host.Info, "resize mode: not while a pane is maximized")
		return
	}
	if len(m.lay.Dividers) == 0 {
		m.host.Notify(host.Info, "resize mode: nothing to resize — the pane fills the window")
		return
	}
	m.resizeMode = true
}

// leaveResizeMode disarms the mode and persists the new geometry, mirroring
// what a released divider drag commits.
func (m *Model) leaveResizeMode() {
	if !m.resizeMode {
		return
	}
	m.resizeMode = false
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// resizeModeKey handles one key press while the mode is armed. It consumes
// every key: the direction keys step the edge, the exit keys leave, and
// anything else is deliberately inert (#2150).
func (m *Model) resizeModeKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc", "enter", "q", "ctrl+c":
		m.leaveResizeMode()
		return
	}
	if dir, ok := resizeModeDir(msg.String()); ok {
		m.resizeFocusedPane(dir)
	}
}

// resizeModeDir maps the mode's movement keys — vim hjkl plus the arrows — to
// the direction the pane edge travels.
func resizeModeDir(keys string) (layout.Zone, bool) {
	switch keys {
	case "h", "left":
		return layout.ZoneLeft, true
	case "l", "right":
		return layout.ZoneRight, true
	case "k", "up":
		return layout.ZoneTop, true
	case "j", "down":
		return layout.ZoneBottom, true
	}
	return 0, false
}

// resizeFocusedPane moves the focused pane's edge one step toward dir. The
// divider carries a pointer into the live split tree, so the step mutates the
// tree in place and re-laying out is all that is needed to see it — exactly
// the mouse drag's motion path.
func (m *Model) resizeFocusedPane(dir layout.Zone) {
	key := m.activeWS().Panes.Focused()
	if key == "" {
		return
	}
	d, ok := m.lay.EdgeDivider(key, dir)
	if !ok {
		return
	}
	delta := resizeModeStep
	if dir == layout.ZoneLeft || dir == layout.ZoneTop {
		delta = -resizeModeStep
	}
	d.ResizeStep(delta)
	m.layout()
}

// resizeModeHint is the status line's mode banner (#2150): the mode has to be
// visible while it owns the keyboard, so the line says which pane is being
// resized and how to move and leave.
func (m Model) resizeModeHint() string {
	return "RESIZE " + m.paneLabel(m.activeWS().Panes.Focused()) +
		"  hjkl / arrows move the edge · esc to finish"
}
