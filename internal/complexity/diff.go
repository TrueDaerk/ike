package complexity

// diff.go is the pure half of "changes vs HEAD" (Roadmap 0550, #2918): it
// compares the functions measured on a base revision of a file (or a set of
// files) against the ones measured on the working tree and classifies every
// unit as changed, added or removed, then renders a verdict for the whole
// change. No git, no UI: the app fetches the base content inside a tea.Cmd
// and the Changes tab of the Complexity window renders the result.
//
// Matching is by (Name, ordinal among same-named units): the k-th "handle"
// of the base pairs with the k-th "handle" of the head, so overloads, nested
// closures ("(anonymous)#2") and repeated local helpers line up in source
// order. A renamed function is a removal plus an addition — the engine has
// no body hash to tell a rename from a rewrite, and a rename that keeps the
// metrics is still a change worth seeing.

import "sort"

// ChangeKind classifies one function of a diff.
type ChangeKind int

const (
	// Changed is a unit present on both sides whose metrics differ.
	Changed ChangeKind = iota + 1
	// Added is a unit only the head has.
	Added
	// Removed is a unit only the base has.
	Removed
)

// String names the kind for a header or a CSV column.
func (k ChangeKind) String() string {
	switch k {
	case Changed:
		return "changed"
	case Added:
		return "added"
	case Removed:
		return "removed"
	}
	return ""
}

// Change is one matched unit whose metrics moved: Before is the base side,
// After the head side.
type Change struct {
	Before Function `json:"before"`
	After  Function `json:"after"`
}

// DeltaCCN is After.CCN - Before.CCN.
func (c Change) DeltaCCN() int { return c.After.CCN - c.Before.CCN }

// DeltaCognitive is After.Cognitive - Before.Cognitive.
func (c Change) DeltaCognitive() int { return c.After.Cognitive - c.Before.Cognitive }

// DeltaNLOC is After.NLOC - Before.NLOC.
func (c Change) DeltaNLOC() int { return c.After.NLOC - c.Before.NLOC }

// FileDiff is the classified comparison of one file. Changed is sorted by
// |ΔCCN| descending (ties by |Δcognitive|, then by head line), Added and
// Removed keep source order.
type FileDiff struct {
	Changed []Change   `json:"changed"`
	Added   []Function `json:"added"`
	Removed []Function `json:"removed"`
}

// Empty reports a file whose functions all kept their metrics.
func (d FileDiff) Empty() bool {
	return len(d.Changed) == 0 && len(d.Added) == 0 && len(d.Removed) == 0
}

// metricsEqual reports whether two units carry the same five metrics; the
// position (Line / EndLine) and the kind are not metrics — a function that
// only moved is unchanged.
func metricsEqual(a, b Function) bool {
	return a.CCN == b.CCN && a.Cognitive == b.Cognitive && a.NLOC == b.NLOC &&
		a.Params == b.Params && a.Depth == b.Depth
}

// DiffFunctions compares the base measurement of a file with its head
// measurement. Units pair by (Name, ordinal among same-named units); a pair
// with different metrics is Changed, an unpaired head unit Added, an
// unpaired base unit Removed. Both slices are read in order and not
// modified.
func DiffFunctions(base, head []Function) FileDiff {
	var d FileDiff
	// Ordinal cursors per name into the base: the next unmatched base unit of
	// that name, in source order.
	byName := map[string][]int{}
	for i, f := range base {
		byName[f.Name] = append(byName[f.Name], i)
	}
	matched := make([]bool, len(base))
	next := map[string]int{}
	for _, h := range head {
		idxs := byName[h.Name]
		k := next[h.Name]
		if k >= len(idxs) {
			d.Added = append(d.Added, h)
			continue
		}
		next[h.Name] = k + 1
		bi := idxs[k]
		matched[bi] = true
		if !metricsEqual(base[bi], h) {
			d.Changed = append(d.Changed, Change{Before: base[bi], After: h})
		}
	}
	for i, f := range base {
		if !matched[i] {
			d.Removed = append(d.Removed, f)
		}
	}
	sortChanges(d.Changed)
	return d
}

// sortChanges orders by |ΔCCN| descending, then |Δcognitive| descending,
// then by the head line so the order is stable.
func sortChanges(cs []Change) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := abs(cs[i].DeltaCCN()), abs(cs[j].DeltaCCN())
		if a != b {
			return a > b
		}
		if a, b := abs(cs[i].DeltaCognitive()), abs(cs[j].DeltaCognitive()); a != b {
			return a > b
		}
		return cs[i].After.Line < cs[j].After.Line
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Verdict is the one-word reading of a project diff.
type Verdict int

const (
	// Unchanged: the summed CCN and the summed cognitive complexity both
	// stayed where they were.
	Unchanged Verdict = iota
	// MoreComplex: the summed CCN rose, or stayed while the summed cognitive
	// complexity rose.
	MoreComplex
	// LessComplex: the summed CCN fell, or stayed while the summed cognitive
	// complexity fell.
	LessComplex
)

// String renders the verdict for the header and the CLI.
func (v Verdict) String() string {
	switch v {
	case MoreComplex:
		return "more complex"
	case LessComplex:
		return "less complex"
	}
	return "unchanged"
}

// Diff is the comparison of a set of files: the per-file classification
// (files whose units all kept their metrics are omitted), the summaries of
// both sides over every file handed in, and the verdict.
type Diff struct {
	// Files maps a path to its classification. The key is the path the
	// caller used — the head path, or the base path for a deleted file.
	Files map[string]FileDiff `json:"files"`
	// Base and Head summarise every function of the respective side across
	// all files handed to DiffProject.
	Base Summary `json:"base"`
	Head Summary `json:"head"`
	// Verdict reads the ΣCCN delta, Σcognitive as the tiebreaker.
	Verdict Verdict `json:"verdict"`
}

// Paths lists the files with a non-empty classification, sorted.
func (d Diff) Paths() []string {
	out := make([]string, 0, len(d.Files))
	for p := range d.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Counts reports the number of changed, added and removed units across all
// files.
func (d Diff) Counts() (changed, added, removed int) {
	for _, f := range d.Files {
		changed += len(f.Changed)
		added += len(f.Added)
		removed += len(f.Removed)
	}
	return changed, added, removed
}

// Empty reports a diff without a single classified unit.
func (d Diff) Empty() bool {
	c, a, r := d.Counts()
	return c+a+r == 0
}

// DeltaCCN is Head.CCNSum - Base.CCNSum.
func (d Diff) DeltaCCN() int { return d.Head.CCNSum - d.Base.CCNSum }

// DeltaCognitive is Head.CogSum - Base.CogSum.
func (d Diff) DeltaCognitive() int { return d.Head.CogSum - d.Base.CogSum }

// DiffProject compares two measurements keyed by path: base holds the
// functions of every changed file at the base revision (absent for a file
// the base does not have), head the working-tree functions (absent for a
// deleted file). A path present in only one map diffs against nothing. The
// summaries use the warning thresholds exactly like Summarize.
func DiffProject(base, head map[string][]Function, ccnWarn, cogWarn int) Diff {
	d := Diff{Files: map[string]FileDiff{}}
	var baseAll, headAll []Function
	seen := map[string]bool{}
	for p, b := range base {
		seen[p] = true
		baseAll = append(baseAll, b...)
		h := head[p]
		headAll = append(headAll, h...)
		if fd := DiffFunctions(b, h); !fd.Empty() {
			d.Files[p] = fd
		}
	}
	for p, h := range head {
		if seen[p] {
			continue
		}
		headAll = append(headAll, h...)
		if fd := DiffFunctions(nil, h); !fd.Empty() {
			d.Files[p] = fd
		}
	}
	d.Base = Summarize(baseAll, ccnWarn, cogWarn)
	d.Head = Summarize(headAll, ccnWarn, cogWarn)
	d.Verdict = verdict(d.DeltaCCN(), d.DeltaCognitive())
	return d
}

// verdict reads the summed deltas: CCN decides, cognitive breaks the tie.
func verdict(dCCN, dCog int) Verdict {
	switch {
	case dCCN > 0:
		return MoreComplex
	case dCCN < 0:
		return LessComplex
	case dCog > 0:
		return MoreComplex
	case dCog < 0:
		return LessComplex
	}
	return Unchanged
}
