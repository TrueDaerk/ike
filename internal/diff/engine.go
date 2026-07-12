// Package diff is the reusable diff viewer (#60): a line-level Myers diff
// engine with intra-line refinement, plus a pane model rendering two text
// versions side by side or unified. It is shared infrastructure — VCS status
// (#28), local history (#35), and the external-change conflict guard (#53)
// open it; on its own it is reachable through the diff.files palette command.
//
// engine.go is the pure computation half: no rendering, no bubbletea. Lines
// computes the line-level edit script; Compute pairs delete/insert runs into
// changed line pairs, refines them at rune level into per-side spans, and
// groups the result into hunks for n/N navigation.
package diff

// Op classifies one edit-script entry.
type Op int

const (
	// OpEqual is a line present in both versions.
	OpEqual Op = iota
	// OpDelete is a line only in the left (old) version.
	OpDelete
	// OpInsert is a line only in the right (new) version.
	OpInsert
)

// Edit is one line of the edit script produced by Lines.
type Edit struct {
	Op   Op
	Text string
}

// Kind classifies one aligned display row.
type Kind int

const (
	// RowSame is an unchanged line, present on both sides.
	RowSame Kind = iota
	// RowChanged is a paired old/new line with intra-line differences.
	RowChanged
	// RowRemoved is a left-only line; the right column shows a gap.
	RowRemoved
	// RowAdded is a right-only line; the left column shows a gap.
	RowAdded
)

// Span is a changed rune range [Start, End) within one side of a changed
// line pair, for intra-line emphasis.
type Span struct {
	Start, End int
}

// Row is one aligned display row of the diff: an unchanged line, a changed
// pair, or a one-sided add/remove with a gap on the other side. Line numbers
// are 1-based; 0 marks the gap side.
type Row struct {
	Kind    Kind
	LeftNo  int
	RightNo int
	Left    string
	Right   string
	// LeftSpans/RightSpans are the intra-line changed ranges of a RowChanged
	// pair, in rune columns of Left/Right.
	LeftSpans  []Span
	RightSpans []Span
}

// Hunk is one contiguous run of non-RowSame rows: [Start, End) row indices.
type Hunk struct {
	Start, End int
}

// Result is a computed diff ready for rendering: the aligned rows and the
// hunks over them.
type Result struct {
	Rows  []Row
	Hunks []Hunk
}

// maxRefineRunes bounds intra-line refinement: rune-level Myers is quadratic
// in the worst case, and emphasis inside very long lines is unreadable anyway.
const maxRefineRunes = 400

// Lines computes the line-level edit script turning a into b, using Myers'
// greedy O(ND) algorithm with common prefix/suffix trimming.
func Lines(a, b []string) []Edit {
	return script(a, b)
}

// Compute diffs two texts (split on '\n') into aligned rows and hunks.
func Compute(left, right string) Result {
	a := splitLines(left)
	b := splitLines(right)
	rows := buildRows(script(a, b))
	return Result{Rows: rows, Hunks: hunksOf(rows)}
}

// splitLines splits text on '\n', treating the empty text as zero lines so an
// empty side diffs as pure inserts/deletes instead of one phantom empty line.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	n := 1
	for _, r := range text {
		if r == '\n' {
			n++
		}
	}
	out := make([]string, 0, n)
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	return append(out, text[start:])
}

// buildRows folds the edit script into aligned display rows: runs of deletes
// followed by inserts pair up positionally into changed rows (with intra-line
// spans); the unpaired remainder stays one-sided.
func buildRows(edits []Edit) []Row {
	var rows []Row
	leftNo, rightNo := 0, 0
	i := 0
	for i < len(edits) {
		switch edits[i].Op {
		case OpEqual:
			leftNo++
			rightNo++
			rows = append(rows, Row{Kind: RowSame, LeftNo: leftNo, RightNo: rightNo, Left: edits[i].Text, Right: edits[i].Text})
			i++
		default:
			// Collect the maximal delete run then insert run.
			var dels, ins []string
			for i < len(edits) && edits[i].Op == OpDelete {
				dels = append(dels, edits[i].Text)
				i++
			}
			for i < len(edits) && edits[i].Op == OpInsert {
				ins = append(ins, edits[i].Text)
				i++
			}
			pairs := min(len(dels), len(ins))
			for p := 0; p < pairs; p++ {
				leftNo++
				rightNo++
				ls, rs := refine(dels[p], ins[p])
				rows = append(rows, Row{
					Kind: RowChanged, LeftNo: leftNo, RightNo: rightNo,
					Left: dels[p], Right: ins[p], LeftSpans: ls, RightSpans: rs,
				})
			}
			for p := pairs; p < len(dels); p++ {
				leftNo++
				rows = append(rows, Row{Kind: RowRemoved, LeftNo: leftNo, Left: dels[p]})
			}
			for p := pairs; p < len(ins); p++ {
				rightNo++
				rows = append(rows, Row{Kind: RowAdded, RightNo: rightNo, Right: ins[p]})
			}
		}
	}
	return rows
}

// hunksOf finds the contiguous runs of non-RowSame rows.
func hunksOf(rows []Row) []Hunk {
	var hunks []Hunk
	start := -1
	for i, r := range rows {
		if r.Kind != RowSame {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			hunks = append(hunks, Hunk{Start: start, End: i})
			start = -1
		}
	}
	if start >= 0 {
		hunks = append(hunks, Hunk{Start: start, End: len(rows)})
	}
	return hunks
}

// refine runs a rune-level diff over a changed line pair and returns the
// changed spans on each side. Oversized lines skip refinement (whole-line
// emphasis reads better than quadratic work).
func refine(left, right string) (ls, rs []Span) {
	lr := []rune(left)
	rr := []rune(right)
	if len(lr) > maxRefineRunes || len(rr) > maxRefineRunes {
		return nil, nil
	}
	edits := runeScript(lr, rr)
	li, ri := 0, 0
	for _, e := range edits {
		switch e.op {
		case OpEqual:
			li += e.n
			ri += e.n
		case OpDelete:
			ls = appendSpan(ls, li, li+e.n)
			li += e.n
		case OpInsert:
			rs = appendSpan(rs, ri, ri+e.n)
			ri += e.n
		}
	}
	return ls, rs
}

// appendSpan appends [start, end), merging into the previous span when they
// touch (adjacent delete/insert runs emphasize as one region).
func appendSpan(spans []Span, start, end int) []Span {
	if n := len(spans); n > 0 && spans[n-1].End >= start {
		if end > spans[n-1].End {
			spans[n-1].End = end
		}
		return spans
	}
	return append(spans, Span{Start: start, End: end})
}

// runEdit is a run-length edit for rune-level scripts.
type runEdit struct {
	op Op
	n  int
}

// script computes the line-level edit script via Myers.
func script(a, b []string) []Edit {
	// Trim the common prefix and suffix — typical edits touch a small region,
	// and Myers cost grows with the differing middle.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	mid := myers(a[pre:len(a)-suf], b[pre:len(b)-suf])
	edits := make([]Edit, 0, pre+len(mid)+suf)
	for _, l := range a[:pre] {
		edits = append(edits, Edit{Op: OpEqual, Text: l})
	}
	edits = append(edits, mid...)
	for _, l := range a[len(a)-suf:] {
		edits = append(edits, Edit{Op: OpEqual, Text: l})
	}
	return edits
}

// runeScript computes a run-length rune-level edit script via the same Myers
// core, for intra-line refinement.
func runeScript(a, b []rune) []runEdit {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	trace := myersTrace(runeSeq{a[pre : len(a)-suf]}, runeSeq{b[pre : len(b)-suf]})
	var out []runEdit
	if pre > 0 {
		out = append(out, runEdit{op: OpEqual, n: pre})
	}
	for _, op := range trace {
		if n := len(out); n > 0 && out[n-1].op == op {
			out[n-1].n++
			continue
		}
		out = append(out, runEdit{op: op, n: 1})
	}
	if suf > 0 {
		if n := len(out); n > 0 && out[n-1].op == OpEqual {
			out[n-1].n += suf
		} else {
			out = append(out, runEdit{op: OpEqual, n: suf})
		}
	}
	return out
}

// myers runs the Myers core over line slices and expands the op trace into
// line-carrying edits.
func myers(a, b []string) []Edit {
	ops := myersTrace(stringSeq{a}, stringSeq{b})
	edits := make([]Edit, 0, len(ops))
	ai, bi := 0, 0
	for _, op := range ops {
		switch op {
		case OpEqual:
			edits = append(edits, Edit{Op: OpEqual, Text: a[ai]})
			ai++
			bi++
		case OpDelete:
			edits = append(edits, Edit{Op: OpDelete, Text: a[ai]})
			ai++
		case OpInsert:
			edits = append(edits, Edit{Op: OpInsert, Text: b[bi]})
			bi++
		}
	}
	return edits
}

// seq abstracts the two element types (lines, runes) the Myers core walks.
type seq interface {
	Len() int
	Eq(other seq, i, j int) bool
}

type stringSeq struct{ s []string }

func (q stringSeq) Len() int { return len(q.s) }
func (q stringSeq) Eq(other seq, i, j int) bool {
	return q.s[i] == other.(stringSeq).s[j]
}

type runeSeq struct{ r []rune }

func (q runeSeq) Len() int { return len(q.r) }
func (q runeSeq) Eq(other seq, i, j int) bool {
	return q.r[i] == other.(runeSeq).r[j]
}

// myersTrace is the greedy O(ND) Myers diff (An O(ND) Difference Algorithm,
// Myers 1986) returning the per-element op sequence turning a into b. The
// D-round snapshots of the furthest-reaching x per diagonal are kept for the
// backtrack.
func myersTrace(a, b seq) []Op {
	n, m := a.Len(), b.Len()
	switch {
	case n == 0 && m == 0:
		return nil
	case n == 0:
		return repeatOp(OpInsert, m)
	case m == 0:
		return repeatOp(OpDelete, n)
	}
	max := n + m
	// v[k+max] is the furthest x on diagonal k.
	v := make([]int, 2*max+1)
	var snapshots [][]int
	var dFound = -1
outer:
	for d := 0; d <= max; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1+max] < v[k+1+max]) {
				x = v[k+1+max] // down: insert from b
			} else {
				x = v[k-1+max] + 1 // right: delete from a
			}
			y := x - k
			for x < n && y < m && a.Eq(b, x, y) {
				x++
				y++
			}
			v[k+max] = x
			if x >= n && y >= m {
				snap := make([]int, len(v))
				copy(snap, v)
				snapshots = append(snapshots, snap)
				dFound = d
				break outer
			}
		}
		snap := make([]int, len(v))
		copy(snap, v)
		snapshots = append(snapshots, snap)
	}
	// Backtrack from (n, m) through the D-round snapshots.
	var rev []Op
	x, y := n, m
	for d := dFound; d > 0; d-- {
		vPrev := snapshots[d-1]
		k := x - y
		var prevK int
		if k == -d || (k != d && vPrev[k-1+max] < vPrev[k+1+max]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vPrev[prevK+max]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			rev = append(rev, OpEqual)
			x--
			y--
		}
		if prevK == k+1 {
			rev = append(rev, OpInsert) // came from below: b[prevY] inserted
			y--
		} else {
			rev = append(rev, OpDelete) // came from the left: a[prevX] deleted
			x--
		}
	}
	for x > 0 && y > 0 {
		rev = append(rev, OpEqual)
		x--
		y--
	}
	for ; x > 0; x-- {
		rev = append(rev, OpDelete)
	}
	for ; y > 0; y-- {
		rev = append(rev, OpInsert)
	}
	// Reverse into forward order.
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func repeatOp(op Op, n int) []Op {
	out := make([]Op, n)
	for i := range out {
		out[i] = op
	}
	return out
}
