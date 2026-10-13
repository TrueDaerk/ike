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

// DeltaCommentLines is After.CommentLines - Before.CommentLines.
func (c Change) DeltaCommentLines() int { return c.After.CommentLines - c.Before.CommentLines }

// DeltaCommentDensity is After.CommentDensity - Before.CommentDensity.
func (c Change) DeltaCommentDensity() float64 {
	return c.After.CommentDensity - c.Before.CommentDensity
}

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

// metricsEqual reports whether two units carry the same metrics; the
// position (Line / EndLine) and the kind are not metrics — a function that
// only moved is unchanged. The comment density follows from NLOC and
// CommentLines.
func metricsEqual(a, b Function) bool {
	return a.CCN == b.CCN && a.Cognitive == b.Cognitive && a.NLOC == b.NLOC &&
		a.Params == b.Params && a.Depth == b.Depth && a.CommentLines == b.CommentLines
}

// DiffFunctions compares the base measurement of a file with its head
// measurement. Units pair by (Name, ordinal among same-named units); a pair
// with different metrics is Changed, an unpaired head unit Added, an
// unpaired base unit Removed. Both slices are read in order and not
// modified.
func DiffFunctions(base, head []Function) FileDiff {
	var d FileDiff
	pairs := PairUnits(base, head)
	matched := make([]bool, len(base))
	for i, h := range head {
		bi := pairs[i]
		if bi < 0 {
			d.Added = append(d.Added, h)
			continue
		}
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

// PairUnits pairs head units with base units the way DiffFunctions does
// (#2949): by (Name, ordinal among same-named units in source order). It
// returns, per head unit, the index of its base partner or -1 for a unit
// the base does not have.
func PairUnits(base, head []Function) []int {
	// Ordinal cursors per name into the base: the next unmatched base unit of
	// that name, in source order.
	byName := map[string][]int{}
	for i, f := range base {
		byName[f.Name] = append(byName[f.Name], i)
	}
	next := map[string]int{}
	out := make([]int, len(head))
	for i, h := range head {
		idxs := byName[h.Name]
		k := next[h.Name]
		if k >= len(idxs) {
			out[i] = -1
			continue
		}
		next[h.Name] = k + 1
		out[i] = idxs[k]
	}
	return out
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

// Verdict is the one-word reading of a project diff (#2945): the excess
// delta decides, the summed cognitive complexity breaks a tie, the summed
// CCN breaks the next one — read as decision points (ΣCCN minus the unit
// count), so the base 1 every unit carries never tips it. ΣCCN alone
// punished extracting a block into its own function (+1 per new unit) and
// adding trivial helpers.
type Verdict int

const (
	// Unchanged: the excess, the summed cognitive complexity and the
	// decision points all stayed where they were.
	Unchanged Verdict = iota
	// MoreComplex: the excess rose, or held while Σcognitive rose, or both
	// held while the decision points (ΣCCN − functions) rose.
	MoreComplex
	// LessComplex: the mirror of MoreComplex.
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
	// Verdict reads the excess delta, then Σcognitive, then ΣCCN − functions.
	Verdict Verdict `json:"verdict"`

	// base / head keep the measurements DiffProject compared, warn its
	// thresholds, so Without can re-run it over a subset of files (#2950).
	base, head map[string][]Function
	warn       [3]int
}

// Without re-runs the comparison without the files drop selects — the
// Complexity window's hide-tests toggle (#2950) — so the summaries and the
// verdict follow the visible files. A diff not built by DiffProject is
// returned as is.
func (d Diff) Without(drop func(path string) bool) Diff {
	if d.base == nil && d.head == nil {
		return d
	}
	keep := func(in map[string][]Function) map[string][]Function {
		out := make(map[string][]Function, len(in))
		for p, fns := range in {
			if !drop(p) {
				out[p] = fns
			}
		}
		return out
	}
	return DiffProject(keep(d.base), keep(d.head), d.warn[0], d.warn[1], d.warn[2])
}

// BaseOf returns the base measurement DiffProject compared for path and
// whether the diff covered path at all (#2949): a file only the head has is
// covered with a nil base, a file outside the diff — unchanged against the
// base — is not. A diff not built by DiffProject covers nothing.
func (d Diff) BaseOf(path string) (base []Function, covered bool) {
	base, inBase := d.base[path]
	_, inHead := d.head[path]
	return base, inBase || inHead
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

// DeltaExcess is Head.Excess - Base.Excess (#2945).
func (d Diff) DeltaExcess() int { return d.Head.Excess - d.Base.Excess }

// DeltaDecisions is the delta of ΣCCN minus the function count: the
// branches, arms and boolean operators gained, without the base 1 of each
// added or removed unit — the verdict's last tiebreak.
func (d Diff) DeltaDecisions() int {
	return (d.Head.CCNSum - d.Head.Functions) - (d.Base.CCNSum - d.Base.Functions)
}

// DiffProject compares two measurements keyed by path: base holds the
// functions of every changed file at the base revision (absent for a file
// the base does not have), head the working-tree functions (absent for a
// deleted file). A path present in only one map diffs against nothing. The
// summaries use the warning thresholds exactly like Summarize.
func DiffProject(base, head map[string][]Function, ccnWarn, cogWarn, cmtWarn int) Diff {
	d := Diff{Files: map[string]FileDiff{}, base: base, head: head, warn: [3]int{ccnWarn, cogWarn, cmtWarn}}
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
	d.Base = Summarize(baseAll, ccnWarn, cogWarn, cmtWarn)
	d.Head = Summarize(headAll, ccnWarn, cogWarn, cmtWarn)
	d.Verdict = verdict(d.DeltaExcess(), d.DeltaCognitive(), d.DeltaDecisions())
	return d
}

// verdict reads the summed deltas in priority order: the first non-zero
// one decides — excess, then cognitive, then decision points.
func verdict(deltas ...int) Verdict {
	for _, d := range deltas {
		switch {
		case d > 0:
			return MoreComplex
		case d < 0:
			return LessComplex
		}
	}
	return Unchanged
}

// FileChange is one file of a diff folded into a row (#2955): its
// classification counts and the Summary of its units on each side — a
// removed unit counts on the base side only, an added one on the head side
// only — so a branch reads per file: which got heavier, by how much.
type FileChange struct {
	File    string  `json:"file"`
	Changed int     `json:"changed"`
	Added   int     `json:"added"`
	Removed int     `json:"removed"`
	Before  Summary `json:"before"`
	After   Summary `json:"after"`
}

// DeltaCCN is After.CCNSum - Before.CCNSum.
func (f FileChange) DeltaCCN() int { return f.After.CCNSum - f.Before.CCNSum }

// DeltaCognitive is After.CogSum - Before.CogSum.
func (f FileChange) DeltaCognitive() int { return f.After.CogSum - f.Before.CogSum }

// DeltaNLOC is After.NLOC - Before.NLOC.
func (f FileChange) DeltaNLOC() int { return f.After.NLOC - f.Before.NLOC }

// DeltaExcess is After.Excess - Before.Excess.
func (f FileChange) DeltaExcess() int { return f.After.Excess - f.Before.Excess }

// ByFile folds the diff per file (#2955): one FileChange per file with a
// non-empty classification, each side summarised over every unit the diff
// measured for that file (the classified units alone for a diff not built
// by DiffProject), sorted by ΔCCN descending, then by file.
func (d Diff) ByFile() []FileChange {
	out := make([]FileChange, 0, len(d.Files))
	for p, fd := range d.Files {
		base, head := d.base[p], d.head[p]
		if _, covered := d.BaseOf(p); !covered {
			base, head = nil, nil
			for _, c := range fd.Changed {
				base = append(base, c.Before)
				head = append(head, c.After)
			}
			base = append(base, fd.Removed...)
			head = append(head, fd.Added...)
		}
		out = append(out, FileChange{File: p,
			Changed: len(fd.Changed), Added: len(fd.Added), Removed: len(fd.Removed),
			Before: Summarize(base, d.warn[0], d.warn[1], d.warn[2]),
			After:  Summarize(head, d.warn[0], d.warn[1], d.warn[2])})
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].DeltaCCN(), out[j].DeltaCCN(); a != b {
			return a > b
		}
		return out[i].File < out[j].File
	})
	return out
}
