package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/diag"
)

// renderreuse.go makes mouse motion cheap (#2626).
//
// bubbletea calls View after every Update that returns, with no way to say
// "nothing changed". A terminal reporting pointer motion (~10-17 events/s)
// therefore cost a full frame composition per coalesced burst even when the
// pointer moved over nothing that reacts to hover — the idle churn the
// telemetry heartbeats kept showing after #2540 (`view/render` tracking
// `app.coalescedInputMsg` nearly 1:1 in minutes without a key or click).
//
// The lever is on the View side: a pass whose only effect was a motion step
// that changed no hover-dependent state marks the frame reusable, and View
// hands bubbletea the frame it composed for the previous pass. The renderer
// diffs it against what is on screen and writes nothing. Counting: a
// composed frame is a `view/render` pass, a reused one a `view/reuse` pass,
// so the ratio "motion bursts → renders" is observable in the heartbeat's
// `top` field and in unit tests through diag.MessageCounts.
//
// The rule is opt-in: a motion consumer must *prove* the hover target did
// not change (explorer hover row, mouse-idle hover popup); every other path
// — a drag step, an overlay hover, a wheel notch, a terminal repaint folded
// into the same burst — keeps the default and renders. A missed consumer
// costs a render, never a stale frame.

// frameCache is the pointer-shared state behind the reuse decision. Model is
// a value type copied on every Update, so the flags live behind a pointer
// that every copy of one model shares (like the tick generation, #2194).
type frameCache struct {
	// pass counts Update entries; viewPass is the pass the cached view was
	// composed (or last handed out) for. A reuse is only ever the frame of
	// the immediately preceding pass — a View called on a stale model copy
	// (tests sequence those) can never be served for a later pass.
	pass     uint64
	viewPass uint64
	view     tea.View
	valid    bool
	// noop is raised by a motion consumer that proved its hover target
	// unchanged; reuse is the pass-level verdict the message handler derives
	// from it (a motion-only burst whose consumer reported no change). Both
	// are cleared at Update entry.
	noop  bool
	reuse bool
}

// beginPass resets the per-pass flags at Update entry, so a verdict from an
// earlier pass never leaks into the next one.
func (c *frameCache) beginPass() {
	if c == nil {
		return
	}
	c.pass++
	c.noop, c.reuse = false, false
}

// noteMotionNoop is called by the plain-pane motion path when neither the
// explorer hover nor the idle-hover popup changed with this step.
func (m Model) noteMotionNoop() {
	if m.frame != nil {
		m.frame.noop = true
	}
}

// motionNoop reports whether the motion consumer of this pass proved its
// hover target unchanged.
func (m Model) motionNoop() bool { return m.frame != nil && m.frame.noop }

// markFrameReusable records the pass verdict: the frame composed for the
// previous pass is still exact, View may hand it out again.
func (m Model) markFrameReusable() {
	if m.frame != nil {
		m.frame.reuse = true
	}
}

// frameReusable reports whether the pass verdict allows reusing the previous
// frame: the flag is set and the cached view was composed for (or already
// reused in) the immediately preceding pass.
func (m Model) frameReusable() bool {
	c := m.frame
	return c != nil && c.reuse && c.valid && c.viewPass+1 == c.pass
}

// cachedView returns the previous pass's frame for reuse and stamps it as
// this pass's view, so a run of no-op passes keeps chaining. Counted as a
// `view/reuse` pass (never `view/render`).
func (m Model) cachedView() tea.View {
	diag.LoopEnter("view/reuse")
	diag.LoopExit()
	m.frame.viewPass = m.frame.pass
	return m.frame.view
}

// storeView remembers the frame View just composed for this pass.
func (m Model) storeView(v tea.View) {
	if c := m.frame; c != nil {
		c.view, c.valid, c.viewPass = v, true, c.pass
	}
}
