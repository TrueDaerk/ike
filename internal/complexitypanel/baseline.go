package complexitypanel

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// baseline.go is the accepted-debt half of the panel (Roadmap 0551,
// #2958): the root model hands the pane the project's
// .ike/complexity-baseline.json (complexity.Baseline) via SetBaseline, nil
// while the setting is off or there is no file. Every function row is
// classified against it when the population is built (Row.Fn.Baseline), so
// the totals, the group rows (complexity.Summarize) and the filter see only
// new and worsened functions as over threshold; an accepted row paints its
// warning cells muted and carries ✓ after the name; `baseline:` filters by
// status. 'i' (complexity.baseline) asks the root model to accept the
// selected function — or every over-threshold function of a group row —
// into the file, or to drop an accepted one again (BaselineToggleMsg);
// the pane never touches the file itself.

// acceptedMark follows the name of a function the baseline accepts.
const acceptedMark = " ✓"

// BaselineItem names one function for BaselineToggleMsg: the
// project-relative slash path (the baseline's key), the unit and its
// ordinal among the file's same-named units.
type BaselineItem struct {
	Rel     string
	Fn      complexity.Function
	Ordinal int
}

// BaselineToggleMsg asks the root model to record Items in the baseline
// at their current metrics (Remove false) or to drop their entries (Remove
// true), then save the file ('i').
type BaselineToggleMsg struct {
	Items  []BaselineItem
	Remove bool
}

// SetBaseline hands the pane the baseline in effect; nil classifies every
// function as new. The rows are rebuilt either way: the same pointer may
// hold new entries.
func (m *Model) SetBaseline(b *complexity.Baseline) {
	m.baseline = b
	m.Refresh()
}

// Baseline reports the baseline the pane classifies against (tests).
func (m *Model) Baseline() *complexity.Baseline { return m.baseline }

// item is the row's BaselineItem.
func (r Row) item() BaselineItem {
	return BaselineItem{Rel: r.Rel, Fn: r.Fn, Ordinal: r.Ordinal}
}

// accepted reports whether a function row is accepted by the baseline.
func (r Row) accepted() bool {
	return r.Group == nil && r.Site == nil && r.Fn.Baseline == complexity.BaselineAccepted
}

// ToggleBaseline builds the BaselineToggleMsg for the selected row and
// reports whether there was one: a function row (or a site row, standing
// for its function) toggles its own entry — an accepted function is
// dropped, a new or worsened one recorded at its current metrics; a group
// row records every over-threshold member not yet accepted, or, when all
// of them already are, drops them all. A Changes-tab row, a function under
// the thresholds that is not accepted, and a group without an
// over-threshold member yield nothing.
func (m *Model) ToggleBaseline() (tea.Cmd, bool) {
	r, ok := m.Selected()
	if !ok || m.tab == TabChanges {
		return nil, false
	}
	if r.Site != nil && r.Parent >= 0 && r.Parent < len(m.rows) {
		r = m.rows[r.Parent]
	}
	var msg BaselineToggleMsg
	switch {
	case r.Group != nil:
		var pending, accepted []BaselineItem
		for _, mem := range r.Members {
			switch {
			case mem.accepted():
				accepted = append(accepted, mem.item())
			case mem.Fn.Over(m.ccnWarn, m.cogWarn):
				pending = append(pending, mem.item())
			}
		}
		switch {
		case len(pending) > 0:
			msg.Items = pending
		case len(accepted) > 0:
			msg.Items, msg.Remove = accepted, true
		default:
			return nil, false
		}
	case r.accepted():
		msg.Items, msg.Remove = []BaselineItem{r.item()}, true
	case r.Fn.Over(m.ccnWarn, m.cogWarn):
		msg.Items = []BaselineItem{r.item()}
	default:
		return nil, false
	}
	return func() tea.Msg { return msg }, true
}

// toggleBaseline is the 'i' key: the message, or nothing for a row that
// has no toggle.
func (m *Model) toggleBaseline() tea.Cmd {
	cmd, _ := m.ToggleBaseline()
	return cmd
}

// classify stamps fns (a copy of the snapshot's slice is never made: the
// rows copy each unit) with the baseline status under rel; the ordinals
// come back for the rows.
func (m *Model) classify(rel string, fns []complexity.Function) ([]complexity.Function, []int) {
	ords := complexity.Ordinals(fns)
	if m.baseline == nil {
		return fns, ords
	}
	out := make([]complexity.Function, len(fns))
	for i, f := range fns {
		f.Baseline = m.baseline.Classify(rel, f, ords[i])
		out[i] = f
	}
	return out, ords
}

// baselineValue reads a baseline: filter value: a function row by its
// status; a group row keeps `accepted` when it has an accepted member,
// `worsened` when one worsened, `new` when an over-threshold member is
// neither.
func baselineMatches(want string, r Row) bool {
	st, ok := complexity.ParseStatus(strings.ToLower(want))
	if !ok {
		return false
	}
	if g := r.Group; g != nil {
		switch st {
		case complexity.BaselineAccepted:
			return g.Accepted > 0
		case complexity.BaselineWorsened:
			return g.Worsened > 0
		}
		return g.OverThreshold-g.Worsened > 0
	}
	return r.Fn.Baseline == st
}

// baselineTotals is the header's "N accepted" part, "" without a baseline.
func (m *Model) baselineTotals() string {
	if m.baseline == nil {
		return ""
	}
	return strconv.Itoa(m.summary.Accepted) + " accepted"
}
