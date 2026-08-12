package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/ui"
)

// floatterm.go — torn-out floating terminal panels (#1793). Dragging a tab
// out of the popup terminal (#1398) re-homes its live session into a panel of
// its own: a detached tab host with the popup's pane chrome, freely positioned
// and sized, z-ordered with the popup box (the #1237 layering rules: the
// topmost/focused surface owns the keyboard, every focus change — click or
// focus chord — raises the surface it lands on, panel or box (#1806), the
// toggle chord hides and shows the whole layer at once). A panel marked
// global (the ●/○ title button) belongs to the app instead of the project:
// it rides across project switches with its session — process, scrollback,
// CWD — intact, and only ends with an app quit or an explicit close.

// floatGlobalOn / floatGlobalOff render the global-toggle button at the start
// of a panel's title row (#1793): ● = app-owned (survives project switches),
// ○ = project-owned (parks with the workspace like the popup box, #1407).
const (
	floatGlobalOn  = "● "
	floatGlobalOff = "○ "
	floatMarkerW   = 2 // cells the button occupies in the title row
)

// floatTerm is one floating terminal panel: a detached tab host plus its
// outer screen rectangle. Panels are runtime state — their sessions never
// resurrect across app restarts (the #1398 rule), so the geometry is not
// persisted either; only the popup box's position/size deltas are (#774,
// #1714). global marks the panel app-owned (#1793): it is excluded from the
// per-project parking (#1407) and carried into the fresh model on a switch.
type floatTerm struct {
	inst   *pane.Instance // detached tab host, like the popup's sides
	x, y   int            // outer top-left screen cell
	w, h   int            // outer box size
	global bool           // app-owned: survives project switches (#1793)
}

// floatMoveDrag tracks a live titlebar move drag (#1793): the popup box
// (target == nil, offset persisted via the winSizes stores) or one floating
// panel (position updated directly).
type floatMoveDrag struct {
	target       *floatTerm // nil: the popup box
	lastX, lastY int
}

// floatTermSeq mints unique host keys for torn-out panels. Package-level like
// popupSessSeq: keys must stay unique across the models a session builds.
var floatTermSeq int

// popupLayerOpen reports whether the popup terminal layer — the popup box
// plus every floating panel (#1793) — is shown and has anything to show.
func (m Model) popupLayerOpen() bool {
	return m.popup.open && (m.popup.inst != nil || len(m.floatTerms) > 0)
}

// popupLayerInstances returns every live tab host of the popup layer: the
// popup box's sides (#1427) plus every floating panel (#1793). Callers that
// must touch every popup-layer shell (theme, quit, focus-clear) iterate this.
func (m Model) popupLayerInstances() []*pane.Instance {
	out := m.popup.instances()
	for _, f := range m.floatTerms {
		out = append(out, f.inst)
	}
	return out
}

// floatFocused resolves the keyboard-owning floating panel: the explicitly
// focused one, or the topmost panel when the popup box is gone — the layer
// then has no other keyboard owner. nil means the popup box owns the keys.
func (m Model) floatFocused() *floatTerm {
	if m.floatFocus != nil {
		return m.floatFocus
	}
	if m.popup.inst == nil && len(m.floatTerms) > 0 {
		return m.floatTerms[len(m.floatTerms)-1]
	}
	return nil
}

// setFloatFocus moves keyboard ownership within the popup layer: to panel f,
// or back to the popup box (f == nil, split-side state preserved). Either way
// the surface focused also rises to the top of the layer's z-order (#1806) —
// the panel here, the box in setPopupFocus — so the #1237 invariant "the
// topmost surface owns the keyboard" holds after every focus change (click,
// chord or programmatic) and the keyboard owner is never covered.
func (m *Model) setFloatFocus(f *floatTerm) {
	if f != nil {
		m.raiseFloatTerm(f)
	}
	m.floatFocus = f
	for _, ft := range m.floatTerms {
		ft.inst.SetFocused(ft == f)
	}
	if f == nil {
		m.setPopupFocus(m.popup.focusRight)
		return
	}
	if m.popup.inst != nil {
		m.popup.inst.SetFocused(false)
	}
	if m.popup.split != nil {
		m.popup.split.SetFocused(false)
	}
}

// raiseFloatTerm moves panel f to the top of the z-order (last in floatTerms,
// drawn last by the compositor) — above the popup box too, whose slot shifts
// down when f came from below it. A panel that is already topmost — or not in
// the list at all — stays put.
func (m *Model) raiseFloatTerm(f *floatTerm) {
	for i, ft := range m.floatTerms {
		if ft == f {
			z := m.popupBoxZ()
			if i < z {
				z--
			}
			m.floatTerms = append(append(m.floatTerms[:i], m.floatTerms[i+1:]...), f)
			m.popup.boxZ = z
			return
		}
	}
}

// raisePopupBox moves the popup box to the top of the layer's z-order (#1806),
// above every floating panel — the box's half of raiseFloatTerm.
func (m *Model) raisePopupBox() { m.popup.boxZ = len(m.floatTerms) }

// popupBoxZ is the box's z-slot: the number of floating panels drawn below it,
// clamped so a slot recorded for a different panel set (project parking,
// #1407) can never index out of the live list.
func (m Model) popupBoxZ() int {
	return min(max(m.popup.boxZ, 0), len(m.floatTerms))
}

// floatTermsSplit cuts the panel z-order at the popup box's slot: the panels
// drawn below the box and the ones drawn above it, each bottom-to-top.
// Compositing, hit testing and focus stepping all walk the layer through this.
func (m Model) floatTermsSplit() (below, above []*floatTerm) {
	z := m.popupBoxZ()
	return m.floatTerms[:z], m.floatTerms[z:]
}

// floatTermFor resolves a tab host back to its floating panel, nil when inst
// is not a panel host (a popup box side, or gone).
func (m Model) floatTermFor(inst *pane.Instance) *floatTerm {
	for _, f := range m.floatTerms {
		if f.inst == inst {
			return f
		}
	}
	return nil
}

// removeFloatTermEntry splices the panel out of the z-order and drops a stale
// focus pointer; callers settle where the keyboard goes next.
func (m *Model) removeFloatTermEntry(f *floatTerm) {
	for i, ft := range m.floatTerms {
		if ft == f {
			z := m.popupBoxZ()
			if i < z {
				z--
			}
			m.floatTerms = append(m.floatTerms[:i], m.floatTerms[i+1:]...)
			m.popup.boxZ = z
			break
		}
	}
	if m.floatFocus == f {
		m.floatFocus = nil
	}
}

// removeFloatTerm removes a closed panel and settles focus: the topmost
// remaining panel when the popup box is gone, the box otherwise. Closing the
// layer's last surface hides the layer — the next toggle spawns fresh,
// mirroring closePopupTab's last-tab arm.
func (m *Model) removeFloatTerm(f *floatTerm) {
	wasFocused := m.floatFocused() == f
	m.removeFloatTermEntry(f)
	switch {
	case m.popup.inst == nil && len(m.floatTerms) == 0:
		m.popup.open = false
	case wasFocused && m.popup.inst == nil:
		m.setFloatFocus(m.floatTerms[len(m.floatTerms)-1])
	case wasFocused:
		m.setFloatFocus(nil)
	}
}

// globalFloatTerms filters the app-owned panels (#1793) — the ones a project
// switch carries into the fresh model instead of parking.
func globalFloatTerms(fs []*floatTerm) []*floatTerm {
	var out []*floatTerm
	for _, f := range fs {
		if f.global {
			out = append(out, f)
		}
	}
	return out
}

// projectFloatTerms filters the project-owned panels — parked in wsExtras
// with the popup box (#1407) and torn down with the workspace.
func projectFloatTerms(fs []*floatTerm) []*floatTerm {
	var out []*floatTerm
	for _, f := range fs {
		if !f.global {
			out = append(out, f)
		}
	}
	return out
}

// toggleFloatTermGlobal flips a panel's owner (#1793). Global: the session
// leaves the project — switches keep it on screen, workspace teardown spares
// it, it ends with the app or an explicit close. Back to project-bound: the
// session belongs to the project active at toggle time and parks with it.
func (m *Model) toggleFloatTermGlobal(f *floatTerm) {
	f.global = !f.global
	if f.global {
		m.host.Notify(host.Info, "terminal pinned globally — it follows you across projects")
	} else {
		m.host.Notify(host.Info, "terminal bound to this project again")
	}
}

// clampFloatTerms re-clamps every panel into the terminal bounds; called on
// every size-affecting event via applyPopupSize, so a shrunken terminal can
// never strand a panel off screen.
func (m *Model) clampFloatTerms() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	for _, f := range m.floatTerms {
		f.w = min(f.w, m.width)
		f.h = min(f.h, m.height)
		f.x = ui.ClampDelta(f.x, 0, 0, m.width-f.w)
		f.y = ui.ClampDelta(f.y, 0, 0, m.height-f.h)
	}
}

// applyFloatTermSizes pushes each panel's box interior into its host (and
// every tab's PTY), the applyPopupSize step for the panel set.
func (m *Model) applyFloatTermSizes() {
	for _, f := range m.floatTerms {
		f.inst.SetSize(paneInterior(f.w, paneChromeW), paneInterior(f.h, paneChromeH))
	}
}

// resizeFloatTerm applies one resize step (chord #774 or border drag #933) to
// a panel, floored like the popup box and clamped to the terminal.
func (m *Model) resizeFloatTerm(f *floatTerm, ddw, ddh int) {
	f.w = ui.ClampDelta(f.w, ddw, popupTermMinW, max(m.width-f.x, popupTermMinW))
	f.h = ui.ClampDelta(f.h, ddh, popupTermMinH, max(m.height-f.y, popupTermMinH))
	m.applyPopupSize()
}

// applyFloatTermResize applies one border-drag motion step to a panel: a
// right/bottom edge tracks the pointer as size, a left/top edge as position
// plus size, so the grabbed edge stays under the pointer (unlike the centered
// popup box, whose resize grows both sides, #1243).
func (m *Model) applyFloatTermResize(d *floatResizeDrag, x, y int) {
	f := d.target
	dx, dy := x-d.lastX, y-d.lastY
	d.lastX, d.lastY = x, y
	switch d.sx {
	case 1:
		f.w = ui.ClampDelta(f.w, dx, popupTermMinW, max(m.width-f.x, popupTermMinW))
	case -1:
		right := f.x + f.w
		nx := ui.ClampDelta(f.x, dx, 0, max(right-popupTermMinW, 0))
		f.x, f.w = nx, right-nx
	}
	switch d.sy {
	case 1:
		f.h = ui.ClampDelta(f.h, dy, popupTermMinH, max(m.height-f.y, popupTermMinH))
	case -1:
		bottom := f.y + f.h
		ny := ui.ClampDelta(f.y, dy, 0, max(bottom-popupTermMinH, 0))
		f.y, f.h = ny, bottom-ny
	}
	m.applyPopupSize()
}

// popupBoxRectFor returns the outer rectangle of the popup-layer box hosting
// inst: a popup split side (#1427) or a floating panel.
func (m Model) popupBoxRectFor(inst *pane.Instance) (x, y, w, h int, ok bool) {
	if f := m.floatTermFor(inst); f != nil {
		return f.x, f.y, f.w, f.h, true
	}
	if inst == nil || m.popup.inst == nil {
		return 0, 0, 0, 0, false
	}
	px, py, pw, ph := m.popupTermRect()
	wl, wr := m.popupSplitWidths()
	switch inst {
	case m.popup.inst:
		if m.popup.split == nil {
			return px, py, pw, ph, true
		}
		return px, py, wl, ph, true
	case m.popup.split:
		return px + wl, py, wr, ph, true
	}
	return 0, 0, 0, 0, false
}

// popupBoxAt resolves the topmost popup-layer box under a screen cell, walking
// the z-order from the top down (#1806): the panels above the box first, then
// the box's split side, then the panels below it.
func (m Model) popupBoxAt(x, y int) *pane.Instance {
	below, above := m.floatTermsSplit()
	if inst := floatTermAt(above, x, y); inst != nil {
		return inst
	}
	if inst := m.popupBoxSideAt(x, y); inst != nil {
		return inst
	}
	return floatTermAt(below, x, y)
}

// floatTermAt returns the host of the topmost panel of fs covering the cell.
func floatTermAt(fs []*floatTerm, x, y int) *pane.Instance {
	for i := len(fs) - 1; i >= 0; i-- {
		if f := fs[i]; inRect(x, y, f.x, f.y, f.w, f.h) {
			return f.inst
		}
	}
	return nil
}

// popupBoxSideAt returns the popup box's split side under the cell, nil when
// the box is gone or the cell lies outside it.
func (m Model) popupBoxSideAt(x, y int) *pane.Instance {
	if m.popup.inst == nil {
		return nil
	}
	px, py, pw, ph := m.popupTermRect()
	if !inRect(x, y, px, py, pw, ph) {
		return nil
	}
	wl, _ := m.popupSplitWidths()
	if m.popup.split != nil && x >= px+wl {
		return m.popup.split
	}
	return m.popup.inst
}

// popupLayerHit reports whether a screen cell lies inside any popup-layer box.
func (m Model) popupLayerHit(x, y int) bool {
	return m.popupBoxAt(x, y) != nil
}

// collapsePopupSlot empties the layer slot that held src after its content
// moved elsewhere — the #1427 collapse semantics of closePopupTab, but
// without ending any session (the tabs live on in their new box).
func (m *Model) collapsePopupSlot(src *pane.Instance) {
	switch {
	case src == m.popup.split:
		m.popup.split = nil
		m.popup.broadcast = false
		m.popup.focusRight = false
	case src == m.popup.inst && m.popup.split != nil:
		m.popup.inst = m.popup.split
		m.popup.split = nil
		m.popup.broadcast = false
		m.popup.focusRight = false
	case src == m.popup.inst:
		m.popup.inst = nil
	default:
		if f := m.floatTermFor(src); f != nil {
			m.removeFloatTermEntry(f)
		}
	}
}

// commitPopupTabTear resolves an engaged popup-layer tab drag's release
// (#1793): inside the source box nothing commits (the press already
// activated the tab); onto another layer box the tab moves there — the
// reverse tear-out direction, session live; onto free space (any spot outside
// the layer boxes, including over layout panes — floating targets only, the
// documented design decision) the tab tears out into its own floating panel.
func (m *Model) commitPopupTabTear(d *dragState, x, y int) {
	src := d.srcInst
	if src == nil || m.floatTermFor(src) == nil && src != m.popup.inst && src != m.popup.split {
		return // the source box collapsed mid-drag (session exit)
	}
	if bx, by, bw, bh, ok := m.popupBoxRectFor(src); ok && inRect(x, y, bx, by, bw, bh) {
		return
	}
	if tgt := m.popupBoxAt(x, y); tgt != nil && tgt != src {
		m.movePopupTabTo(src, d.srcTab, tgt)
		return
	}
	if f := m.floatTermFor(src); f != nil && src.TabCount() == 1 {
		// A single-tab panel's only tab dropped on free space just moves the
		// panel itself.
		f.x = ui.ClampDelta(x-f.w/2, 0, 0, max(m.width-f.w, 0))
		f.y = ui.ClampDelta(y, 0, 0, max(m.height-f.h, 0))
		m.setFloatFocus(f)
		return
	}
	m.tearOutPopupTab(src, d.srcTab, x, y)
}

// tearOutPopupTab tears tab idx out of src into a fresh floating panel at the
// drop point (#1793): the session moves live (#708 pattern) — a multi-tab
// source detaches just that tab, a single-tab source re-homes its whole host
// and its layer slot collapses (#1427 semantics). The new panel spawns at the
// source box's size, title row under the pointer, and takes the keyboard.
func (m *Model) tearOutPopupTab(src *pane.Instance, idx, x, y int) {
	w, h := m.popupSize()
	if _, _, bw, bh, ok := m.popupBoxRectFor(src); ok {
		w, h = bw, bh
	}
	var hostInst *pane.Instance
	if src.TabCount() > 1 {
		term, ok := src.DetachTerminalTab(idx)
		if !ok {
			return
		}
		floatTermSeq++
		hostInst = pane.NewDetachedTerminalHost(fmt.Sprintf("float%d", floatTermSeq), term, m.host.Config(), m.pal())
	} else {
		hostInst = src
		m.collapsePopupSlot(src)
	}
	f := &floatTerm{inst: hostInst, w: min(w, m.width), h: min(h, m.height)}
	f.x = ui.ClampDelta(x-f.w/2, 0, 0, max(m.width-f.w, 0))
	f.y = ui.ClampDelta(y, 0, 0, max(m.height-f.h, 0))
	m.floatTerms = append(m.floatTerms, f)
	m.applyPopupSize()
	m.setFloatFocus(f)
}

// movePopupTabTo moves tab idx of src into another popup-layer box, session
// live: the reverse of the tear-out. A single-tab source moves all its tabs
// (AdoptTabsFrom — DetachTerminalTab refuses the last tab) and its slot
// collapses. The target box takes the keyboard with the landed tab active.
func (m *Model) movePopupTabTo(src *pane.Instance, idx int, tgt *pane.Instance) {
	if src.TabCount() > 1 {
		term, ok := src.DetachTerminalTab(idx)
		if !ok {
			return
		}
		tgt.AddTerminalTab(term)
	} else {
		if !tgt.AdoptTabsFrom(src) {
			return
		}
		m.collapsePopupSlot(src)
	}
	m.applyPopupSize()
	if f := m.floatTermFor(tgt); f != nil {
		m.setFloatFocus(f)
		return
	}
	m.setFloatFocus(nil)
	m.setPopupFocus(tgt == m.popup.split)
}

// renderFloatTerm renders one floating panel: the popup's pane chrome, the
// global-toggle button (●/○) leading the title row, the tab bar once several
// tabs exist, and the focus border on the keyboard-owning panel.
func (m Model) renderFloatTerm(f *floatTerm) string {
	marker := floatGlobalOff
	if f.global {
		marker = floatGlobalOn
	}
	title := "TERMINAL"
	if t := f.inst.Tab(f.inst.ActiveTab()); t != nil && t.IsTerminal() {
		title = "TERMINAL — " + t.Title()
	}
	if bar, ok := m.tabBar(f.inst, f.w-paneChromeW-floatMarkerW); ok {
		title = bar
	}
	border := m.pal().Border
	if m.popup.open && m.floatFocused() == f {
		border = m.pal().BorderFocus
	}
	return paneBox(marker+title, f.inst.View(), f.w, f.h, border)
}

// popupLayerMouse routes a mouse event while the popup layer is open and no
// drag is active: a press outside every box hides the whole layer (the #1398
// toggle unit — panels have no per-panel hidden state), otherwise the box the
// z-order puts on top under the pointer handles it (#1806), and anything over
// no box at all falls through to the popup box's handler. done reports whether
// the event was consumed (always — the layer owns the mouse like it owns the
// keyboard).
func (m Model) popupLayerMouse(msg mouseEvent) (tea.Model, tea.Cmd, bool) {
	if msg.action == mousePress && !m.popupLayerHit(msg.X, msg.Y) {
		m.togglePopupTerminal()
		return m, nil, true
	}
	if f := m.floatTermFor(m.popupBoxAt(msg.X, msg.Y)); f != nil {
		return m.floatTermMouse(f, msg)
	}
	if m.popup.inst != nil {
		return m.popupTermMouse(msg)
	}
	// Motion and stray releases over a panels-only layer stay with it.
	return m, nil, true
}

// floatTermMouse routes a mouse event inside one floating panel: a press
// focuses and raises it (#1237), the border ring starts a resize drag (#933),
// the title row hosts the global button, the tab bar (activate/close/tear
// again) and the move drag (#1793); body presses anchor selections or hit the
// scrollbar/links and the wheel pages the scrollback, exactly like a popup
// box side.
func (m Model) floatTermMouse(f *floatTerm, msg mouseEvent) (tea.Model, tea.Cmd, bool) {
	term := f.inst.ActiveTerminal()
	lx, ly := msg.X-(f.x+paneContentX), msg.Y-(f.y+paneContentY)
	switch {
	case msg.action == mousePress && msg.Button == tea.MouseLeft:
		m.setFloatFocus(f)
		if zx, zy, ok := ui.ResizeZone(msg.X-f.x, msg.Y-f.y, f.w, f.h); ok {
			m.floatDrag = &floatResizeDrag{kind: "floatterm", target: f, sx: zx, sy: zy, lastX: msg.X, lastY: msg.Y}
			return m, nil, true
		}
		if msg.Y == f.y+1 {
			// The global-toggle button occupies the title row's first cells.
			if lx >= 0 && lx < floatMarkerW {
				m.toggleFloatTermGlobal(f)
				return m, nil, true
			}
			if f.inst.TabCount() > 1 || m.tabsAlwaysShow() {
				labels := tabLabels(f.inst)
				if idx, closeHit := tabHit(labels, f.inst.ActiveTab(), f.w-paneChromeW-floatMarkerW, lx-floatMarkerW); idx >= 0 {
					switch {
					case !closeHit:
						f.inst.ActivateTab(idx)
						// The press arms a tear-again drag (#1793): engaged,
						// the tab moves to another box or its own panel.
						m.drag = &dragState{kind: dragTab, srcPane: popupPaneKey, srcInst: f.inst, srcTab: idx,
							curX: msg.X, curY: msg.Y, startX: msg.X, startY: msg.Y}
					case idx == f.inst.ActiveTab():
						m.requestPopupTabClose()
					default:
						m.closePopupTab(f.inst, idx)
					}
					return m, nil, true
				}
			}
			// The title row outside any tab segment starts the move drag.
			m.floatMove = &floatMoveDrag{target: f, lastX: msg.X, lastY: msg.Y}
			return m, nil, true
		}
		if term == nil {
			return m, nil, true
		}
		if term.ScrollbarHit(lx, ly) {
			if term.ScrollbarPress(ly) {
				m.drag = &dragState{kind: dragTermScroll, srcPane: popupPaneKey, curX: msg.X, curY: msg.Y}
			}
			return m, nil, true
		}
		if msg.Mod&(tea.ModSuper|tea.ModMeta) != 0 {
			if p, line, col, ok := term.LinkAt(lx, ly); ok {
				tm, cmd := m.openPathAt(p, line, col)
				return tm, cmd, true
			}
			return m, nil, true
		}
		term.MousePress(lx, ly)
		m.drag = &dragState{kind: dragTermSelect, srcPane: popupPaneKey, curX: msg.X, curY: msg.Y}
		return m, nil, true
	case msg.action == mouseWheel && term != nil:
		lines := wheelLines * msg.ticks()
		switch msg.Button {
		case tea.MouseWheelUp:
			term.MouseWheel(lx, ly, lines)
		case tea.MouseWheelDown:
			term.MouseWheel(lx, ly, -lines)
		}
		return m, nil, true
	}
	return m, nil, true
}
