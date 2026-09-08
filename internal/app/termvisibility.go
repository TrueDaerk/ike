package app

import (
	"ike/internal/pane"
	"ike/internal/terminal"
)

// termvisibility.go is the idle half of terminal output handling (#2540):
// a terminal session nothing renders must not wake the program per output
// burst. Before it, every session of the active workspace reported each
// quiet-interval burst as a terminal.OutputMsg, whether or not its grid was
// on screen — the shell of the closed popup layer, the inactive tabs of a
// terminal-tab host, the panes hidden behind a zoom — and a spinner in a
// hidden tab alone cost ~180 coalescedInputMsg passes a minute with nobody
// typing (the #2540 telemetry). The sync below runs on Update's settled
// pass, where every message that can open, close, reorder or zoom a pane
// has landed, and flips each session's visibility park on the edge:
// hidden sessions fold their bursts into the one repaint owed on reveal
// (terminal.Session.SetHidden), keeping a single wake per hidden stretch
// for the activity indicators that ride on OutputMsg.

// syncTerminalVisibility parks the terminal sessions the frame does not
// render and un-parks the ones it does. It touches a session only on the
// edge — Parked() is one atomic load — so an ordinary pass costs a walk over
// the pane set and nothing else. Sessions of parked workspaces (#1522) and
// detached global tools (#1890) live outside the active workspace and are
// parked by their own lifecycles; they are not visited here.
func (m *Model) syncTerminalVisibility() {
	ws := m.activeWS()
	if ws == nil || ws.Panes == nil {
		return
	}
	// A zoomed layout (pane.maximize, zen) renders the zoomed pane alone;
	// every other pane's terminals are off screen. zoomActive also drops a
	// zoom whose pane or tree is gone, as the layout does.
	zoomed := ""
	if m.zoomActive() {
		zoomed = m.zoomed
	}
	for _, key := range ws.Panes.Keys() {
		inst := ws.Panes.Get(key)
		if inst == nil {
			continue
		}
		paneShown := zoomed == "" || key == zoomed
		switch inst.Kind() {
		case pane.KindTerminal:
			setTermHidden(inst.Terminal(), !paneShown)
		case pane.KindEditor:
			// A terminal-tab host (#573) shows its active tab only.
			active := inst.ActiveTab()
			for i := 0; i < inst.TabCount(); i++ {
				if t := inst.TabTerminal(i); t != nil {
					setTermHidden(t, !paneShown || i != active)
				}
			}
		}
	}
	// The popup layer (#1398): hidden as a whole while closed, and each
	// box side / floating panel (#1427, #1793) shows its active tab only.
	visible := m.popupLayerVisible()
	for _, inst := range m.popupLayerInstances() {
		active := inst.ActiveTab()
		for i := 0; i < inst.TabCount(); i++ {
			if t := inst.TabTerminal(i); t != nil {
				setTermHidden(t, !visible || i != active)
			}
		}
	}
}

// setTermHidden applies the visibility park on the edge only: SetHidden
// re-arms the session's one hidden wake, so calling it on every pass would
// grant a wake per pass instead of one per hidden stretch.
func setTermHidden(t *terminal.Model, hidden bool) {
	if t == nil || t.Parked() == hidden {
		return
	}
	t.SetHidden(hidden)
}

// noteTerminalOutput is the app's screen-changed hook for one session, shared
// by the raw terminal.OutputMsg path and the folded coalescedInputMsg path
// (#803) — before #2540 the fold reached only the completion hook, so the
// popup's activity indicator (#2309) never armed in a running program. The
// completion popup (#740) recomputes here: the shell has echoed the
// keystrokes, so the cursor row reads current. Output landing in a hidden
// popup-layer shell arms the statusbar activity indicator; the next show
// clears it. A visible layer — focused or blurred — has the output on screen
// already.
func (m *Model) noteTerminalOutput(key string) {
	t := m.terminalModelForSession(key)
	if t == nil {
		return
	}
	t.OnOutput()
	if !m.popupLayerVisible() {
		if _, _, pt := m.popupTabForSession(key); pt != nil {
			m.popupUnseen = true
		}
	}
}

// syncPreviewBound publishes whether any markdown preview pane is open
// (#2540), so the editor emitter can skip the preview.CursorMsg it would
// otherwise send on every caret move — a message, and so an Update+View
// pass, that had no consumer in the common no-preview session. Runs on the
// settled pass, where pane opens and closes have landed.
func (m *Model) syncPreviewBound() {
	if m.previewBound == nil {
		return
	}
	bound := false
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if c.Kind() == pane.KindMarkdown {
			bound = true
			return false
		}
		return true
	})
	m.previewBound.Store(bound)
}
