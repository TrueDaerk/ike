package app

import (
	"strings"
	"testing"
	"time"

	"ike/internal/diag"
	"ike/internal/terminal"
)

// termoutput_render_test.go guards the no-render rule for hidden terminals
// (#3078): output landing in a session nothing draws — an inactive terminal
// tab, the shells of the closed popup layer — reuses the previous frame, on
// the coalesced path and the raw one alike, and never counts as a render
// attributed to app.coalescedInputMsg. The owed repaint on reveal composes
// a fresh frame that shows the output.

// coalescedRenders reads the render-trigger counter for the coalesced
// input pass, the figure the telemetry heartbeats report.
func coalescedRenders() uint64 {
	return diag.RenderTriggers()["app.coalescedInputMsg"]
}

// viewedPass runs one settled pass and composes its frame, so the next
// pass's reuse verdict has an exact frame to hand out.
func viewedPass(t *testing.T, m Model) Model {
	t.Helper()
	m = settlePass(t, m)
	m.View()
	return m
}

func TestHiddenTerminalTabOutputNeverRenders(t *testing.T) {
	m := dismissOnboarding(sized(t, 100, 40))
	inst := m.activeWS().Panes.Get(m.activeEditorKey())
	if inst == nil {
		t.Fatal("test setup: no editor pane")
	}
	hidden := pipeTab(t, m, inst, "hid-first")
	pipeTab(t, m, inst, "hid-second") // the active tab; hides the first
	m = viewedPass(t, m)
	if !hidden.Parked() {
		t.Fatal("test setup: the inactive tab parks")
	}
	key := hidden.SessionKey()
	before := coalescedRenders()
	hidden.FeedText("hidden spinner line\n")
	var composed bool
	for i := 0; i < 3; i++ {
		m, composed = pass(t, m, coalescedInputMsg{termKeys: []string{key}})
		if composed {
			t.Fatalf("coalesced output %d into a hidden tab composed a frame", i)
		}
	}
	m, composed = pass(t, m, terminal.OutputMsg{Key: key})
	if composed {
		t.Fatal("a raw OutputMsg for a hidden tab composed a frame")
	}
	if got := coalescedRenders(); got != before {
		t.Fatalf("renders attributed to app.coalescedInputMsg grew %d → %d while the tab was hidden", before, got)
	}
	// Revealing the tab un-parks the session; its owed repaint is a live
	// OutputMsg, and that one renders a frame showing the output.
	if !inst.ActivateTab(inst.ActiveTab() - 1) {
		t.Fatal("test setup: activating the hidden tab")
	}
	m = settlePass(t, m)
	if hidden.Parked() {
		t.Fatal("the revealed tab must un-park")
	}
	m, composed = pass(t, m, coalescedInputMsg{termKeys: []string{key}})
	if !composed {
		t.Fatal("output into the revealed tab must render")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(plainView(m), "hidden spinner line") {
		if time.Now().After(deadline) {
			t.Fatal("the revealed tab does not show the output fed while hidden")
		}
		time.Sleep(10 * time.Millisecond)
		m, _ = pass(t, m, terminal.OutputMsg{Key: key})
	}
}

func TestHiddenPopupOutputRendersTheIndicatorOnce(t *testing.T) {
	m := dismissOnboarding(openTestPopup(t))
	shell := pipeTab(t, m, m.popup.inst, "hid-popup")
	m = settlePass(t, m)
	out, _ := m.Update(TerminalPopupMsg{}) // hide the layer
	m = out.(Model)
	m.View()
	if !shell.Parked() || m.popupLayerVisible() {
		t.Fatal("test setup: the hidden popup's shell parks")
	}
	key := shell.SessionKey()
	// The first output of the hidden stretch arms the statusbar indicator:
	// the frame changes, so this one composes.
	m, composed := pass(t, m, coalescedInputMsg{termKeys: []string{key}})
	if !composed || !m.popupUnseen {
		t.Fatalf("the arming wake must render (composed=%v unseen=%v)", composed, m.popupUnseen)
	}
	before := coalescedRenders()
	for i := 0; i < 3; i++ {
		m, composed = pass(t, m, coalescedInputMsg{termKeys: []string{key}})
		if composed {
			t.Fatalf("output %d into the hidden popup with the indicator armed composed a frame", i)
		}
	}
	m, composed = pass(t, m, terminal.OutputMsg{Key: key})
	if composed {
		t.Fatal("a raw OutputMsg for the hidden popup composed a frame")
	}
	if got := coalescedRenders(); got != before {
		t.Fatalf("renders attributed to app.coalescedInputMsg grew %d → %d while the popup was hidden", before, got)
	}
	// Showing the layer un-parks the shell; its output renders again.
	out, _ = m.Update(TerminalPopupMsg{})
	m = out.(Model)
	m = viewedPass(t, m)
	if shell.Parked() {
		t.Fatal("the shown popup's shell must un-park")
	}
	if _, composed = pass(t, m, coalescedInputMsg{termKeys: []string{key}}); !composed {
		t.Fatal("output into the visible popup must render")
	}
}

func TestVisibleTerminalOutputStillRenders(t *testing.T) {
	m := dismissOnboarding(sized(t, 100, 40))
	inst := m.activeWS().Panes.Get(m.activeEditorKey())
	if inst == nil {
		t.Fatal("test setup: no editor pane")
	}
	live := pipeTab(t, m, inst, "vis-live")
	m = viewedPass(t, m)
	if live.Parked() {
		t.Fatal("test setup: the active tab is live")
	}
	if _, composed := pass(t, m, coalescedInputMsg{termKeys: []string{live.SessionKey()}}); !composed {
		t.Fatal("output into a visible terminal must render")
	}
}
