// Package diff is the reusable diff viewer (#60): a line-level Myers diff
// engine with intra-line refinement, plus a pane model rendering two text
// versions side by side or unified. It is shared infrastructure — VCS status
// (#28), local history (#35), and the external-change conflict guard (#53)
// open it; on its own it is reachable through the diff.files palette command.
//
// engine.go is the pure computation half: no rendering, no bubbletea. Lines
// computes the line-level edit script; Compute pairs delete/insert runs into
// changed line pairs, refines them at rune level into per-side spans, and
// groups the result into hunks for n/N navigation. ComputeWith runs the same
// computation under Options — today: ignoring whitespace (#2170).
package diff

import (
	"strings"
	"unicode"
)

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
	// TooLarge marks a refused comparison (#2505): a side was over
	// MaxDiffBytes, so Rows is empty and nothing was computed.
	TooLarge bool
}

// maxRefineRunes bounds intra-line refinement: rune-level Myers is quadratic
// in the worst case, and emphasis inside very long lines is unreadable anyway.
const maxRefineRunes = 400

// maxRefineBytes rejects a line pair before the []rune conversion (#2505): a
// multi-megabyte single line (minified JSON, a spooled body) would allocate
// four bytes per rune just to learn it is over maxRefineRunes anyway — any
// line over 4 KiB has more than maxRefineRunes runes, so the byte length
// decides without allocating.
const maxRefineBytes = 4 << 10

// MaxDiffBytes is the hard per-side input budget of the engine (#2505):
// ComputeWith refuses anything larger outright (Result.TooLarge) instead of
// diffing it, and openers surface the refusal as a notice. Even with the
// bounded Myers core below, a giant side still costs seconds of comparison
// and a full syntax re-parse — past this budget the answer is "no", not
// "slower". A constant, not a setting: no input is allowed to grow past what
// the IDE survives.
const MaxDiffBytes = 2 << 20

// maxMyersRounds caps the Myers D loop (#2505): each round widens the search
// band by one diagonal, so memory and time grow with D — two sides divergent
// beyond this budget fall back to a plain delete-all/insert-all script for
// the (prefix/suffix-trimmed) middle, which buildRows still pairs into
// changed rows. The optimal alignment of 20k+ differing lines is not worth
// gigabytes; before this cap two ~600 KiB responses allocated ~25 GiB.
const maxMyersRounds = 1024

// Lines computes the line-level edit script turning a into b, using Myers'
// greedy O(ND) algorithm with common prefix/suffix trimming.
func Lines(a, b []string) []Edit {
	return script(a, b)
}

// Options tunes how a diff compares its two sides (#2170).
type Options struct {
	// IgnoreWhitespace drops whitespace from every comparison, the way
	// "git diff -w" does: lines differing only in whitespace pair up as
	// unchanged rows (both sides keep their own raw text, so each column
	// still shows what it really holds), and intra-line refinement reports
	// only the ranges that carry non-whitespace changes.
	IgnoreWhitespace bool
}

// Compute diffs two texts (split on '\n') into aligned rows and hunks, with
// the default (whitespace-significant) options.
func Compute(left, right string) Result { return ComputeWith(left, right, Options{}) }

// ComputeWith diffs two texts under opts. Oversized input is refused rather
// than diffed (#2505): the caller gets Result.TooLarge and explains, instead
// of the engine burning seconds and memory on a comparison nobody can read.
func ComputeWith(left, right string, opts Options) Result {
	if TooLarge(left, right) {
		return Result{TooLarge: true}
	}
	a := splitLines(left)
	b := splitLines(right)
	rows := buildRows(pairScript(a, b, opts), opts)
	return Result{Rows: rows, Hunks: hunksOf(rows)}
}

// TooLarge reports whether a side is over the engine's MaxDiffBytes budget
// (#2505) — the check openers run before opening a pane, so the refusal is a
// notice naming the limit instead of an empty diff.
func TooLarge(left, right string) bool {
	return len(left) > MaxDiffBytes || len(right) > MaxDiffBytes
}

// lineKey is the comparison key of one line: the line itself, or — ignoring
// whitespace — the line with every whitespace rune removed, so indentation,
// alignment padding and re-wrapped spacing compare equal.
func lineKey(line string, opts Options) string {
	if !opts.IgnoreWhitespace || !strings.ContainsFunc(line, unicode.IsSpace) {
		return line
	}
	var b strings.Builder
	b.Grow(len(line))
	for _, r := range line {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// lineKeys maps a whole side onto its comparison keys.
func lineKeys(lines []string, opts Options) []string {
	if !opts.IgnoreWhitespace {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = lineKey(l, opts)
	}
	return out
}

// splitLines splits text on '\n', treating the empty text as zero lines so an
// empty side diffs as pure inserts/deletes instead of one phantom empty line.
// A trailing newline is a line terminator, not a separator (#507): without
// dropping the final empty element, a HEAD blob ("a\n") against an editor
// buffer ("a") rendered a phantom removed empty row. Trailing-newline-only
// differences are therefore invisible to the viewer, by design.
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
	out = append(out, text[start:])
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// pairEdit is one entry of the aligned edit script: an equal pair carries the
// raw text of *both* sides (they may differ in whitespace when the comparison
// ignores it), a delete only the left, an insert only the right.
type pairEdit struct {
	op          Op
	left, right string
}

// buildRows folds the edit script into aligned display rows: runs of deletes
// followed by inserts pair up positionally into changed rows (with intra-line
// spans); the unpaired remainder stays one-sided.
func buildRows(edits []pairEdit, opts Options) []Row {
	var rows []Row
	leftNo, rightNo := 0, 0
	i := 0
	for i < len(edits) {
		switch edits[i].op {
		case OpEqual:
			leftNo++
			rightNo++
			rows = append(rows, Row{Kind: RowSame, LeftNo: leftNo, RightNo: rightNo, Left: edits[i].left, Right: edits[i].right})
			i++
		default:
			// Collect the maximal delete run then insert run.
			var dels, ins []string
			for i < len(edits) && edits[i].op == OpDelete {
				dels = append(dels, edits[i].left)
				i++
			}
			for i < len(edits) && edits[i].op == OpInsert {
				ins = append(ins, edits[i].right)
				i++
			}
			pairs := min(len(dels), len(ins))
			for p := 0; p < pairs; p++ {
				leftNo++
				rightNo++
				ls, rs := refineWith(dels[p], ins[p], opts)
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

// Refine runs a rune-level diff over a changed line pair and returns the
// changed spans on each side — refine exported for consumers outside the
// row model (#1630): the unified-diff language pairs adjacent -/+ lines and
// emphasizes their changed ranges with the same algorithm the diff views use.
func Refine(left, right string) (ls, rs []Span) { return refine(left, right) }

// refineWith refines a changed pair under opts: ignoring whitespace, spans
// that carry no non-whitespace change drop out and the remaining ones shrink
// to their non-whitespace core, so a re-indented line whose *content* changed
// emphasizes the content and not the leading run.
func refineWith(left, right string, opts Options) (ls, rs []Span) {
	ls, rs = refine(left, right)
	if !opts.IgnoreWhitespace {
		return ls, rs
	}
	return trimSpaceSpans(left, ls), trimSpaceSpans(right, rs)
}

// trimSpaceSpans trims each span's leading and trailing whitespace runes and
// drops the spans left empty (whitespace-only changes).
func trimSpaceSpans(line string, spans []Span) []Span {
	if len(spans) == 0 {
		return nil
	}
	runes := []rune(line)
	var out []Span
	for _, s := range spans {
		start := clamp(s.Start, 0, len(runes))
		end := clamp(s.End, start, len(runes))
		for start < end && unicode.IsSpace(runes[start]) {
			start++
		}
		for end > start && unicode.IsSpace(runes[end-1]) {
			end--
		}
		if start < end {
			out = append(out, Span{Start: start, End: end})
		}
	}
	return out
}

// refine runs a rune-level diff over a changed line pair and returns the
// changed spans on each side. Oversized lines skip refinement (whole-line
// emphasis reads better than quadratic work).
func refine(left, right string) (ls, rs []Span) {
	if len(left) > maxRefineBytes || len(right) > maxRefineBytes {
		// Over 4 KiB the line is over maxRefineRunes for sure (#2505) — skip
		// before the []rune conversion would allocate a rune per byte of it.
		return nil, nil
	}
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

// script computes the line-level edit script (whitespace significant), the
// shape Lines exposes.
func script(a, b []string) []Edit {
	pairs := pairScript(a, b, Options{})
	edits := make([]Edit, 0, len(pairs))
	for _, p := range pairs {
		text := p.left
		if p.op == OpInsert {
			text = p.right
		}
		edits = append(edits, Edit{Op: p.op, Text: text})
	}
	return edits
}

// pairScript computes the line-level edit script via Myers, comparing lines
// by their opts key (whole line, or whitespace-stripped) while every entry
// carries the raw text of the side(s) it consumes.
func pairScript(a, b []string, opts Options) []pairEdit {
	ka, kb := lineKeys(a, opts), lineKeys(b, opts)
	// Trim the common prefix and suffix — typical edits touch a small region,
	// and Myers cost grows with the differing middle.
	pre := 0
	for pre < len(a) && pre < len(b) && ka[pre] == kb[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && ka[len(a)-1-suf] == kb[len(b)-1-suf] {
		suf++
	}
	ops := myersTrace(stringSeq{ka[pre : len(ka)-suf]}, stringSeq{kb[pre : len(kb)-suf]})
	out := make([]pairEdit, 0, pre+len(ops)+suf)
	for i := 0; i < pre; i++ {
		out = append(out, pairEdit{op: OpEqual, left: a[i], right: b[i]})
	}
	ai, bi := pre, pre
	for _, op := range ops {
		switch op {
		case OpEqual:
			out = append(out, pairEdit{op: OpEqual, left: a[ai], right: b[bi]})
			ai++
			bi++
		case OpDelete:
			out = append(out, pairEdit{op: OpDelete, left: a[ai]})
			ai++
		case OpInsert:
			out = append(out, pairEdit{op: OpInsert, right: b[bi]})
			bi++
		}
	}
	for i := 0; i < suf; i++ {
		out = append(out, pairEdit{op: OpEqual, left: a[len(a)-suf+i], right: b[len(b)-suf+i]})
	}
	return out
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
// backtrack; each snapshot holds only the round's reachable band -d..d, not
// the full diagonal range (#2505) — the full-width copies made the memory
// O(D·(N+M)), gigabytes for two divergent multi-thousand-line sides. Rounds
// past maxMyersRounds abandon the optimal alignment for a plain replace-all
// script, keeping the worst case bounded.
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
	// snapshots[d][k+d] is round d's furthest x on diagonal k, |k| <= d.
	var snapshots [][]int
	var dFound = -1
outer:
	for d := 0; d <= max; d++ {
		if d > maxMyersRounds {
			// Too divergent for the budget (#2505): a delete-all/insert-all
			// script over the trimmed middle, which buildRows pairs into
			// changed rows positionally — coarser, but bounded.
			return append(repeatOp(OpDelete, n), repeatOp(OpInsert, m)...)
		}
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
				snapshots = append(snapshots, bandSnapshot(v, d, max))
				dFound = d
				break outer
			}
		}
		snapshots = append(snapshots, bandSnapshot(v, d, max))
	}
	// Backtrack from (n, m) through the D-round snapshots.
	var rev []Op
	x, y := n, m
	for d := dFound; d > 0; d-- {
		vPrev := snapshots[d-1]
		off := d - 1 // vPrev[k+off] is round d-1's x on diagonal k
		k := x - y
		var prevK int
		if k == -d || (k != d && vPrev[k-1+off] < vPrev[k+1+off]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vPrev[prevK+off]
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

// bandSnapshot copies round d's reachable diagonals -d..d out of the
// full-width v (indexed k+max) into a 2d+1 slice (indexed k+d) — the only
// part of v the backtrack ever reads, and the difference between O(D²) and
// O(D·(N+M)) total snapshot memory (#2505).
func bandSnapshot(v []int, d, max int) []int {
	snap := make([]int, 2*d+1)
	copy(snap, v[max-d:max+d+1])
	return snap
}

func repeatOp(op Op, n int) []Op {
	out := make([]Op, n)
	for i := range out {
		out[i] = op
	}
	return out
}
