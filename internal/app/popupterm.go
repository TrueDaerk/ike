package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/keymap"
	"ike/internal/pane"
	"ike/internal/terminal"
	"ike/internal/ui"
)

// popupterm.go — the popup terminal (#1398): a quake-style floating terminal
// overlay toggled by one chord, independent of the pane layout. It hosts a
// detached pane.Instance tab host (the #983 tab machinery) that lives outside
// every registry and outside the split tree, so toggling it never touches the
// workspace layout and its sessions keep running while hidden (the same
// goroutine independence toolhide relies on). Sessions never resurrect across
// app restarts; only the resize delta persists (via ui.WinSizes).

// popupTermSizeKey is the WinSizes delta key for the popup box (#774 store).
const popupTermSizeKey = "popupterm"

// popupTermPosKey is the WinSizes key holding the popup box's position offset
// from center (#1793): a titlebar drag moves the box, and the offset persists
// through the same project-store/user-fallback cascade as the size delta
// (#1714) — offsets, not absolute cells, so the box re-clamps against the
// live terminal bounds.
const popupTermPosKey = "popupterm:pos"

// popupPaneKey is the sentinel pane key the drag machinery uses for gestures
// anchored inside the popup (termLocal and dragTerminal special-case it).
const popupPaneKey = "popup"

// Popup box geometry: fraction of the screen the box takes by default, and
// the floors a resize can never go below.
const (
	popupTermWFrac = 0.60
	popupTermHFrac = 0.55
	popupTermMinW  = 40
	popupTermMinH  = 10
)

// popupTerm is the popup terminal's state on the root model. The instance is
// created on first toggle and dropped when its last tab closes; open merely
// gates rendering and key routing. cmd+d splits the box into two side-by-side
// tab hosts (#1427): split holds the right side while it exists, focusRight
// tracks which side owns the keyboard, and broadcast (cmd+shift+i) mirrors
// typed input to both sides' active shells.
type popupTerm struct {
	inst       *pane.Instance // primary (left) detached tab host; nil until first use
	split      *pane.Instance // right split host (#1427); nil while unsplit
	focusRight bool           // keyboard owner: false = inst, true = split
	broadcast  bool           // cmd+shift+i (#1427): input mirrors to both sides
	open       bool
}

// popupSessSeq mints "popup:term:N" session keys. Package-level, not
// per-model (#1793): a global floating terminal rides across the fresh model
// a project switch builds, so per-model counters would collide its session
// key with the new project's popup sessions — and session-key lookups
// (popupTabForSession, terminalModelForSession) must stay unambiguous.
var popupSessSeq int

// instances returns the popup's live tab hosts, primary first — one entry
// while unsplit, two while split (#1427). Callers that must touch every popup
// shell (theme, quit, teardown, activity) iterate this instead of inst.
func (p popupTerm) instances() []*pane.Instance {
	out := make([]*pane.Instance, 0, 2)
	if p.inst != nil {
		out = append(out, p.inst)
	}
	if p.split != nil {
		out = append(out, p.split)
	}
	return out
}

// togglePopupTerminal shows or hides the popup terminal layer — the popup box
// plus every floating panel (#1793) — as one unit (terminal.popup). The first
// show with nothing retained spawns a shell; later shows reveal the retained
// boxes unchanged (a layer whose box was fully torn out reveals just its
// panels). Pane focus is never moved — while the layer is open the key funnel
// routes to it before any pane, and hiding it falls back to the focused pane.
func (m *Model) togglePopupTerminal() {
	if m.popup.open {
		m.popup.open = false
		for _, inst := range m.popupLayerInstances() {
			inst.SetFocused(false)
		}
		return
	}
	if m.popup.inst == nil && len(m.floatTerms) == 0 {
		term := m.newPopupShell()
		m.popup.inst = pane.NewDetachedTerminalHost("popup", term, m.host.Config(), m.pal())
	}
	m.popup.open = true
	m.applyPopupSize()
	if f := m.floatFocused(); f != nil {
		m.setFloatFocus(f)
		return
	}
	m.setPopupFocus(m.popup.focusRight)
}

// popupFocused resolves the popup-layer host that owns the keyboard: the
// focused floating panel when one holds it (#1793), else the split side —
// the right host while it exists and holds focus, the primary otherwise
// (#1427).
func (m Model) popupFocused() *pane.Instance {
	if f := m.floatFocused(); f != nil {
		return f.inst
	}
	if m.popup.focusRight && m.popup.split != nil {
		return m.popup.split
	}
	return m.popup.inst
}

// setPopupFocus moves keyboard ownership to one split side (#1427); right is
// coerced to the primary while no split exists.
func (m *Model) setPopupFocus(right bool) {
	m.popup.focusRight = right && m.popup.split != nil
	if m.popup.inst != nil {
		m.popup.inst.SetFocused(!m.popup.focusRight)
	}
	if m.popup.split != nil {
		m.popup.split.SetFocused(m.popup.focusRight)
	}
}

// splitPopupTerminal splits the popup box into two side-by-side shells (#1427,
// reserved cmd+d inside the popup — the same chord that splits a terminal
// pane, #982). Only one split is supported; the fresh right side takes focus.
// Box-only: while a floating panel owns the keyboard the chord is a no-op.
func (m *Model) splitPopupTerminal() {
	if m.popup.inst == nil || m.popup.split != nil || m.floatFocused() != nil {
		return
	}
	term := m.newPopupShell()
	m.popup.split = pane.NewDetachedTerminalHost("popup2", term, m.host.Config(), m.pal())
	m.applyPopupSize()
	m.setPopupFocus(true)
}

// popupInputTerminals returns the shells typed input goes to: both box sides'
// active terminals under broadcast (#1427), otherwise the focused host's only
// — broadcast is a box affair, so a focused floating panel (#1793) always
// receives alone.
func (m Model) popupInputTerminals() []*terminal.Model {
	insts := []*pane.Instance{m.popupFocused()}
	if m.floatFocused() == nil && m.popup.broadcast && m.popup.split != nil {
		insts = m.popup.instances()
	}
	out := make([]*terminal.Model, 0, len(insts))
	for _, inst := range insts {
		if inst == nil {
			continue
		}
		if t := inst.ActiveTerminal(); t != nil {
			out = append(out, t)
		}
	}
	return out
}

// newPopupShell spawns a fresh shell session for the popup, mirroring the
// pane-terminal creation recipe (shell config, toolchain env, host injector).
func (m *Model) newPopupShell() terminal.Model {
	shell := ""
	if v, ok := m.host.Config().Get("terminal.shell"); ok {
		shell = v
	}
	popupSessSeq++
	key := fmt.Sprintf("popup:term:%d", popupSessSeq)
	return terminal.New(key, terminal.Shell(shell), ".", 80, 24, terminalEnv(), m.host.Send)
}

// newPopupTerminalTab opens a sibling shell tab inside the popup (cmd+t
// there), on the focused split side (#1427).
func (m *Model) newPopupTerminalTab() {
	inst := m.popupFocused()
	if inst == nil {
		return
	}
	inst.AddTerminalTab(m.newPopupShell())
	m.applyPopupSize()
}

// popupTermDelta resolves the popup box's resize delta through the #1714
// cascade: the project's own delta wins, the user-scoped last-resize delta is
// the fallback for a project that was never resized, and no store at all means
// the plain default fractions. Deltas — not absolute sizes — travel between
// projects, so the box keeps re-clamping against the live terminal bounds.
func (m Model) popupTermDelta() (dw, dh int) {
	if m.winSizes.Has(popupTermSizeKey) {
		return m.winSizes.Get(popupTermSizeKey)
	}
	return m.winSizesAll.Get(popupTermSizeKey)
}

// popupSize computes the popup's outer box size: screen fractions capped by
// ui.popup_max_width, adjusted by the resolved resize delta, floored so the
// box never degenerates.
func (m Model) popupSize() (w, h int) {
	w = int(float64(m.width) * popupTermWFrac)
	h = int(float64(m.height) * popupTermHFrac)
	if cap := popupMaxWidth(); cap > 0 && w > cap {
		w = cap
	}
	maxW, maxH := m.width-2, m.height-2
	dw, dh := m.popupTermDelta()
	w = ui.ClampDelta(w, dw, popupTermMinW, maxW)
	h = ui.ClampDelta(h, dh, popupTermMinH, maxH)
	return w, h
}

// popupTermSeed copies the user-scoped delta into the project store when the
// project carries none, so the first resize in a project that merely inherited
// the fallback continues from the size on screen instead of jumping back to
// the default (#1714).
func (m *Model) popupTermSeed() {
	if m.winSizes.Has(popupTermSizeKey) {
		return
	}
	dw, dh := m.winSizesAll.Get(popupTermSizeKey)
	m.winSizes.Set(popupTermSizeKey, dw, dh)
}

// popupTermResize applies one resize step to the popup box (chord #774 or
// mouse drag #933) and re-sizes the hosted PTYs. persist=false is the mid-drag
// step, where writing the stores per motion event would be waste — the drag's
// release calls popupTermPersist instead.
func (m *Model) popupTermResize(ddw, ddh int, persist bool) {
	m.popupTermSeed()
	m.winSizes.Nudge(popupTermSizeKey, ddw, ddh)
	if persist {
		m.popupTermPersist()
	}
	m.applyPopupSize()
}

// popupTermPersist writes the popup delta to the project store and mirrors it
// into the user-scoped store, so the size just chosen becomes the fallback for
// every project that has none of its own (#1714).
func (m *Model) popupTermPersist() {
	m.winSizes.Flush()
	dw, dh := m.winSizes.Get(popupTermSizeKey)
	m.winSizesAll.Set(popupTermSizeKey, dw, dh)
}

// popupTermPos resolves the popup box's position offset from center (#1793)
// through the same #1714 cascade as the size delta: the project's own offset
// wins, the user-scoped one is the fallback for a project never moved.
func (m Model) popupTermPos() (dx, dy int) {
	if m.winSizes.Has(popupTermPosKey) {
		return m.winSizes.Get(popupTermPosKey)
	}
	return m.winSizesAll.Get(popupTermPosKey)
}

// popupTermMoveBy applies one titlebar-drag move step to the popup box
// (#1793), seeding the project store from the user fallback first (the #1714
// continue-from-what-you-see rule). persist=false is the mid-drag step; the
// drag's release calls popupTermPersistPos.
func (m *Model) popupTermMoveBy(ddx, ddy int, persist bool) {
	if !m.winSizes.Has(popupTermPosKey) {
		dx, dy := m.winSizesAll.Get(popupTermPosKey)
		m.winSizes.Set(popupTermPosKey, dx, dy)
	}
	m.winSizes.Nudge(popupTermPosKey, ddx, ddy)
	if persist {
		m.popupTermPersistPos()
	}
}

// popupTermPersistPos writes the popup position offset to the project store
// and mirrors it into the user-scoped store, like popupTermPersist does for
// the size delta (#1714).
func (m *Model) popupTermPersistPos() {
	m.winSizes.Flush()
	dx, dy := m.winSizes.Get(popupTermPosKey)
	m.winSizesAll.Set(popupTermPosKey, dx, dy)
}

// popupSplitWidths returns the outer widths of the left and right boxes: the
// full box width (and 0) while unsplit, halves while split (#1427) — the left
// side takes the floor so both sides sum to the box width exactly.
func (m Model) popupSplitWidths() (wl, wr int) {
	w, _ := m.popupSize()
	if m.popup.split == nil {
		return w, 0
	}
	wl = w / 2
	return wl, w - wl
}

// applyPopupSize pushes the current box interior into the instance (and every
// tab's PTY). The popup lives outside the layout tree, so layout() never sizes
// it — every size-affecting event calls this instead. While split (#1427) each
// side gets its half's interior; the floating panels (#1793) re-clamp and
// re-size in the same pass.
func (m *Model) applyPopupSize() {
	if m.popup.inst != nil {
		_, h := m.popupSize()
		wl, wr := m.popupSplitWidths()
		m.popup.inst.SetSize(paneInterior(wl, paneChromeW), paneInterior(h, paneChromeH))
		if m.popup.split != nil {
			m.popup.split.SetSize(paneInterior(wr, paneChromeW), paneInterior(h, paneChromeH))
		}
	}
	m.clampFloatTerms()
	m.applyFloatTermSizes()
}

// popupTermRect is the popup box's screen rectangle: centered geometry plus
// the persisted move offset (#1793), clamped so the box always stays fully on
// screen.
func (m Model) popupTermRect() (x, y, w, h int) {
	w, h = m.popupSize()
	dx, dy := m.popupTermPos()
	x = ui.ClampDelta((m.width-w)/2, dx, 0, max(m.width-w, 0))
	y = ui.ClampDelta((m.height-h)/2, dy, 0, max(m.height-h, 0))
	return x, y, w, h
}

// renderPopupTerm renders the popup box: pane-style chrome (rounded border,
// title row) whose title row is the tab bar once multiple tabs exist, exactly
// like an editor pane hosting terminal tabs. While split (#1427) the two side
// boxes join horizontally; only the focused side gets the focus border —
// except under broadcast (#1592), where both sides render it because both
// receive the typed input.
func (m Model) renderPopupTerm() string {
	_, h := m.popupSize()
	wl, wr := m.popupSplitWidths()
	// A focused floating panel (#1793) owns the keyboard, so no box side
	// renders the focus border then.
	boxFocus := m.floatFocused() == nil
	if m.popup.split == nil {
		return m.renderPopupSide(m.popup.inst, wl, h, boxFocus)
	}
	left := m.renderPopupSide(m.popup.inst, wl, h, boxFocus && (!m.popup.focusRight || m.popup.broadcast))
	right := m.renderPopupSide(m.popup.split, wr, h, boxFocus && (m.popup.focusRight || m.popup.broadcast))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// renderPopupSide renders one split side's pane box. Under broadcast (#1427)
// a ⇉ marker prefixes the title row of both sides so it is obvious that
// keystrokes go to every shell.
func (m Model) renderPopupSide(inst *pane.Instance, w, h int, focused bool) string {
	marker := ""
	if m.popup.broadcast && m.popup.split != nil {
		marker = "⇉ "
	}
	title := "POPUP TERMINAL"
	if t := inst.Tab(inst.ActiveTab()); t != nil && t.IsTerminal() {
		title = "POPUP TERMINAL — " + t.Title()
	}
	if bar, ok := m.tabBar(inst, w-paneChromeW-lipgloss.Width(marker)); ok {
		title = bar
	}
	border := m.pal().Border
	if focused {
		border = m.pal().BorderFocus
	}
	return paneBox(marker+title, inst.View(), w, h, border)
}

// popupTabForSession resolves a session key to the popup-layer tab hosting
// it — either split side (#1427) or a floating panel (#1793); returns the
// hosting instance, the tab index and its terminal model, or (nil, -1, nil).
func (m Model) popupTabForSession(sess string) (*pane.Instance, int, *terminal.Model) {
	for _, inst := range m.popupLayerInstances() {
		for i := 0; i < inst.TabCount(); i++ {
			if t := inst.TabTerminal(i); t != nil && t.SessionKey() == sess {
				return inst, i, t
			}
		}
	}
	return nil, -1, nil
}

// dragTerminal resolves a drag's source pane key to the live terminal a
// selection or scrollbar gesture targets: the popup's active shell for the
// popup sentinel, the pane's active terminal otherwise.
func (m Model) dragTerminal(key string) *terminal.Model {
	if key == popupPaneKey {
		// The focused split side (#1427): a press focuses its side before the
		// drag starts, so the anchor and the drag resolve the same shell.
		if inst := m.popupFocused(); inst != nil {
			return inst.ActiveTerminal()
		}
		return nil
	}
	if inst := m.activeWS().Panes.Get(key); inst != nil {
		return inst.ActiveTerminal()
	}
	return nil
}

// popupTermMouse routes a mouse event over the popup box while the layer is
// open and no drag is active (presses outside every layer box and presses on
// floating panels are settled by popupLayerMouse first): border press starts
// a resize drag, the title row hosts the tab bar (activate/close/tear-out
// #1793) and the move drag, body presses anchor selections or hit the
// scrollbar/links, the wheel pages the scrollback. Motion and release with an
// active drag never reach this — the generic drag machinery owns them. done
// reports whether the event was consumed.
func (m Model) popupTermMouse(msg mouseEvent) (tea.Model, tea.Cmd, bool) {
	px, py, pw, ph := m.popupTermRect()
	// Resolve the split side under the pointer (#1427): the x offset picks the
	// box; unsplit, the primary spans the whole popup.
	wl, wr := m.popupSplitWidths()
	inst, sx, sw, right := m.popup.inst, px, wl, false
	if m.popup.split != nil && msg.X >= px+wl {
		inst, sx, sw, right = m.popup.split, px+wl, wr, true
	}
	term := inst.ActiveTerminal()
	lx, ly := msg.X-(sx+paneContentX), msg.Y-(py+paneContentY)
	switch {
	case msg.action == mousePress && msg.Button == tea.MouseLeft:
		// The border ring starts a mouse resize (#933), centered geometry.
		if zx, zy, ok := ui.ResizeZone(msg.X-px, msg.Y-py, pw, ph); ok {
			m.floatDrag = &floatResizeDrag{kind: "popupterm", sx: zx, sy: zy, lastX: msg.X, lastY: msg.Y}
			return m, nil, true
		}
		// A press on the box reclaims the keyboard from a floating panel
		// (#1793) and claims it for its split side (#1427), like pane focus
		// follows clicks.
		if m.floatFocus != nil {
			m.setFloatFocus(nil)
		}
		if right != m.popup.focusRight {
			m.setPopupFocus(right)
		}
		// Title row: tab bar (#157/#1128) — a segment click activates and
		// arms the tear-out drag (#1793), its ✕ closes (the active tab
		// through the busy guard, others directly) — or, outside every
		// segment, the box move drag (#1793).
		if msg.Y == py+1 {
			if inst.TabCount() > 1 || m.tabsAlwaysShow() {
				labels := tabLabels(inst)
				// The broadcast marker (#1427) prefixes the title row, shifting
				// the bar right by its width — mirror that in the hit test.
				bx := lx
				if m.popup.broadcast && m.popup.split != nil {
					bx -= lipgloss.Width("⇉ ")
				}
				if idx, closeHit := tabHit(labels, inst.ActiveTab(), sw-paneChromeW, bx); idx >= 0 {
					switch {
					case !closeHit:
						inst.ActivateTab(idx)
						// Engaged, the drag tears the tab out of the box —
						// into another layer box or its own floating panel.
						m.drag = &dragState{kind: dragTab, srcPane: popupPaneKey, srcInst: inst, srcTab: idx,
							curX: msg.X, curY: msg.Y, startX: msg.X, startY: msg.Y}
					case idx == inst.ActiveTab():
						m.requestPopupTabClose()
					default:
						m.closePopupTab(inst, idx)
					}
					return m, nil, true
				}
			}
			m.floatMove = &floatMoveDrag{lastX: msg.X, lastY: msg.Y}
			return m, nil, true
		}
		if term == nil {
			return m, nil, true
		}
		// Scrollback scrollbar (#1368) outranks links and selection.
		if term.ScrollbarHit(lx, ly) {
			if term.ScrollbarPress(ly) {
				m.drag = &dragState{kind: dragTermScroll, srcPane: popupPaneKey, curX: msg.X, curY: msg.Y}
			}
			return m, nil, true
		}
		// cmd+click opens a file:line reference (#1168).
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
		// Wheel like a terminal pane (#226): mouse-reporting children get the
		// event, a plain shell pages the scrollback.
		lines := wheelLines * msg.ticks()
		switch msg.Button {
		case tea.MouseWheelUp:
			term.MouseWheel(lx, ly, lines)
		case tea.MouseWheelDown:
			term.MouseWheel(lx, ly, -lines)
		}
		return m, nil, true
	}
	// Motion (hover) and stray releases stay with the popup — they must not
	// leak into the panes underneath.
	return m, nil, true
}

// popupChordCommand resolves a single-step chord against the live binding
// table under the terminal context, so rebinds move the reserved popup chords
// along (the same rule terminalGlobalChord follows).
func (m Model) popupChordCommand(keys string) string {
	table := m.bindings.Table()
	if table == nil {
		return ""
	}
	k, err := keymap.ParseKey(keys)
	if err != nil {
		return ""
	}
	if b, found := table.Lookup(keymap.Chord{Steps: []keymap.Key{k}}, keymap.Context("terminal")); found {
		return b.Command
	}
	return ""
}

// popupReservedKey is the reserved chord set while the popup terminal owns the
// keyboard, mirroring terminalReservedKey: the toggle chord hides the popup
// from inside, cmd+t opens a sibling tab, tab-cycling and cmd+w act on the
// popup's tabs, and the floating-window resize chords (#774) adjust the box.
// Everything else belongs to the popup's shell.
func (m Model) popupReservedKey(keys string) (bool, tea.Model, tea.Cmd) {
	if k, err := keymap.ParseKey(keys); err == nil {
		keys = k.String()
	}
	if !terminalShellChords[keys] {
		switch m.popupChordCommand(keys) {
		case "terminal.popup":
			m.togglePopupTerminal()
			return true, m, nil
		case "editor.tab.next":
			m.cyclePopupTab(1)
			return true, m, nil
		case "editor.tab.prev":
			m.cyclePopupTab(-1)
			return true, m, nil
		}
	}
	switch keys {
	case "cmd+t":
		// iTerm-style sibling tab, like the pane-terminal reserved cmd+t.
		m.newPopupTerminalTab()
		return true, m, nil
	case "cmd+d":
		// iTerm-style split (#1427): two side-by-side shells, like the
		// pane-terminal reserved cmd+d (#982). A second cmd+d is a no-op.
		m.splitPopupTerminal()
		return true, m, nil
	case "cmd+shift+i":
		// Broadcast toggle (#1427): while on, typed input mirrors to both
		// split sides' active shells. Meaningless unsplit or while a floating
		// panel owns the keyboard (#1793) — ignored then.
		if m.popup.split != nil && m.floatFocused() == nil {
			m.popup.broadcast = !m.popup.broadcast
		}
		return true, m, nil
	case "ctrl+tab":
		m.cyclePopupTab(1)
		return true, m, nil
	case "cmd+w":
		m.requestPopupTabClose()
		return true, m, nil
	case "cmd+f":
		// cmd+f opens the scrollback search (#1504) on the focused split
		// side, like the pane-terminal reserved cmd+f. Under an alt-screen
		// or mouse-reporting child the chord stays with the child.
		if inst := m.popupFocused(); inst != nil {
			if term := inst.ActiveTerminal(); term != nil && term.StartSearch() {
				return true, m, nil
			}
		}
		return false, m, nil
	}
	// The spatial focus keys (default ctrl+left/right, #228 overrides apply)
	// move the keyboard between the split sides (#1427) while the box owns
	// it; unsplit (or with a floating panel focused, #1793) they stay with
	// the shell like every other unreserved key.
	if m.popup.split != nil && m.floatFocused() == nil {
		if dir, ok := m.focusKeys[keys]; ok && (dir == DirLeft || dir == DirRight) {
			m.setPopupFocus(dir == DirRight)
			return true, m, nil
		}
	}
	// The resize chords (#774) act on the keyboard-owning surface: a focused
	// floating panel (#1793) resizes in place, the box otherwise.
	if ddw, ddh, ok := ui.ResizeDelta(keys); ok {
		if f := m.floatFocused(); f != nil {
			m.resizeFloatTerm(f, ddw, ddh)
		} else {
			m.popupTermResize(ddw, ddh, true)
		}
		return true, m, nil
	}
	return false, m, nil
}

// cyclePopupTab activates the next/previous tab on the focused split side,
// wrapping around.
func (m *Model) cyclePopupTab(step int) {
	inst := m.popupFocused()
	if inst == nil || inst.TabCount() < 2 {
		return
	}
	n := inst.TabCount()
	inst.ActivateTab(((inst.ActiveTab()+step)%n + n) % n)
}

// requestPopupTabClose handles the reserved cmd+w inside the popup (#986
// semantics): an idle shell gets an EOF — the exit path closes its tab — and
// a busy one raises the confirmation guard targeted at the popup.
func (m *Model) requestPopupTabClose() {
	inst := m.popupFocused()
	if inst == nil {
		return
	}
	term := inst.ActiveTerminal()
	if term == nil {
		return
	}
	if term.Busy() {
		m.termClosePopup = true
		m.termCloseSess = term.SessionKey()
		m.openTermClosePrompt()
		return
	}
	term.SendEOF()
}

// closePopupTab closes tab idx on the given popup-layer host (session ends).
// Closing a side's last tab collapses the split back to a single box (#1427);
// a floating panel's last tab closes the panel (#1793); closing the last tab
// of an unsplit popup drops the whole instance and hides the popup — the next
// toggle starts a fresh shell, mirroring terminal.toggle's create-on-demand
// arm.
func (m *Model) closePopupTab(inst *pane.Instance, idx int) {
	if inst == nil {
		return
	}
	if inst.TabCount() > 1 {
		inst.CloseTab(idx)
		return
	}
	if f := m.floatTermFor(inst); f != nil {
		inst.CloseTerminalTabs()
		m.removeFloatTerm(f)
		return
	}
	inst.CloseTerminalTabs()
	switch {
	case inst == m.popup.split:
		// The right side emptied: the primary spans the box again. A focused
		// floating panel keeps the keyboard (the exit may come from any
		// session, #1793); setFloatFocus settles either way.
		m.popup.split = nil
		m.popup.broadcast = false
		m.popup.focusRight = false
		m.setFloatFocus(m.floatFocus)
		m.applyPopupSize()
	case m.popup.split != nil:
		// The primary emptied while split: the right side is promoted.
		m.popup.inst = m.popup.split
		m.popup.split = nil
		m.popup.broadcast = false
		m.popup.focusRight = false
		m.setFloatFocus(m.floatFocus)
		m.applyPopupSize()
	default:
		m.popup.inst = nil
		if len(m.floatTerms) > 0 {
			// Floating panels remain (#1793): the layer stays open and the
			// topmost panel takes the keyboard.
			m.setFloatFocus(m.floatTerms[len(m.floatTerms)-1])
			return
		}
		m.popup.open = false
	}
}
