package complexitypanel

import (
	"strings"

	"ike/internal/complexity"
)

// changesgroup.go is the per-file grouping of the Changes tab (#2955): 'b'
// cycles none → file there, replacing the classified units with one row
// per file (complexity.Diff.ByFile) — the counts and both sides of every
// sum with the delta. The metric columns sort and min: / max: read |Δ|, a
// positive Δ takes the warning colour, and the section column disappears.

// changesFileLayout is the Changes tab's file-level layout.
var (
	changesFileLayout = []Column{ColFile, ColChangedN, ColAddedN, ColRemovedN, ColCCN, ColCog, ColNLOC, ColExcess}
	groupCycleChanges = []complexity.Level{complexity.ByNone, complexity.ByFile}
)

// changesFileRows folds the Changes tab's unit rows into one row per file,
// in the diff's own order; a file whose rows are all hidden drops out.
func (m *Model) changesFileRows(fnRows []Row) []Row {
	byPath := map[string][]Row{}
	for _, r := range fnRows {
		byPath[r.Path] = append(byPath[r.Path], r)
	}
	var out []Row
	for _, fc := range m.changesDiff().ByFile() {
		rows := byPath[fc.File]
		if len(rows) == 0 {
			continue
		}
		fc := fc
		src := rows[0]
		out = append(out, Row{Path: fc.File, Rel: src.Rel, Lang: src.Lang, Test: src.Test, Dirty: src.Dirty,
			FileChange: &fc, Members: rows})
	}
	return out
}

// fileChangeValue is value for a Changes file row: the counts, and |Δ| on
// the metric columns so the sort and min: / max: read the size of a move.
func (r Row) fileChangeValue(c Column) (float64, string) {
	fc := r.FileChange
	switch c {
	case ColFile:
		return 0, r.Rel
	case ColChangedN:
		return float64(fc.Changed), ""
	case ColAddedN:
		return float64(fc.Added), ""
	case ColRemovedN:
		return float64(fc.Removed), ""
	}
	return float64(abs(fileChangeDelta(fc, c))), ""
}

// fileChangeDelta is a file row's after − before on a metric column.
func fileChangeDelta(fc *complexity.FileChange, c Column) int {
	switch c {
	case ColCCN:
		return fc.DeltaCCN()
	case ColCog:
		return fc.DeltaCognitive()
	case ColNLOC:
		return fc.DeltaNLOC()
	case ColExcess:
		return fc.DeltaExcess()
	}
	return 0
}

// fileChangeCell is cell for a Changes file row: "before→after (Δ)", the
// NLOC without its delta like on the unit rows.
func (r Row) fileChangeCell(c Column) string {
	fc := r.FileChange
	switch c {
	case ColFile:
		if r.Dirty {
			return r.Rel + dirtyMark
		}
		return r.Rel
	case ColChangedN:
		return itoa(fc.Changed)
	case ColAddedN:
		return itoa(fc.Added)
	case ColRemovedN:
		return itoa(fc.Removed)
	case ColCCN:
		return sidesCell(fc.Before.CCNSum, fc.After.CCNSum, true)
	case ColCog:
		return sidesCell(fc.Before.CogSum, fc.After.CogSum, true)
	case ColNLOC:
		return sidesCell(fc.Before.NLOC, fc.After.NLOC, false)
	case ColExcess:
		return sidesCell(fc.Before.Excess, fc.After.Excess, true)
	}
	return ""
}

// sidesCell renders "b→a", with " (Δ)" when withDelta.
func sidesCell(b, a int, withDelta bool) string {
	s := itoa(b) + "→" + itoa(a)
	if withDelta {
		s += " (" + signed(a-b) + ")"
	}
	return s
}

// fileChangeLine is where enter opens a file row: the head line of its
// first changed or added function, the top when it only lost functions.
func (r Row) fileChangeLine() int {
	line := -1
	for _, m := range r.Members {
		if m.Change == complexity.Removed {
			continue
		}
		if line < 0 || m.Fn.Line < line {
			line = m.Fn.Line
		}
	}
	return max(line, 0)
}

// fileChangeCopyText is CopyText for a Changes file row: "path: changed 3
// added 1 removed 0 ccn 40→52 (+12) cog 31→44 (+13)".
func fileChangeCopyText(r Row) string {
	fc := r.FileChange
	return r.Rel + ": changed " + itoa(fc.Changed) + " added " + itoa(fc.Added) + " removed " + itoa(fc.Removed) +
		" ccn " + sidesCell(fc.Before.CCNSum, fc.After.CCNSum, true) +
		" cog " + sidesCell(fc.Before.CogSum, fc.After.CogSum, true)
}

// changesFileCSVHeader is the file-level Changes export's header, the
// CLI's JSON names.
var changesFileCSVHeader = []string{"file", "changed", "added", "removed",
	"ccn_before", "ccn_after", "ccn_delta", "cog_before", "cog_after", "cog_delta", "nloc_before", "nloc_after"}

// changesFileCSV renders the visible Changes file rows.
func (m *Model) changesFileCSV() string {
	var b strings.Builder
	writeCSVRow(&b, changesFileCSVHeader)
	for _, r := range m.rows {
		fc := r.FileChange
		writeCSVRow(&b, []string{r.Rel, itoa(fc.Changed), itoa(fc.Added), itoa(fc.Removed),
			itoa(fc.Before.CCNSum), itoa(fc.After.CCNSum), itoa(fc.DeltaCCN()),
			itoa(fc.Before.CogSum), itoa(fc.After.CogSum), itoa(fc.DeltaCognitive()),
			itoa(fc.Before.NLOC), itoa(fc.After.NLOC)})
	}
	return b.String()
}
