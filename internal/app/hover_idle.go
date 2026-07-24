package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	"ike/internal/pane"
)

// hover_idle.go is the mouse-idle hover (#1129): resting the pointer over
// editor content for hoverIdleDelay opens the hover popup at the hovered cell
// (not the caret) — the diagnostic under the pointer immediately (pure local
// data), the LSP hover content when the server answers. The timer follows the
// idle-tick discipline (#1001): one demand-armed tea.Tick per resting cell,
// no free-running ticker. MVP scope: the focused editor pane only (JetBrains
// also hovers unfocused panes; deferred).

// hoverIdleDelay is how long the pointer must rest on one cell before the
// hover fires.
const hoverIdleDelay = 600 * time.Millisecond

// mouseHoverState tracks the pointer's resting cell for the idle hover.
type mouseHoverState struct {
	// pending marks an armed-but-unfired idle wait; fired marks a cell whose
	// hover already fired (so same-cell motion jitter never re-arms or
	// dismisses the open popup).
	pending  bool
	fired    bool
	paneKey  string
	x, y     int // screen cell the pointer rests on
	localX   int // pane-content-local cell, re-hit-tested at fire time
	localY   int
	pos      buffer.Position // hover target at track time
	deadline time.Time
}

// mouseHoverTickMsg wakes the model to check whether the pointer is still
// resting on the tracked cell.
type mouseHoverTickMsg struct{}

// trackMouseHover runs on every non-drag mouse motion (overlay branches in
// handleMouse return before it, so reaching here implies no context menu,
// finder, palette, settings, shell, or open menu). Motion onto a new cell
// closes a mouse-anchored popup, cancels the pending wait, and — over
// hoverable editor content — arms a fresh idle wait for the new cell.
func (m *Model) trackMouseHover(msg mouseEvent) tea.Cmd {
	if (m.hoverIdle.pending || m.hoverIdle.fired) && msg.X == m.hoverIdle.x && msg.Y == m.hoverIdle.y {
		return nil // still the same cell: the armed tick keeps counting
	}
	m.cancelMouseHover()
	st, ok := m.mouseHoverTarget(msg.X, msg.Y)
	if !ok {
		return nil
	}
	st.pending = true
	st.deadline = time.Now().Add(hoverIdleDelay)
	m.hoverIdle = st
	return m.armMouseHoverTick()
}

// cancelMouseHover drops the pending idle wait and closes a mouse-anchored
// popup (a key-triggered, cursor-anchored popup is left alone). Any click,
// wheel, or key funnels through here; the armed tick then no-ops.
func (m *Model) cancelMouseHover() {
	if !m.hoverIdle.pending && !m.hoverIdle.fired {
		return
	}
	if inst := m.activeWS().Panes.Get(m.hoverIdle.paneKey); inst != nil && inst.Kind() == pane.KindEditor {
		if ed := inst.Editor(); ed != nil {
			ed.DismissMouseHover()
		}
	}
	m.hoverIdle = mouseHoverState{}
}

// mouseHoverTarget hit-tests a screen cell for the idle hover: it must lie in
// the focused editor pane (MVP scope), over buffer content (the editor's
// HoverTarget rejects the gutter, scrollbar, sticky headers, and cells past
// the text), not on the large-file banner, and not while a text tab is
// showing a terminal.
func (m *Model) mouseHoverTarget(x, y int) (mouseHoverState, bool) {
	var zero mouseHoverState
	key, ok := m.lay.PaneAt(x, y)
	if !ok || key != m.activeWS().Panes.Focused() {
		return zero, false
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor || inst.ActiveTerminal() != nil {
		return zero, false
	}
	ed := inst.Editor()
	if ed == nil || !ed.HasFile() {
		return zero, false
	}
	// The large-file banner (#1124) overlays the first content row: resting
	// on it must not hover the text underneath.
	if text, bx, by, _, on := m.largeFileBanner(); on && y == by && x >= bx && x < bx+lipgloss.Width(text) {
		return zero, false
	}
	r, found := m.lay.Panes[key]
	if !found {
		return zero, false
	}
	lx, ly := x-(r.X+paneContentX), y-(r.Y+paneContentY)
	pos, hit := ed.HoverTarget(lx, ly)
	if !hit {
		return zero, false
	}
	return mouseHoverState{paneKey: key, x: x, y: y, localX: lx, localY: ly, pos: pos}, true
}

// armMouseHoverTick schedules one wake at the pending deadline; at most one
// tick is in flight (#1001). The tick handler re-arms when the deadline moved
// (the pointer settled on a newer cell while the old tick was pending).
func (m *Model) armMouseHoverTick() tea.Cmd {
	if m.hoverIdleTickArmed || !m.hoverIdle.pending {
		return nil
	}
	m.hoverIdleTickArmed = true
	d := time.Until(m.hoverIdle.deadline)
	if d < 0 {
		d = 0
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return mouseHoverTickMsg{} })
}

// mouseHoverTick handles the idle wake: fire when the pointer is still
// resting on the tracked cell past its deadline, re-arm when the wait moved
// to a newer cell, no-op when it was cancelled.
func (m *Model) mouseHoverTick(now time.Time) tea.Cmd {
	m.hoverIdleTickArmed = false
	if !m.hoverIdle.pending {
		return nil
	}
	if now.Before(m.hoverIdle.deadline) {
		return m.armMouseHoverTick()
	}
	m.hoverIdle.pending = false
	m.hoverIdle.fired = true
	m.fireMouseHover()
	return nil
}

// fireMouseHover opens the hover for the resting cell: the guards re-run (a
// drag may have started, focus moved, the buffer scrolled under the pointer —
// the cell is re-hit-tested and must map to the same position), then the
// diagnostic part shows immediately and the LSP request goes out through the
// editor-event seam the bridge listens on (host.EditorHoverRequest).
func (m *Model) fireMouseHover() {
	if m.drag != nil || m.ctxMenu.IsOpen() {
		return
	}
	st, ok := m.mouseHoverTarget(m.hoverIdle.x, m.hoverIdle.y)
	if !ok || st.pos != m.hoverIdle.pos {
		return
	}
	ed := m.activeWS().Panes.Get(st.paneKey).Editor()
	ed.ShowMouseHover(st.pos)
	m.host.EmitEditor(host.EditorEvent{
		Kind: host.EditorHoverRequest,
		Path: ed.Path(),
		Line: st.pos.Line,
		Col:  st.pos.Col,
	})
}
