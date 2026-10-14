package complexitypanel

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// pins.go is the optimisation-session half of the panel (Roadmap 0551,
// #2959). The root model hands the pane the project's pin list
// (complexity.Pins, the state store's complexity-pins.json) via SetPins;
// 'p' asks it to pin or unpin the selected function (PinToggleMsg) — the
// pane never touches the file. A pinned function carries ★ after its name
// on every tab; on the ungrouped Project tab it moves into the Pinned
// section that leads the list whatever the sort, with a `since pin`
// column ("ccn 42→30 (−12) · cog 31→20 (−11)") in the success colour when
// a metric fell and none rose, the warning colour when one rose. A pin
// whose function the snapshot no longer holds stays listed as (gone)
// until it is unpinned. `pinned:` filters.
//
// The session Δ is the header's "session Δexcess −120": the Project tab's
// excess against the first settled snapshot of the session, which the
// root model keeps per project root and hands over via SetSessionBase.

// pinMark follows the name of a pinned function.
const pinMark = " ★"

// PinToggleMsg asks the root model to pin Fn under (Rel, Ordinal) at its
// current metrics, or to unpin it when it is pinned, then save the file
// ('p').
type PinToggleMsg struct {
	Rel     string
	Fn      complexity.Function
	Ordinal int
}

// SetPins hands the pane the pin list in effect; nil is no pins. The rows
// are rebuilt either way: the same pointer may hold new entries.
func (m *Model) SetPins(p *complexity.Pins) {
	m.pins = p
	m.Refresh()
}

// SetSessionBase hands the pane the session's first settled snapshot, the
// session Δ's reference.
func (m *Model) SetSessionBase(v complexity.View) {
	if m.hasSessionBase && v.Gen == m.sessionBase.Gen {
		return
	}
	m.sessionBase, m.hasSessionBase = v, true
	m.sessionKey = nil
	m.Refresh()
}

// SessionDelta reports the Project tab's excess minus the session base's
// and whether there is a base (tests).
func (m *Model) SessionDelta() (int, bool) { return m.sessionDelta, m.hasSessionBase }

// TogglePin builds the PinToggleMsg for the selected row: a function row
// (a site or candidate row stands for its function, a Changes row for its
// working-tree side) or a gone pin. A group or Changes file row, and a
// removed function, yield "" and the reason the footer shows.
func (m *Model) TogglePin() (tea.Cmd, string) {
	r, ok := m.Selected()
	if !ok {
		return nil, "no row selected"
	}
	if r.child() && r.Parent >= 0 && r.Parent < len(m.rows) {
		r = m.rows[r.Parent]
	}
	switch {
	case r.Group != nil || r.FileChange != nil:
		return nil, "p pins a function row, not a group"
	case r.Change == complexity.Removed:
		return nil, "a removed function cannot be pinned"
	}
	msg := PinToggleMsg{Rel: r.Rel, Fn: r.Fn, Ordinal: r.Ordinal}
	if r.Change != 0 {
		msg.Ordinal = m.changeOrdinal(r)
	}
	return func() tea.Msg { return msg }, ""
}

// togglePin is the 'p' key: the message, or the reason in the footer.
func (m *Model) togglePin() tea.Cmd {
	cmd, why := m.TogglePin()
	m.note = why
	return cmd
}

// markPins flags the pinned rows of a population and, on the ungrouped
// Project tab, moves them into the Pinned section and adds a (gone) row
// per pin the settled snapshot no longer holds.
func (m *Model) markPins(rows []Row) []Row {
	m.pinSection = false
	set := m.pins.Set()
	if set == nil {
		return rows
	}
	section := m.tab == TabProject && m.Grouping() == complexity.ByNone && !m.treeActive()
	found := map[complexity.EntryKey]bool{}
	for i := range rows {
		r := &rows[i]
		if r.Change == complexity.Removed {
			continue
		}
		ord := r.Ordinal
		if r.Change != 0 {
			ord = m.changeOrdinal(*r)
		}
		k := complexity.EntryKey{File: r.Rel, Function: r.Fn.Name, Ordinal: ord}
		p, ok := set[k]
		if !ok {
			continue
		}
		r.Pin = &p
		found[k] = true
		if section {
			r.PinRow = true
			m.pinSection = true
		}
	}
	if !section || !m.hasView || m.stats.Scanning {
		return rows
	}
	for _, p := range m.pins.Pinned {
		if found[p.Key()] {
			continue
		}
		p := p
		path := filepath.Join(m.root, filepath.FromSlash(p.File))
		rows = append(rows, Row{Path: path, Rel: p.File, Lang: m.pathLang(path), Ordinal: p.Ordinal,
			Fn: complexity.Function{Name: p.Function, CCN: p.CCN, Cognitive: p.Cognitive}, Pin: &p, PinRow: true, Gone: true})
		m.pinSection = true
	}
	return rows
}

// sinceCell is the `since pin` column: the pinned → current metrics of a
// row of the Pinned section, (gone) for a vanished function.
func (r Row) sinceCell() string {
	switch {
	case !r.PinRow || r.Pin == nil:
		return ""
	case r.Gone:
		return "(gone)"
	}
	return r.Pin.SinceText(r.Fn)
}

// sinceValue is the column's sort key: the summed delta.
func (r Row) sinceValue() int {
	if !r.PinRow || r.Pin == nil || r.Gone {
		return 0
	}
	dc, dg := r.Pin.SincePin(r.Fn)
	return dc + dg
}

// sinceTone reads the since-pin deltas: rose when a metric grew, fell
// when one shrank and none grew.
func (r Row) sinceTone() (rose, fell bool) {
	if !r.PinRow || r.Pin == nil || r.Gone {
		return false, false
	}
	dc, dg := r.Pin.SincePin(r.Fn)
	rose = dc > 0 || dg > 0
	return rose, !rose && (dc < 0 || dg < 0)
}

// pinnedMatches reads a pinned: filter value: yes keeps pinned functions
// (groups: with a pinned member), no the rest.
func pinnedMatches(v string, r Row) bool {
	want, ok := testValue(v)
	if !ok {
		return false
	}
	pinned := r.Pin != nil
	if r.Group != nil {
		pinned = false
		for _, mem := range r.Members {
			if mem.Pin != nil {
				pinned = true
				break
			}
		}
	}
	return pinned == want
}

// sessionCacheKey is what the session base's excess depends on besides
// the snapshot.
type sessionCacheKey struct {
	hideTests        bool
	ccnWarn, cogWarn int
	baseline         *complexity.Baseline
	root             string
}

// updateSessionDelta recomputes the Project tab's session Δ: the current
// excess minus the session base's, the base measured under the same
// thresholds, baseline and test-file rule (cached until one changes).
func (m *Model) updateSessionDelta() {
	if !m.hasSessionBase || m.tab != TabProject {
		return
	}
	key := sessionCacheKey{m.hideTests, m.ccnWarn, m.cogWarn, m.baseline, m.root}
	if m.sessionKey == nil || *m.sessionKey != key {
		excess := 0
		for _, p := range m.sessionBase.Paths() {
			if m.hideTests && complexity.IsTestFile(m.root, p) {
				continue
			}
			fns, _ := m.classify(m.relPath(p), m.sessionBase.Files[p])
			for _, f := range fns {
				if f.Baseline != complexity.BaselineAccepted {
					excess += f.Excess(m.ccnWarn, m.cogWarn)
				}
			}
		}
		m.sessionExcess, m.sessionKey = excess, &key
	}
	m.sessionDelta = m.summary.Excess - m.sessionExcess
}

// sessionTotals is the header's "session Δexcess −120", "" without a base
// or when nothing moved.
func (m *Model) sessionTotals() string {
	if !m.hasSessionBase || m.sessionDelta == 0 {
		return ""
	}
	return "session Δexcess " + complexity.SignedDelta(m.sessionDelta)
}

// pinFooter is the footer's pin hint, the last 'p' refusal while it holds.
func (m *Model) pinFooter() string {
	if m.note != "" {
		return " · " + m.note
	}
	return " · p pin"
}
