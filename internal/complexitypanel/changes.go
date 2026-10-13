package complexitypanel

// changes.go is the Changes tab of the Complexity window (Roadmap 0550,
// #2918): the functions whose metrics differ between a base revision and
// the working tree, classified changed / added / removed by the pure diff
// in internal/complexity, with a verdict in the header. The pane stays a
// consumer: the root model fetches the base content and measures both
// sides off the Update goroutine, then hands the result in via SetChanges;
// 'R' asks it — through SetBaseMsg — to compare against another revision.

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/ui"
)

// Changes is the data behind the Changes tab, handed in whole by the root
// model.
type Changes struct {
	// Rev is the base revision as the user named it ("HEAD", "main",
	// "HEAD~3"); Sha the commit it resolved to, "" before the first run.
	Rev string
	Sha string
	// Diff is the classified comparison, keyed by absolute path (the base
	// path for a deleted file).
	Diff complexity.Diff
	// Dirty marks the absolute paths whose head side came from an unsaved
	// buffer: their rows carry the editor tabs' '*'.
	Dirty map[string]bool
	// Renames notes every renamed file as "old → new": each shows up as
	// removed + added, and the header says why.
	Renames []string
	// Err is the git message of a rejected base revision ('R' with a ref
	// that does not resolve); the previous base stays in effect.
	Err string
	// Computing is true while the root model measures; Ready once a diff
	// for Rev has landed at least once; NotGit when the project is not a
	// repository and the tab has nothing to compare.
	Computing bool
	Ready     bool
	NotGit    bool
}

// SetBaseMsg asks the root model to compare against another base revision
// ('R' + enter); an empty Rev means HEAD.
type SetBaseMsg struct{ Rev string }

// SetChanges replaces the Changes tab's data and rebuilds its rows.
func (m *Model) SetChanges(c Changes) {
	m.changes = c
	if m.tab == TabChanges {
		m.Refresh()
	}
}

// Changes reports the tab's current data (tests, the status segment).
func (m *Model) Changes() Changes { return m.changes }

// Prompting reports whether the base-revision prompt holds the keyboard.
func (m *Model) Prompting() bool { return m.prompting }

// changesRows lists the Changes tab's population: one row per classified
// unit, section order changed, added, removed.
func (m *Model) changesRows() []Row {
	var out []Row
	d := m.changes.Diff
	for _, p := range d.Paths() {
		fd := d.Files[p]
		rel, id := m.relPath(p), m.pathLang(p)
		dirty := m.changes.Dirty[p]
		for _, c := range fd.Changed {
			out = append(out, Row{Path: p, Rel: rel, Lang: id, Fn: c.After, Before: c.Before, Change: complexity.Changed, Dirty: dirty})
		}
		for _, f := range fd.Added {
			out = append(out, Row{Path: p, Rel: rel, Lang: id, Fn: f, Change: complexity.Added, Dirty: dirty})
		}
		for _, f := range fd.Removed {
			out = append(out, Row{Path: p, Rel: rel, Lang: id, Fn: f, Before: f, Change: complexity.Removed, Dirty: dirty})
		}
	}
	return out
}

// before / after are the two sides of a metric for a Changes row; an
// absent side reads as zero and is rendered as "-".
func (r Row) sides(pick func(complexity.Function) int) (before, after int) {
	switch r.Change {
	case complexity.Added:
		return 0, pick(r.Fn)
	case complexity.Removed:
		return pick(r.Before), 0
	}
	return pick(r.Before), pick(r.Fn)
}

func ccnOf(f complexity.Function) int  { return f.CCN }
func cogOf(f complexity.Function) int  { return f.Cognitive }
func nlocOf(f complexity.Function) int { return f.NLOC }

// cmtOf is the comment density in whole percent (#2934).
func cmtOf(f complexity.Function) int { return complexity.DensityPercent(f.CommentDensity) }

// DeltaCCN is the row's CCN delta: after - before, the whole value for an
// added unit, its negation for a removed one.
func (r Row) DeltaCCN() int { b, a := r.sides(ccnOf); return a - b }

// DeltaCognitive is the cognitive delta (see DeltaCCN).
func (r Row) DeltaCognitive() int { b, a := r.sides(cogOf); return a - b }

// DeltaNLOC is the NLOC delta (see DeltaCCN).
func (r Row) DeltaNLOC() int { b, a := r.sides(nlocOf); return a - b }

// DeltaCmt is the comment-density delta in percentage points (see
// DeltaCCN).
func (r Row) DeltaCmt() int { b, a := r.sides(cmtOf); return a - b }

// metricCell renders "before→after (Δ)" for a Changes row; the missing
// side of an added or removed unit is "-".
func (r Row) metricCell(pick func(complexity.Function) int, withDelta bool) string {
	b, a := r.sides(pick)
	bs, as := strconv.Itoa(b), strconv.Itoa(a)
	switch r.Change {
	case complexity.Added:
		bs = "-"
	case complexity.Removed:
		as = "-"
	}
	s := bs + "→" + as
	if withDelta {
		s += " (" + signed(a-b) + ")"
	}
	return s
}

// signed renders a delta with its sign ("+2", "-3", "0").
func signed(n int) string {
	if n > 0 {
		return "+" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// sectionRank orders the Changes sections: changed, added, removed.
func (r Row) sectionRank() int { return int(r.Change) }

// changesTotals renders the Changes tab's header: base → working tree,
// the function counts, both sums and the excess with their deltas, the
// verdict, and the
// rename note.
func (m *Model) changesTotals() string {
	c := m.changes
	var parts []string
	switch {
	case c.NotGit:
		return "not a git repository"
	case c.Err != "":
		parts = append(parts, "base "+c.Rev+" kept · "+c.Err)
	case c.Computing:
		parts = append(parts, "computing…")
	}
	rev := c.Rev
	if rev == "" {
		rev = "HEAD"
	}
	d := c.Diff
	parts = append(parts,
		"base "+rev+" → working tree",
		"functions "+itoa(d.Base.Functions)+"→"+itoa(d.Head.Functions),
		"ΣCCN "+itoa(d.Base.CCNSum)+"→"+itoa(d.Head.CCNSum)+" ("+signed(d.DeltaCCN())+")",
		"Σcog "+itoa(d.Base.CogSum)+"→"+itoa(d.Head.CogSum)+" ("+signed(d.DeltaCognitive())+")",
		"excess "+itoa(d.Base.Excess)+"→"+itoa(d.Head.Excess)+" ("+signed(d.DeltaExcess())+")",
		d.Verdict.String(),
	)
	if n := len(c.Renames); n > 0 {
		parts = append(parts, itoa(n)+" renamed (removed + added): "+strings.Join(c.Renames, ", "))
	}
	return strings.Join(parts, " · ")
}

// changesEmpty explains an empty Changes list.
func (m *Model) changesEmpty() string {
	c := m.changes
	switch {
	case c.NotGit:
		return "(not a git repository)"
	case c.Computing && !c.Ready:
		return "(measuring the base revision…)"
	case !c.Ready:
		return "(no diff yet — the tab fills once the working tree is compared)"
	case !m.filter.Empty():
		return "(no row matches the filter)"
	}
	rev := c.Rev
	if rev == "" {
		rev = "HEAD"
	}
	return "(no function changed its metrics against " + rev + ")"
}

// promptBase opens the base-revision prompt seeded with the current base.
func (m *Model) promptBase() {
	rev := m.changes.Rev
	if rev == "" {
		rev = "HEAD"
	}
	m.revField = ui.NewField(rev)
	m.revField.SelectAll()
	m.prompting = true
}

// promptKey routes one key to the open prompt: enter confirms, esc cancels,
// the rest edits.
func (m *Model) promptKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		rev := strings.TrimSpace(m.revField.Text)
		m.prompting = false
		return func() tea.Msg { return SetBaseMsg{Rev: rev} }
	case "esc":
		m.prompting = false
		return nil
	}
	m.revField.Key(msg)
	return nil
}

// promptView renders the prompt in the filter row's place.
func (m *Model) promptView() string {
	return m.clip(" base revision (branch, tag, sha, HEAD~3): " + m.revField.View())
}

// changesCSVHeader is the Changes tab's export header.
var changesCSVHeader = []string{"change", "file", "function", "kind", "line",
	"ccn_before", "ccn_after", "ccn_delta", "cog_before", "cog_after", "cog_delta", "nloc_before", "nloc_after",
	"cmt_before", "cmt_after", "cmt_delta"}

// changesCSV renders the Changes tab: both sides of every metric plus the
// deltas, one row per visible unit.
func (m *Model) changesCSV() string {
	var b strings.Builder
	writeCSVRow(&b, changesCSVHeader)
	for _, r := range m.rows {
		cb, ca := r.sides(ccnOf)
		gb, ga := r.sides(cogOf)
		nb, na := r.sides(nlocOf)
		tb, ta := r.sides(cmtOf)
		writeCSVRow(&b, []string{r.Change.String(), r.Rel, r.Fn.Name, r.Fn.Kind, strconv.Itoa(r.Fn.Line + 1),
			strconv.Itoa(cb), strconv.Itoa(ca), strconv.Itoa(ca - cb),
			strconv.Itoa(gb), strconv.Itoa(ga), strconv.Itoa(ga - gb),
			strconv.Itoa(nb), strconv.Itoa(na),
			strconv.Itoa(tb), strconv.Itoa(ta), strconv.Itoa(ta - tb)})
	}
	return b.String()
}
