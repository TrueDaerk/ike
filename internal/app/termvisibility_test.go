package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/forge"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/preview"
	"ike/internal/terminal"
)

// termvisibility_test.go guards the idle model of #2540: terminal sessions
// nothing renders park on the settled pass, the popup's activity indicator
// still arms through the coalesced output path, caret moves send no
// preview.CursorMsg while no preview pane is open, and user input restarts
// the forge poll's idle clock.

// settlePass runs one message through Update so the settled pass applies.
func settlePass(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return out.(Model)
}

// pipeTab adds a pipe-backed terminal tab to inst and closes it with the
// test.
func pipeTab(t *testing.T, m Model, inst *pane.Instance, key string) *terminal.Model {
	t.Helper()
	term := inst.AddTerminalTab(terminal.NewPipe(key, 40, 10, m.host.Send))
	t.Cleanup(term.Close)
	return term
}

func TestInactiveTerminalTabsParkOnTheSettledPass(t *testing.T) {
	m := sized(t, 100, 40)
	inst := m.activeWS().Panes.Get(m.activeEditorKey())
	if inst == nil {
		t.Fatal("test setup: no editor pane")
	}
	first := pipeTab(t, m, inst, "vis-first")
	second := pipeTab(t, m, inst, "vis-second")
	if inst.ActiveTerminal() != second {
		t.Fatal("test setup: the last added tab is active")
	}
	m = settlePass(t, m)
	if !first.Parked() {
		t.Fatal("the inactive terminal tab must park: nothing renders it")
	}
	if second.Parked() {
		t.Fatal("the active terminal tab stays live")
	}
	// Switching tabs flips both on the next settled pass.
	inst.ActivateTab(inst.ActiveTab() - 1)
	m = settlePass(t, m)
	if first.Parked() {
		t.Fatal("the tab switched to must un-park")
	}
	if !second.Parked() {
		t.Fatal("the tab switched away from must park")
	}
	// Zooming another pane hides the whole host.
	m.zoomed = pane.ExplorerKey
	m.zoomSig = leavesSignature(m.activeWS().Tree)
	m = settlePass(t, m)
	if !first.Parked() {
		t.Fatal("a terminal behind a zoomed pane is off screen and parks")
	}
	m.zoomed = ""
	m = settlePass(t, m)
	if first.Parked() {
		t.Fatal("leaving the zoom un-parks the active tab again")
	}
}

func TestClosedPopupLayerParksItsShells(t *testing.T) {
	m := openTestPopup(t)
	// A pipe tab stands in for the shell: the full suite on some machines
	// cannot spawn one more zsh, and the visibility park is about tabs, not
	// what runs in them.
	shell := pipeTab(t, m, m.popup.inst, "popup-shell")
	m = settlePass(t, m)
	if shell.Parked() {
		t.Fatal("the open popup's shell is rendered and stays live")
	}
	out, _ := m.Update(TerminalPopupMsg{}) // hide
	m = out.(Model)
	if m.popup.open {
		t.Fatal("test setup: the toggle hides the layer")
	}
	if !shell.Parked() {
		t.Fatal("the hidden popup's shell must park on the pass that hid it")
	}
	out, _ = m.Update(TerminalPopupMsg{}) // show
	m = out.(Model)
	if shell.Parked() {
		t.Fatal("showing the layer un-parks the shell")
	}
	// A second popup tab: only the active one is live.
	other := pipeTab(t, m, m.popup.inst, "popup-other")
	m = settlePass(t, m)
	if other.Parked() || !shell.Parked() {
		t.Fatal("of the popup's tabs only the active one stays live")
	}
}

func TestHiddenPopupWakesOnceAndArmsTheIndicator(t *testing.T) {
	m := openTestPopup(t)
	msgs := make(chan tea.Msg, 64)
	m.host.SetSender(func(msg tea.Msg) { msgs <- msg })
	inst := m.popup.inst
	shell := pipeTab(t, m, inst, "popup-pipe")
	m = settlePass(t, m)
	out, _ := m.Update(TerminalPopupMsg{}) // hide
	m = out.(Model)
	if !shell.Parked() {
		t.Fatal("test setup: the hidden popup's shell parks")
	}
	// The layer's real zsh tab is hidden too and may report its own late
	// output once; only the pipe's messages count here.
	key := shell.SessionKey()
	pipeOutput := func(wait time.Duration) bool {
		deadline := time.After(wait)
		for {
			select {
			case msg := <-msgs:
				if om, ok := msg.(terminal.OutputMsg); ok && om.Key == key {
					return true
				}
			case <-deadline:
				return false
			}
		}
	}
	// The first output of the hidden stretch wakes the program once …
	shell.FeedText("spinner\n")
	if !pipeOutput(2 * time.Second) {
		t.Fatal("the first hidden output must wake once")
	}
	// … through the coalesced path, and that wake arms the indicator —
	// before #2540 the fold reached only the completion hook.
	out, _ = m.Update(coalescedInputMsg{termKeys: []string{key}})
	m = out.(Model)
	if !m.popupUnseen {
		t.Fatal("coalesced output into the hidden popup must arm the activity indicator")
	}
	// Every later burst folds: no further message while hidden.
	for i := 0; i < 4; i++ {
		shell.FeedText("more\n")
		time.Sleep(20 * time.Millisecond)
	}
	if pipeOutput(80 * time.Millisecond) {
		t.Fatal("hidden popup sent another OutputMsg after its one wake")
	}
}

func TestCaretMovesSendNoPreviewCursorWithoutAPreviewPane(t *testing.T) {
	msgs := make(chan tea.Msg, 64)
	h := host.New(host.MapConfig{})
	h.SetSender(func(msg tea.Msg) { msgs <- msg })
	m := sized(t, 100, 40)
	m = settlePass(t, m)
	if m.previewBound.Load() {
		t.Fatal("test setup: no preview pane is open")
	}
	e := editorEmitter{host: h, previews: m.previewBound}
	e.Emit(editor.Event{Kind: editor.EventCursorMove, Path: "/tmp/x.md", Line: 3})
	select {
	case msg := <-msgs:
		if _, ok := msg.(preview.CursorMsg); ok {
			t.Fatal("a caret move must not send preview.CursorMsg while no preview is open")
		}
	case <-time.After(50 * time.Millisecond):
	}
	// With a preview open the message goes out as before.
	m.previewBound.Store(true)
	e.Emit(editor.Event{Kind: editor.EventCursorMove, Path: "/tmp/x.md", Line: 4})
	select {
	case msg := <-msgs:
		if _, ok := msg.(preview.CursorMsg); !ok {
			t.Fatalf("got %T, want preview.CursorMsg", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("with a preview open the caret move must send preview.CursorMsg")
	}
	// A bare emitter (no flag) keeps the old unconditional behaviour.
	bare := editorEmitter{host: h}
	bare.Emit(editor.Event{Kind: editor.EventCursorMove, Path: "/tmp/x.md", Line: 5})
	select {
	case <-msgs:
	case <-time.After(2 * time.Second):
		t.Fatal("a bare emitter sends unconditionally")
	}
}

func TestUserInputRestartsTheForgeIdleClock(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	p := m.forgePoller()
	if p == nil {
		t.Fatal("test setup: the app builds a poller")
	}
	now := time.Unix(1_700_000_000, 0)
	p.SetClock(func() time.Time { return now })
	p.Input()
	now = now.Add(3 * forge.IdleBackoffAfter)
	if !p.IdleStretched() {
		t.Fatal("test setup: six idle minutes stretch the cadence")
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = out.(Model)
	if p.IdleStretched() {
		t.Fatal("a key press must restart the idle clock")
	}
	now = now.Add(3 * forge.IdleBackoffAfter)
	out, _ = m.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	m = out.(Model)
	if p.IdleStretched() {
		t.Fatal("a click must restart the idle clock")
	}
	now = now.Add(3 * forge.IdleBackoffAfter)
	w := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}
	out, _ = m.Update(coalescedInputMsg{wheels: []tea.MouseWheelMsg{w}})
	_ = out
	if p.IdleStretched() {
		t.Fatal("a wheel notch must restart the idle clock")
	}
	now = now.Add(3 * forge.IdleBackoffAfter)
	out, _ = m.Update(coalescedInputMsg{termKeys: []string{"nobody"}})
	_ = out
	if !p.IdleStretched() {
		t.Fatal("terminal output alone is not user input and must not reset the clock")
	}
}
