package app

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/safego"
	"ike/internal/telemetry"
)

// complexity_duplicates.go wires the Duplicates tab of the Complexity
// window (Roadmap 0560, #3044): the project's clone groups over the token
// streams the index keeps per file. The index does the per-file half — a
// saved or changed file is re-tokenized alone, with its measurement — and
// the detector (complexity.View.Duplicates) is one pass over the streams
// of a snapshot, run off the Update goroutine. Like the Trend tab it is
// pulled, not pushed: every ComplexityChangedMsg marks the result stale,
// and once per settled Update pass, while the Complexity window is open,
// the model launches one detection when the result is stale or the clone
// size changed. A closed window costs nothing; opening it runs the
// detector once over the snapshot already held.

// complexityDuplicatesState is the session state behind the Duplicates
// tab, shared by pointer across value-model copies like cxDiff.
type complexityDuplicatesState struct {
	// seen counts the index generations that arrived; started is the count
	// the running (or last) detection saw, so a generation landing during
	// a detection leaves the result stale.
	seen, started int
	// minTokens is the clone size the last detection ran with.
	minTokens int
	computing bool
	stale     bool
	result    complexitypanel.Duplicates
}

// newComplexityDuplicatesState starts stale: the first open window runs
// the detector.
func newComplexityDuplicatesState() *complexityDuplicatesState {
	return &complexityDuplicatesState{stale: true}
}

// complexityDuplicatesMsg carries one finished detection.
type complexityDuplicatesMsg struct {
	started   int
	minTokens int
	groups    []complexity.CloneGroup
	ms        int64
}

// duplicatesMinTokens is the configured clone size, the default for a
// config that has none (a bare test model).
func duplicatesMinTokens() int {
	if c := config.Get(); c != nil && c.Complexity.DuplicatesMinTokens > 0 {
		return c.Complexity.DuplicatesMinTokens
	}
	return complexity.DefaultDuplicatesMinTokens
}

// noteComplexityDuplicatesGen records an index generation: the result is
// stale until a detection over a later snapshot lands.
func (m *Model) noteComplexityDuplicatesGen() {
	if m.cxDup == nil {
		return
	}
	m.cxDup.seen++
	m.cxDup.stale = true
}

// complexityDuplicatesSyncCmd runs once per settled Update pass: while the
// Complexity window is open and the result is stale, or was found with
// another clone size, it launches one detection off the loop.
func (m *Model) complexityDuplicatesSyncCmd() tea.Cmd {
	st := m.cxDup
	if st == nil || m.complexityIndex == nil || st.computing {
		return nil
	}
	p := m.complexityPanel()
	if p == nil {
		return nil
	}
	min := duplicatesMinTokens()
	if !st.stale && st.minTokens == min {
		return nil
	}
	return m.startComplexityDuplicates(p, min)
}

// startComplexityDuplicates launches the detector over the current
// snapshot: the pane shows the detecting state, the detection runs in its
// own goroutine and one command delivers its result.
func (m *Model) startComplexityDuplicates(p *complexitypanel.Model, min int) tea.Cmd {
	st := m.cxDup
	st.computing, st.started, st.minTokens = true, st.seen, min
	st.result.Computing = true
	st.result.MinTokens = min
	p.SetDuplicates(st.result)
	view := m.complexityIndex.Snapshot()
	started := st.started
	done := make(chan complexityDuplicatesMsg, 1)
	safego.Go("app.complexityDuplicates.detect", func() {
		start := time.Now()
		groups := view.Duplicates(min)
		done <- complexityDuplicatesMsg{started: started, minTokens: min, groups: groups, ms: time.Since(start).Milliseconds()}
	})
	return func() tea.Msg { return <-done }
}

// applyComplexityDuplicates stores a finished detection: the pane shows
// it, and the result is fresh unless a generation landed meanwhile — the
// settled pass that follows then launches the next detection.
func (m *Model) applyComplexityDuplicates(msg complexityDuplicatesMsg) {
	st := m.cxDup
	if st == nil {
		return
	}
	st.computing = false
	st.stale = msg.started != st.seen
	st.result = complexitypanel.Duplicates{Groups: msg.groups, MinTokens: msg.minTokens, Ready: true}
	if m.usage != nil {
		m.usage.Op(telemetry.OpComplexityDuplicates, telemetry.OpPhaseOK, map[string]string{
			"ms":     strconv.FormatInt(msg.ms, 10),
			"groups": strconv.Itoa(len(msg.groups)),
			"min":    strconv.Itoa(msg.minTokens),
		})
	}
	if p := m.complexityPanel(); p != nil {
		p.SetDuplicates(st.result)
	}
}

// seedComplexityDuplicates hands a (re)opened pane the result the session
// holds, so a layout restore does not start from an empty tab; a stale
// result is refreshed by the next settled pass.
func (m *Model) seedComplexityDuplicates(p *complexitypanel.Model) {
	if p != nil && m.cxDup != nil {
		p.SetDuplicates(m.cxDup.result)
	}
}

// ComplexityDuplicates exposes the Duplicates tab's data (tests).
func (m Model) ComplexityDuplicates() complexitypanel.Duplicates {
	if m.cxDup == nil {
		return complexitypanel.Duplicates{}
	}
	return m.cxDup.result
}
