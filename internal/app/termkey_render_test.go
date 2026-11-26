package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/diag"
	"ike/internal/host"
	"ike/internal/terminal"
)

// termkey_render_test.go guards the one-frame-per-echo rule: a key press
// forwarded unchanged to a terminal child reuses the previous frame, and the
// child's answer composes the only frame of the pair. Every key the IDE or
// the pane acts on itself still renders at once.

// keyRenders reads the render-trigger counter for key press passes.
func keyRenders() uint64 {
	return diag.RenderTriggers()["tea.KeyPressMsg"]
}

// focusedPipeTerminal puts a pipe-backed terminal tab into the focused
// editor pane and returns it with a composed frame behind it.
func focusedPipeTerminal(t *testing.T, key string) (Model, *terminal.Model) {
	t.Helper()
	m := dismissOnboarding(sized(t, 100, 40))
	m.activeWS().Panes.SetFocused(m.activeEditorKey())
	inst := m.activeWS().Panes.Get(m.activeEditorKey())
	if inst == nil {
		t.Fatal("test setup: no editor pane")
	}
	term := pipeTab(t, m, inst, key)
	m = viewedPass(t, m)
	if !m.terminalFocused() {
		t.Fatal("test setup: the terminal tab holds the focus")
	}
	return m, term
}

// typeWithEcho forwards each rune as a key press and lets the child echo it,
// failing when a key pass composes a frame or an echo pass does not.
func typeWithEcho(t *testing.T, m Model, term *terminal.Model, text string) Model {
	t.Helper()
	for _, r := range text {
		var composed bool
		m, composed = pass(t, m, keyMsg(r))
		if composed {
			t.Fatalf("forwarding %q composed a frame of its own", r)
		}
		term.FeedText(string(r))
		m, composed = pass(t, m, coalescedInputMsg{termKeys: []string{term.SessionKey()}})
		if !composed {
			t.Fatalf("the echo of %q must compose a frame", r)
		}
	}
	return m
}

func TestForwardedTerminalKeyRendersOncePerEcho(t *testing.T) {
	// serial: keyRenders: diag.RenderTriggers (process-wide counters)
	m, term := focusedPipeTerminal(t, "key-pane")
	before, echoes := keyRenders(), coalescedRenders()
	const typed = "ls -la"
	typeWithEcho(t, m, term, typed)
	if got := keyRenders(); got != before {
		t.Fatalf("renders attributed to tea.KeyPressMsg grew %d → %d over forwarded keys", before, got)
	}
	if got := coalescedRenders() - echoes; got != uint64(len(typed)) {
		t.Fatalf("want one echo frame per key (%d), got %d", len(typed), got)
	}
}

func TestForwardedPopupKeyRendersOncePerEcho(t *testing.T) {
	// serial: keyRenders: diag.RenderTriggers (process-wide counters)
	m := dismissOnboarding(openTestPopup(t))
	term := pipeTab(t, m, m.popup.inst, "key-popup")
	m = viewedPass(t, m)
	if !m.popupLayerFocused() || m.popupFocused().ActiveTerminal() != term {
		t.Fatal("test setup: the popup's pipe tab holds the keyboard")
	}
	before := keyRenders()
	typeWithEcho(t, m, term, "make")
	if got := keyRenders(); got != before {
		t.Fatalf("renders attributed to tea.KeyPressMsg grew %d → %d over keys forwarded to the popup", before, got)
	}
}

func TestForwardedFloatingPanelKeyRendersOncePerEcho(t *testing.T) {
	// serial: withStore: t.Setenv
	withStore(t)
	m := dismissOnboarding(openTestPopup(t))
	m, f := tearOutFirstTab(t, m)
	term := pipeTab(t, m, f.inst, "key-float")
	m = viewedPass(t, m)
	if m.floatFocused() != f || m.popupFocused().ActiveTerminal() != term {
		t.Fatal("test setup: the floating panel's pipe tab holds the keyboard")
	}
	before := keyRenders()
	typeWithEcho(t, m, term, "top")
	if got := keyRenders(); got != before {
		t.Fatalf("renders attributed to tea.KeyPressMsg grew %d → %d over keys forwarded to the panel", before, got)
	}
}

func TestTerminalKeyTheIDEHandlesRendersAtOnce(t *testing.T) {
	// serial: pass: diag.MessageCounts (process-wide counters)
	m, term := focusedPipeTerminal(t, "key-ide")
	for i := 0; i < 30; i++ {
		term.FeedText("history line\n")
	}
	m, _ = pass(t, m, coalescedInputMsg{termKeys: []string{term.SessionKey()}})

	// Scrollback paging moves the view.
	m, composed := pass(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModShift})
	if !composed {
		t.Fatal("paging the scrollback must render")
	}
	// The next typed key snaps the view back to live: the frame changes
	// before the child answers.
	if m, composed = pass(t, m, keyMsg('x')); !composed {
		t.Fatal("a key that snaps the scrolled view back to live must render")
	}
	if m, composed = pass(t, m, keyMsg('y')); composed {
		t.Fatal("test setup: a plain key at the live view is forwarded without a frame")
	}

	// Typing drops a selection.
	term.MousePress(2, 1)
	term.MouseDrag(8, 1)
	term.MouseRelease(8, 1)
	if !term.HasSelection() {
		t.Fatal("test setup: the drag selects")
	}
	m = viewedPass(t, m)
	if m, composed = pass(t, m, keyMsg('z')); !composed || term.HasSelection() {
		t.Fatalf("a key that clears the selection must render (composed=%v)", composed)
	}

	// Copy mode owns the keyboard: its cursor moves render.
	if !term.StartCopyMode() {
		t.Fatal("test setup: copy mode opens")
	}
	m.View()
	if m, composed = pass(t, m, keyMsg('k')); !composed {
		t.Fatal("a copy-mode key must render")
	}
	if m, composed = pass(t, m, tea.KeyPressMsg{Code: tea.KeyEscape}); !composed {
		t.Fatal("leaving copy mode must render")
	}

	// A spatial focus move leaves the terminal.
	focused := m.activeWS().Panes.Focused()
	m, composed = pass(t, m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	if !composed || m.activeWS().Panes.Focused() == focused {
		t.Fatalf("a focus chord must move the focus and render (composed=%v)", composed)
	}
}

func TestTerminalGlobalChordRendersAtOnce(t *testing.T) {
	// serial: pass: diag.MessageCounts (process-wide counters)
	m, _ := focusedPipeTerminal(t, "key-global")
	m.paletteKey = "ctrl+p"
	m, composed := pass(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if !composed || !m.palette.IsOpen() {
		t.Fatalf("the palette chord must open the palette and render (composed=%v open=%v)", composed, m.palette.IsOpen())
	}
}

func TestForwardedKeyWithPendingFrameChangeRenders(t *testing.T) {
	// serial: pass: diag.MessageCounts (process-wide counters)
	m, _ := focusedPipeTerminal(t, "key-toast")
	// Esc dismisses an error toast on its way to the child.
	m.host.Notify(host.Error, "boom")
	m = viewedPass(t, m)
	if len(m.toasts) == 0 {
		t.Fatal("test setup: an error toast is showing")
	}
	m, composed := pass(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !composed || len(m.toasts) != 0 {
		t.Fatalf("esc dismissing a toast must render (composed=%v toasts=%d)", composed, len(m.toasts))
	}
	// A notification raised during a forwarded key's pass withdraws the reuse.
	m.host.Notify(host.Info, "note")
	if _, composed = pass(t, m, keyMsg('a')); !composed {
		t.Fatal("a toast queued during the key pass must render")
	}
}

func TestSilentChildLeavesFrameExact(t *testing.T) {
	// serial: pass: diag.MessageCounts (process-wide counters)
	m, term := focusedPipeTerminal(t, "key-silent")
	term.FeedText("prompt$ ")
	m, _ = pass(t, m, coalescedInputMsg{termKeys: []string{term.SessionKey()}})
	want := m.View().Content
	m, composed := pass(t, m, keyMsg('a'))
	if composed {
		t.Fatal("a forwarded key with a silent child composes no frame")
	}
	if got := m.View().Content; got != want {
		t.Fatal("the reused frame must equal a freshly composed one")
	}
}
