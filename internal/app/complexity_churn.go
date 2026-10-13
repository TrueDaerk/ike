package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/vcs"
)

// complexity_churn.go wires the hotspot columns of the Complexity window
// (Roadmap 0551, #2957): the commits per file in the last
// complexity.churn_days days, one `git log` for the project
// (vcs.ChurnCmd), multiplied with complexity by the pane. Like the Changes
// tab's diff it is pulled, not pushed: once per settled Update pass, while
// the window is open, the model compares the key of the counts the pane
// shows — (VCS snapshot generation, window, project root) — with the
// current one and launches the fetch when they differ. A snapshot refresh
// or a changed window therefore re-fetches once; a closed window costs
// nothing; the counts stay cached with their key, so reopening the window
// on the same generation runs no git. Outside a repository the pane gets
// nil and hides the columns.

// cxChurnKey identifies the inputs a churn map was fetched for.
type cxChurnKey struct {
	snapGen int
	days    int
	root    string
}

// complexityChurnState is the cached churn map and its fetch bookkeeping,
// shared by pointer across value-model copies like cxDiff.
type complexityChurnState struct {
	key      cxChurnKey     // the inputs of counts
	pending  cxChurnKey     // the inputs of the run in flight
	fetching bool           // one run at a time
	counts   map[string]int // nil until fetched, outside git and on a git error
	// days is the window counts was taken over (the footer's "churn 90d").
	days int
}

// churnDays is the configured window, the default for a config that has
// none (a bare test model).
func churnDays() int {
	if c := config.Get(); c != nil && c.Complexity.ChurnDays > 0 {
		return c.Complexity.ChurnDays
	}
	return 90
}

// currentCxChurnKey is the key a fetch launched now would carry.
func (m Model) currentCxChurnKey() cxChurnKey {
	k := cxChurnKey{days: churnDays()}
	if m.cxDiff != nil {
		k.snapGen = m.cxDiff.snapGen
	}
	if m.complexityIndex != nil {
		k.root = m.complexityIndex.Root()
	}
	return k
}

// complexityChurnSyncCmd runs once per settled Update pass: while the
// Complexity window is open inside a repository and the counts are behind
// the current key, it launches one fetch off the loop. Outside a
// repository the pane hides the columns and nothing runs.
func (m *Model) complexityChurnSyncCmd() tea.Cmd {
	st := m.cxChurn
	p := m.complexityPanel()
	if st == nil || p == nil || m.complexityIndex == nil {
		return nil
	}
	if m.vcs == nil || m.vcs.snap == nil {
		if st.counts != nil {
			st.counts, st.key = nil, cxChurnKey{}
		}
		if counts, _ := p.Churn(); counts != nil {
			p.SetChurn(nil, 0)
		}
		return nil
	}
	key := m.currentCxChurnKey()
	if st.fetching || key == st.key || key.root == "" {
		return nil
	}
	st.fetching, st.pending = true, key
	return vcs.ChurnCmd(key.root, time.Now().AddDate(0, 0, -key.days))
}

// applyComplexityChurn stores a finished fetch under the key it was
// launched for and hands it to an open pane; a git error (the project
// outside the repository's history, a timeout) hides the columns until the
// next key change rather than retrying every pass.
func (m *Model) applyComplexityChurn(msg vcs.ChurnMsg) {
	st := m.cxChurn
	if st == nil {
		return
	}
	st.fetching = false
	st.key, st.days = st.pending, st.pending.days
	st.counts = msg.Counts
	if msg.Err != nil {
		st.counts = nil
	} else if st.counts == nil {
		st.counts = map[string]int{} // inside git with no history: columns of zeros
	}
	m.seedComplexityChurn(m.complexityPanel())
}

// seedComplexityChurn hands an open pane the cached counts — on every
// fetch and when the window (re)opens on a generation already fetched.
func (m *Model) seedComplexityChurn(p *complexitypanel.Model) {
	if p == nil || m.cxChurn == nil {
		return
	}
	counts, days := m.cxChurn.counts, m.cxChurn.days
	if m.vcs == nil || m.vcs.snap == nil {
		counts = nil
	}
	if cur, curDays := p.Churn(); mapSame(cur, counts) && curDays == days {
		return
	}
	p.SetChurn(counts, days)
}

// mapSame reports whether a and b hold the same counts, telling nil (no
// columns) from empty (columns of zeros), so a reopened pane on an
// unchanged generation skips the re-render.
func mapSame(a, b map[string]int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
