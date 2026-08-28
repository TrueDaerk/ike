package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/project"
	"ike/internal/terminal"
)

// floatterm_test.go — movable floating terminal panels (#1793): the popup
// box's titlebar move with persisted position, tab tear-out into panels with
// the live session, z-order/focus among several panels, and the global
// toggle's cross-project lifecycle.

// tearOutFirstTab opens a second popup tab and tears the first one out via
// the full mouse gesture (tab press arms the drag, motion engages, release on
// free space commits), returning the model and the new panel.
func tearOutFirstTab(t *testing.T, m Model) (Model, *floatTerm) {
	t.Helper()
	m.newPopupTerminalTab()
	if m.popup.inst.TabCount() != 2 {
		t.Fatal("test setup: popup should hold two tabs")
	}
	sess := m.popup.inst.TabTerminal(0).SessionKey()
	px, py, _, ph := m.popupTermRect()
	m = step(m, press(px+paneContentX+1, py+1))
	if m.drag == nil || m.drag.kind != dragTab || m.drag.srcInst != m.popup.inst {
		t.Fatal("a tab press must arm the popup tear-out drag")
	}
	m = step(m, motion(px+10, py+ph+3))
	m = step(m, release(px+10, py+ph+3))
	if len(m.floatTerms) != 1 {
		t.Fatalf("the release on free space must tear the tab out, got %d panels", len(m.floatTerms))
	}
	f := m.floatTerms[0]
	if got := f.inst.ActiveTerminal().SessionKey(); got != sess {
		t.Fatalf("the panel must host the dragged live session %q, got %q", sess, got)
	}
	t.Cleanup(func() { f.inst.CloseTerminalTabs() })
	return m, f
}

func TestPopupMoveDragPersistsPosition(t *testing.T) {
	m := openTestPopup(t)
	x0, y0, _, _ := m.popupTermRect()
	px, py, pw, _ := m.popupTermRect()
	m = step(m, press(px+pw/2, py+1)) // title row, single tab: the move drag
	if m.floatMove == nil || m.floatMove.target != nil {
		t.Fatal("a titlebar press must start the popup box move drag")
	}
	m = step(m, motion(px+pw/2+5, py+1+3))
	m = step(m, release(px+pw/2+5, py+1+3))
	if m.floatMove != nil {
		t.Fatal("the release must end the move drag")
	}
	x1, y1, _, _ := m.popupTermRect()
	if x1 != x0+5 || y1 != y0+3 {
		t.Fatalf("the box must follow the drag: want %d,%d got %d,%d", x0+5, y0+3, x1, y1)
	}
	// The offset persists like the size delta (#774/#1714): project store
	// entry plus the user-scoped mirror.
	if !m.winSizes.Has(popupTermPosKey) {
		t.Fatal("the release must persist the position offset in the project store")
	}
	if dx, dy := m.winSizes.Get(popupTermPosKey); dx != 5 || dy != 3 {
		t.Fatalf("persisted offset = %d,%d, want 5,3", dx, dy)
	}
	if dx, dy := m.winSizesAll.Get(popupTermPosKey); dx != 5 || dy != 3 {
		t.Fatalf("user-scoped mirror = %d,%d, want 5,3", dx, dy)
	}
}

func TestPopupMoveOffsetClampsToScreen(t *testing.T) {
	m := openTestPopup(t)
	m.winSizes.Set(popupTermPosKey, 10_000, 10_000)
	x, y, w, h := m.popupTermRect()
	if x+w > m.width || y+h > m.height || x < 0 || y < 0 {
		t.Fatalf("a huge stored offset must re-clamp on screen, got rect %d,%d %dx%d", x, y, w, h)
	}
}

func TestTearOutTabKeepsSessionLiveAndFocusesPanel(t *testing.T) {
	m := openTestPopup(t)
	m, f := tearOutFirstTab(t, m)
	if m.popup.inst.TabCount() != 1 {
		t.Fatalf("the source box must shed the torn tab, got %d tabs", m.popup.inst.TabCount())
	}
	if !f.inst.ActiveTerminal().Running() {
		t.Fatal("the torn-out session must keep running — no shell restart")
	}
	if m.floatFocused() != f || m.popupFocused() != f.inst {
		t.Fatal("the fresh panel must take the keyboard")
	}
	if v := m.render(); !strings.Contains(v, floatGlobalOff) {
		t.Fatal("the panel must render with the project-owned global toggle (○)")
	}
	// The panel's session stays resolvable for output/exit routing.
	sess := f.inst.ActiveTerminal().SessionKey()
	if m.terminalModelForSession(sess) == nil {
		t.Fatal("panel sessions must resolve via terminalModelForSession")
	}
}

func TestTearOutLastTabMovesWholeHost(t *testing.T) {
	m := openTestPopup(t)
	inst := m.popup.inst
	sess := inst.ActiveTerminal().SessionKey()
	m.tearOutPopupTab(inst, 0, 30, 20)
	if m.popup.inst != nil {
		t.Fatal("tearing out the box's only tab must collapse the box slot")
	}
	if len(m.floatTerms) != 1 || m.floatTerms[0].inst != inst {
		t.Fatal("the single-tab source must re-home its whole host into the panel")
	}
	if got := m.floatTerms[0].inst.ActiveTerminal().SessionKey(); got != sess {
		t.Fatalf("session must move unchanged, got %q want %q", got, sess)
	}
	if !m.popup.open {
		t.Fatal("the layer must stay open — a panel still shows")
	}
	if m.popupFocused() != inst {
		t.Fatal("the panel keeps the keyboard with the box gone")
	}
}

// twoFloatPanels tears two tabs out of the popup box into panels placed side
// by side (so either is clickable), returning the model and the panels in
// stack order — the second one owns the keyboard.
func twoFloatPanels(t *testing.T, m Model) (Model, *floatTerm, *floatTerm) {
	t.Helper()
	m.newPopupTerminalTab()
	m.newPopupTerminalTab()
	m.tearOutPopupTab(m.popup.inst, 0, 10, 5)
	m.tearOutPopupTab(m.popup.inst, 0, 40, 5)
	if len(m.floatTerms) != 2 {
		t.Fatalf("test setup: want two panels, got %d", len(m.floatTerms))
	}
	lower, upper := m.floatTerms[0], m.floatTerms[1]
	t.Cleanup(func() { lower.inst.CloseTerminalTabs(); upper.inst.CloseTerminalTabs() })
	// Separate the panels so the lower one is clickable.
	lower.x, lower.y, lower.w, lower.h = 0, 2, 46, 12
	upper.x, upper.y, upper.w, upper.h = 50, 2, 46, 12
	if m.floatFocused() != upper {
		t.Fatal("test setup: the last torn-out panel owns the keyboard")
	}
	return m, lower, upper
}

// topFloatPanel returns the topmost panel of the z-order — the one the
// compositor draws last.
func topFloatPanel(m Model) *floatTerm {
	if len(m.floatTerms) == 0 {
		return nil
	}
	return m.floatTerms[len(m.floatTerms)-1]
}

func TestFloatPanelClickRaisesAndFocuses(t *testing.T) {
	m, lower, _ := twoFloatPanels(t, openTestPopup(t))
	m = step(m, press(lower.x+10, lower.y+5))
	m = step(m, release(lower.x+10, lower.y+5)) // settle the selection drag the body press armed
	if m.floatTerms[len(m.floatTerms)-1] != lower {
		t.Fatal("a click must raise the panel to the top of the z-order")
	}
	if m.floatFocused() != lower || m.popupFocused() != lower.inst {
		t.Fatal("a click must focus the panel")
	}
	// A click on the popup box hands the keyboard back to it.
	px, py, pw, ph := m.popupTermRect()
	m = step(m, press(px+pw/2, py+ph/2))
	m = step(m, release(px+pw/2, py+ph/2))
	if m.floatFocused() != nil || m.popupFocused() != m.popup.inst {
		t.Fatal("a box click must reclaim the keyboard from the panels")
	}
}

// TestFloatPanelKeyFocusRaises: the spatial focus keys step through the layer
// (#1806) — the newly focused panel is raised to the top of the z-order, so
// keyboard owner and topmost panel always agree (#1237).
func TestFloatPanelKeyFocusRaises(t *testing.T) {
	m, lower, upper := twoFloatPanels(t, openTestPopup(t))
	// Down the stack: the second-topmost panel takes the keyboard and rises.
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.floatFocused() != lower || m.popupFocused() != lower.inst {
		t.Fatal("the focus key must move the keyboard to the other panel")
	}
	if topFloatPanel(m) != lower {
		t.Fatal("the keyboard switch must raise the focused panel to the top of the z-order")
	}
	// Another step alternates back, raising again — alt-tab style.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.floatFocused() != upper || topFloatPanel(m) != upper {
		t.Fatal("the next step must focus and raise the other panel again")
	}
	// Up the stack the ring wraps onto the box, which keeps its base layer.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.floatFocused() != nil || m.popupFocused() != m.popup.inst {
		t.Fatal("stepping past the topmost panel must hand the keyboard to the box")
	}
	// And from the box back onto the bottom panel, which rises with the focus.
	bottom := m.floatTerms[0]
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.floatFocused() != bottom || topFloatPanel(m) != bottom {
		t.Fatal("stepping off the box must focus and raise the next panel")
	}
}

// TestFloatFocusInvariantAcrossSwitches: mixed click/keyboard switches in any
// order keep the #1237 invariant — the topmost panel owns the keyboard.
func TestFloatFocusInvariantAcrossSwitches(t *testing.T) {
	m, lower, upper := twoFloatPanels(t, openTestPopup(t))
	check := func(what string) {
		t.Helper()
		if f := m.floatFocused(); f != nil && topFloatPanel(m) != f {
			t.Fatalf("%s: the keyboard-owning panel must be topmost", what)
		}
	}
	click := func(f *floatTerm) {
		t.Helper()
		m = step(m, press(f.x+10, f.y+5))
		m = step(m, release(f.x+10, f.y+5))
	}
	key := func(code rune) {
		t.Helper()
		out, _ := m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
		m = out.(Model)
	}
	click(lower)
	check("click on the lower panel")
	key(tea.KeyLeft)
	check("focus key after a click")
	click(upper)
	check("click back onto the other panel")
	key(tea.KeyLeft)
	key(tea.KeyLeft)
	check("repeated focus keys")
	// The box in between must not disturb it either.
	px, py, pw, ph := m.popupTermRect()
	m = step(m, press(px+pw/2, py+ph/2))
	m = step(m, release(px+pw/2, py+ph/2))
	if m.floatFocused() != nil {
		t.Fatal("a box click must reclaim the keyboard")
	}
	key(tea.KeyRight)
	check("focus key from the box")
}

// TestPopupBoxFocusKeyRaisesBox: the popup box is a layer surface of its own
// (#1806), not a fixed base layer — a keyboard focus switch onto it lifts it
// over a panel that covered it, and the next step lowers it again under the
// panel it hands the keyboard to.
func TestPopupBoxFocusKeyRaisesBox(t *testing.T) {
	m := openTestPopup(t)
	m, f := tearOutFirstTab(t, m)
	px, py, pw, ph := m.popupTermRect()
	// The panel covers the box completely: while it is on top, none of the
	// box's chrome reaches the screen.
	f.x, f.y, f.w, f.h = px-2, py-1, pw+4, ph+2
	if strings.Contains(m.render(), "POPUP TERMINAL") {
		t.Fatal("test setup: the covering panel must hide the box's chrome")
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.floatFocused() != nil || m.popupFocused() != m.popup.inst {
		t.Fatal("the focus key must hand the keyboard to the popup box")
	}
	if m.popupBoxZ() != len(m.floatTerms) {
		t.Fatal("focusing the box must raise it to the top of the layer's z-order")
	}
	if !strings.Contains(m.render(), "POPUP TERMINAL") {
		t.Fatal("the raised box must render above the panel")
	}
	if m.popupBoxAt(px+pw/2, py+ph/2) != m.popup.inst {
		t.Fatal("the raised box must own the mouse where it overlaps the panel")
	}
	// Stepping on lands back on the panel, which rises over the box again.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.floatFocused() != f || m.popupBoxZ() != 0 {
		t.Fatal("stepping onto the panel must raise it over the box")
	}
	if strings.Contains(m.render(), "POPUP TERMINAL") {
		t.Fatal("the topmost panel must cover the box again")
	}
}

// TestPopupBoxClickRaisesBox: the same raise on a mouse focus switch — a click
// on the box lifts it over the panels, a click back on a panel lifts that one,
// and the layer's dismiss keeps working after the reordering (#1806).
func TestPopupBoxClickRaisesBox(t *testing.T) {
	m := openTestPopup(t)
	m, f := tearOutFirstTab(t, m)
	px, py, pw, ph := m.popupTermRect()
	// The panel covers the box's right half and juts out to the right, so both
	// surfaces keep a clickable spot of their own.
	f.x, f.y, f.w, f.h = px+pw/2, py, pw/2+10, ph
	overlapX, overlapY := px+pw/2+2, py+3
	if m.popupBoxAt(overlapX, overlapY) != f.inst {
		t.Fatal("test setup: the fresh panel must be topmost in the overlap")
	}
	click := func(x, y int) {
		t.Helper()
		m = step(m, press(x, y))
		m = step(m, release(x, y)) // settle the selection drag the body press armed
	}
	click(px+pw/4, py+ph/2) // the box's uncovered left half
	if m.floatFocused() != nil || m.popupFocused() != m.popup.inst {
		t.Fatal("a box click must reclaim the keyboard from the panel")
	}
	if m.popupBoxZ() != len(m.floatTerms) || m.popupBoxAt(overlapX, overlapY) != m.popup.inst {
		t.Fatal("a box click must raise the box over the panel")
	}
	click(f.x+f.w-3, py+3) // the panel's part outside the box
	if m.floatFocused() != f || topFloatPanel(m) != f {
		t.Fatal("a panel click must move the keyboard back to the panel")
	}
	if m.popupBoxZ() != 0 || m.popupBoxAt(overlapX, overlapY) != f.inst {
		t.Fatal("a panel click must raise it over the box again")
	}
	// A press outside every surface blurs the whole layer (#2309) — box and
	// panels stay visible as one unit, the keyboard moves to the panes below.
	m = step(m, press(1, m.height-2))
	if !m.popup.open || !m.popup.blurred {
		t.Fatal("a press outside the layer must blur it as one unit")
	}
}

func TestToggleHidesAndShowsWholeLayer(t *testing.T) {
	m := openTestPopup(t)
	m, f := tearOutFirstTab(t, m)
	inst := m.popup.inst
	out, _ := m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if m.popup.open {
		t.Fatal("the toggle must hide the whole layer")
	}
	if v := m.render(); strings.Contains(v, "POPUP TERMINAL") || strings.Contains(v, floatGlobalOff) {
		t.Fatal("a hidden layer must not render box or panels")
	}
	out, _ = m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if !m.popup.open || m.popup.inst != inst || len(m.floatTerms) != 1 || m.floatTerms[0] != f {
		t.Fatal("the next toggle must reveal box and panels unchanged")
	}
	if m.popup.inst.TabCount() != 1 || f.inst.TabCount() != 1 {
		t.Fatal("no shells may spawn on reveal — everything was retained")
	}
}

func TestPopupResizeChordActsOnFocusedPanel(t *testing.T) {
	m := openTestPopup(t)
	m, f := tearOutFirstTab(t, m)
	w0, h0 := f.w, f.h
	bw0, bh0 := m.popupSize()
	handled, out, _ := m.popupReservedKey("shift+super+right")
	if !handled {
		t.Fatal("the resize chord must stay reserved with a panel focused")
	}
	m = out.(Model)
	f = m.floatTerms[0]
	if f.w != w0+4 || f.h != h0 {
		t.Fatalf("the chord must resize the focused panel: want %dx%d, got %dx%d", w0+4, h0, f.w, f.h)
	}
	if bw, bh := m.popupSize(); bw != bw0 || bh != bh0 {
		t.Fatal("the box size must stay untouched while a panel owns the keyboard")
	}
}

func TestExitedSessionClosesPanel(t *testing.T) {
	m := openTestPopup(t)
	m, f := tearOutFirstTab(t, m)
	sess := f.inst.ActiveTerminal().SessionKey()
	f.inst.CloseTerminalTabs() // end the shell like an exit would
	out, _ := m.Update(terminal.ExitedMsg{Key: sess})
	m = out.(Model)
	if len(m.floatTerms) != 0 {
		t.Fatal("the session's exit must close its panel")
	}
	if !m.popup.open || m.popup.inst == nil {
		t.Fatal("the box must survive a panel's exit")
	}
	if m.popupFocused() != m.popup.inst {
		t.Fatal("the keyboard must fall back to the box")
	}
}

func TestGlobalPanelRidesAcrossProjectSwitch(t *testing.T) {
	_, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	m, f := tearOutFirstTab(t, m)
	// The global toggle sits at the title row's first cells (●/○).
	m = step(m, press(f.x+paneContentX, f.y+1))
	f = m.floatTerms[len(m.floatTerms)-1]
	if !f.global {
		t.Fatal("a click on the title-row button must mark the panel global")
	}
	sess := f.inst.ActiveTerminal().SessionKey()

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	// The global panel rode across with its live session; the popup box and
	// any project panels parked with workspace a.
	if len(m.floatTerms) != 1 || m.floatTerms[0] != f {
		t.Fatal("the global panel must ride into the fresh model")
	}
	if got := f.inst.ActiveTerminal().SessionKey(); got != sess || !f.inst.ActiveTerminal().Running() {
		t.Fatal("the global session must keep running unchanged across the switch")
	}
	if !m.popup.open {
		t.Fatal("a layer visible before the switch must stay visible after it")
	}
	if v := m.render(); !strings.Contains(v, floatGlobalOn) {
		t.Fatal("the global panel must render (●) in the new project")
	}
	extras := m.ws.Peek(m.ws.Background()[0]).Aux.(wsExtras)
	if len(extras.floats) != 0 {
		t.Fatal("a global panel must not park in the workspace's Aux")
	}
	if extras.popup.inst == nil {
		t.Fatal("the popup box itself still parks per project (#1407)")
	}

	// Switching back re-unites the restored project popup with the global
	// panel still on top.
	out, _ = m.Update(project.SwitchProjectMsg{Root: m.ws.Background()[0]})
	m = out.(Model)
	if m.popup.inst == nil {
		t.Fatal("switching back must restore the project's popup box")
	}
	if len(m.floatTerms) != 1 || m.floatTerms[0].inst.ActiveTerminal().SessionKey() != sess {
		t.Fatal("the global panel must still be there after switching back")
	}
}

func TestProjectPanelParksAndDiesWithWorkspace(t *testing.T) {
	_, b := twoRoots(t)
	m := openTestPopupWith(t, switchModel(t))
	m, f := tearOutFirstTab(t, m)
	term := f.inst.ActiveTerminal()

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if len(m.floatTerms) != 0 {
		t.Fatal("a project-owned panel must not follow into the new project")
	}
	root := m.ws.Background()[0]
	extras := m.ws.Peek(root).Aux.(wsExtras)
	if len(extras.floats) != 1 || extras.floats[0] != f {
		t.Fatal("the project panel must park in the workspace's Aux")
	}
	if !term.Running() {
		t.Fatal("parked panel sessions must keep running")
	}
	// Tearing the parked workspace down ends the panel's session like the
	// popup's (#1407) — the global lifecycle rule only spares global panels.
	teardownWorkspace(m.ws.Drop(root))
	if term.Running() {
		t.Fatal("the workspace teardown must end the parked panel session")
	}
}

func TestQuitEndsGlobalPanelSession(t *testing.T) {
	m := openTestPopup(t)
	m, f := tearOutFirstTab(t, m)
	m.toggleFloatTermGlobal(f)
	term := f.inst.ActiveTerminal()
	if !term.Running() {
		t.Fatal("test setup: the panel session must be running")
	}
	if _, cmd := m.quit(); cmd == nil {
		t.Fatal("quit must return the exit command")
	}
	if term.Running() {
		t.Fatal("a global session ends with the app — quit must close it")
	}
}
