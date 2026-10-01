// Package diff is the reusable diff viewer (#60): a line-level Myers diff
// engine with intra-line refinement, plus a pane model rendering two text
// versions side by side or unified. It is shared infrastructure — VCS status
// (#28), local history (#35), and the external-change conflict guard (#53)
// open it; on its own it is reachable through the diff.files palette command.
//
// engine.go is the pure computation half: no rendering, no bubbletea. Lines
// computes the line-level edit script; Compute pairs delete/insert runs into
// changed line pairs, refines them at token level into per-side spans, and
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

// maxRefineRunes bounds intra-line refinement: the token-level Myers (#2849)
// is still quadratic in the worst case (a fully divergent pair of long lines
// runs the full bounded D loop), and emphasis inside very long lines is
// unreadable anyway. Tokens are far fewer than runes, so the cap sits well
// above the 400 runes the rune-level diff allowed.
const maxRefineRunes = 1000

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

// Refine runs a token-level diff over a changed line pair and returns the
// changed spans on each side — refine exported for consumers outside the
// row model (#1630): the unified-diff language pairs adjacent -/+ lines and
// emphasizes their changed ranges with the same algorithm the diff views use.
func Refine(left, right string) (ls, rs []Span) { return refine(left, right) }

// refine is refineTokens with whitespace significant.
func refine(left, right string) (ls, rs []Span) { return refineTokens(left, right, false) }

// refineWith refines a changed pair under opts: ignoring whitespace, the
// token diff treats any two whitespace runs as equal — so a re-indented line
// whose *content* changed emphasizes the content and not the leading run,
// and the re-indentation never pushes the pair into the whole-line fallback
// — and the spans still shrink to their non-whitespace core afterwards,
// dropping the ones left empty (a whitespace run the other side lacks).
func refineWith(left, right string, opts Options) (ls, rs []Span) {
	ls, rs = refineTokens(left, right, opts.IgnoreWhitespace)
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

// refineTokens runs a token-level diff over a changed line pair and returns
// the changed spans on each side (#2849): the lines are tokenized into
// identifier runs, whitespace runs and single symbols, the token edit script
// is mapped back to rune spans, equal gaps shorter than minEqualGapRunes
// between two changes are absorbed into the emphasis (a shared "." or ", "
// between two renamed identifiers is not worth a break in the emphasis), and
// a pair whose emphasis would cover more than refineFallbackPercent of both
// lines falls back to whole-line emphasis (nil spans) — a rewritten line
// reads better without confetti. Oversized lines skip refinement the same
// way. With wsEqual, whitespace runs compare equal whatever they hold.
func refineTokens(left, right string, wsEqual bool) (ls, rs []Span) {
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
	// Common prefix and suffix are trimmed at rune level first (the usual
	// edit touches a few tokens of a long line) and snapped back to token
	// boundaries, so only the changed middle is tokenized and diffed.
	pre, suf := commonTokenEnds(lr, rr)
	lt := tokenize(lr, pre, len(lr)-suf)
	rt := tokenize(rr, pre, len(rr)-suf)
	ops := myersTrace(tokenSeq{r: lr, t: lt, ws: wsEqual}, tokenSeq{r: rr, t: rt, ws: wsEqual})
	blocks := mergeShortGaps(refineBlocks(ops, lt, rt, pre, len(lr), len(rr)))
	for _, b := range blocks {
		if !b.changed {
			continue
		}
		if b.left.End > b.left.Start {
			ls = appendSpan(ls, b.left.Start, b.left.End)
		}
		if b.right.End > b.right.Start {
			rs = appendSpan(rs, b.right.Start, b.right.End)
		}
	}
	if emphasisDominates(ls, len(lr)) && emphasisDominates(rs, len(rr)) {
		return nil, nil
	}
	return ls, rs
}

// minEqualGapRunes is the shortest equal run between two changes that stays
// unemphasized (#2849): a gap of fewer runes — a lone symbol, a ", ", a
// single shared letter — merges into the surrounding emphasis.
const minEqualGapRunes = 3

// refineFallbackPercent is the share of a line's runes the token-level
// emphasis may cover before the pair falls back to whole-line emphasis
// (#2849); the fallback needs both sides over the limit, so a short line
// growing a long insertion still shows where the insertion sits.
const refineFallbackPercent = 60

// token is one refinement unit of a line (#2849): an identifier run
// (letters, digits, marks, underscore), a whitespace run, or a single
// symbol rune, as the rune range [start, end) of the line.
type token struct {
	start, end int
}

// tokenize splits the rune range [from, to) of a line into refinement
// tokens (positions stay absolute). Identifier and whitespace runs group;
// every other rune (punctuation, operators, emoji) is a token of its own, so
// "a.b" diffs as three tokens and a changed operator stays a one-rune edit.
// The range must start and end on token boundaries (commonTokenEnds).
func tokenize(r []rune, from, to int) []token {
	toks := make([]token, 0, (to-from)/2+1)
	for i := from; i < to; {
		start := i
		switch {
		case isWordRune(r[i]):
			for i < to && isWordRune(r[i]) {
				i++
			}
		case unicode.IsSpace(r[i]):
			for i < to && unicode.IsSpace(r[i]) {
				i++
			}
		default:
			i++
		}
		toks = append(toks, token{start: start, end: i})
	}
	return toks
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// joined reports whether runes i-1 and i of r belong to the same token —
// both word runes or both whitespace — so position i is not a token
// boundary.
func joined(r []rune, i int) bool {
	if i <= 0 || i >= len(r) {
		return false
	}
	return (isWordRune(r[i-1]) && isWordRune(r[i])) ||
		(unicode.IsSpace(r[i-1]) && unicode.IsSpace(r[i]))
}

// commonTokenEnds returns the lengths of the common rune prefix and suffix
// of a and b, each shortened to the nearest token boundary of *both* lines,
// so the middle [pre, len-suf) can be tokenized on its own without a token
// straddling the cut ("oldName" vs "newName" share "Name" at rune level but
// differ as one token).
func commonTokenEnds(a, b []rune) (pre, suf int) {
	n := min(len(a), len(b))
	for pre < n && a[pre] == b[pre] {
		pre++
	}
	for pre > 0 && (joined(a, pre) || joined(b, pre)) {
		pre--
	}
	for suf < n-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	for suf > 0 && (joined(a, len(a)-suf) || joined(b, len(b)-suf)) {
		suf--
	}
	return pre, suf
}

// tokenEq reports whether two tokens hold the same runes; with wsEqual two
// whitespace runs match whatever they hold (ignore-whitespace mode).
func tokenEq(lr []rune, a token, rr []rune, b token, wsEqual bool) bool {
	if wsEqual && unicode.IsSpace(lr[a.start]) && unicode.IsSpace(rr[b.start]) {
		return true
	}
	if a.end-a.start != b.end-b.start {
		return false
	}
	for k := 0; k < a.end-a.start; k++ {
		if lr[a.start+k] != rr[b.start+k] {
			return false
		}
	}
	return true
}

// refineBlock is one run of the token edit script folded to rune extents:
// an equal run or a change run (adjacent deletes and inserts), with the
// rune range it covers on each side — empty on a side the run does not
// touch (a pure insertion has an empty left extent at the insertion point).
type refineBlock struct {
	changed     bool
	left, right Span
}

// refineBlocks folds the per-token ops over the tokenized middle into
// alternating equal/changed blocks with rune extents, walking a token cursor
// per side; the untouched common prefix [0, pre) and the suffix up to
// lend/rend become the outer equal blocks.
func refineBlocks(ops []Op, lt, rt []token, pre, lend, rend int) []refineBlock {
	blocks := make([]refineBlock, 0, 4)
	if pre > 0 {
		blocks = append(blocks, refineBlock{left: Span{0, pre}, right: Span{0, pre}})
	}
	li, ri := 0, 0
	lpos := func() int {
		if li < len(lt) {
			return lt[li].start
		}
		if len(lt) > 0 {
			return lt[len(lt)-1].end
		}
		return pre
	}
	rpos := func() int {
		if ri < len(rt) {
			return rt[ri].start
		}
		if len(rt) > 0 {
			return rt[len(rt)-1].end
		}
		return pre
	}
	open := func(changed bool) *refineBlock {
		if n := len(blocks); n > 0 && blocks[n-1].changed == changed {
			return &blocks[n-1]
		}
		l, r := lpos(), rpos()
		blocks = append(blocks, refineBlock{changed: changed, left: Span{l, l}, right: Span{r, r}})
		return &blocks[len(blocks)-1]
	}
	for _, op := range ops {
		switch op {
		case OpEqual:
			b := open(false)
			b.left.End = lt[li].end
			b.right.End = rt[ri].end
			li++
			ri++
		case OpDelete:
			b := open(true)
			b.left.End = lt[li].end
			li++
		case OpInsert:
			b := open(true)
			b.right.End = rt[ri].end
			ri++
		}
	}
	if lpos() < lend || rpos() < rend {
		b := open(false)
		b.left.End = lend
		b.right.End = rend
	}
	return blocks
}

// mergeShortGaps absorbs every equal block shorter than minEqualGapRunes
// that sits between two changed blocks into one changed block spanning all
// three, on both sides — the diff-match-patch style semantic cleanup that
// keeps "foo.bar" → "baz.qux" one emphasized region instead of two.
// Leading and trailing equal runs are never gaps.
func mergeShortGaps(blocks []refineBlock) []refineBlock {
	out := blocks[:0]
	for _, b := range blocks {
		n := len(out)
		if b.changed && n >= 2 && !out[n-1].changed && out[n-2].changed &&
			out[n-1].left.End-out[n-1].left.Start < minEqualGapRunes {
			out[n-2].left.End = b.left.End
			out[n-2].right.End = b.right.End
			out = out[:n-1]
			continue
		}
		out = append(out, b)
	}
	return out
}

// emphasisDominates reports whether spans cover more than
// refineFallbackPercent of a line of n runes; an empty side counts as
// fully covered (there is nothing left unchanged to anchor the emphasis).
func emphasisDominates(spans []Span, n int) bool {
	if n == 0 {
		return true
	}
	covered := 0
	for _, s := range spans {
		covered += s.End - s.Start
	}
	return 100*covered > refineFallbackPercent*n
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

// seq abstracts the two element types (lines, tokens) the Myers core walks.
type seq interface {
	Len() int
	Eq(other seq, i, j int) bool
}

type stringSeq struct{ s []string }

func (q stringSeq) Len() int { return len(q.s) }
func (q stringSeq) Eq(other seq, i, j int) bool {
	return q.s[i] == other.(stringSeq).s[j]
}

// tokenSeq walks the refinement tokens of one line (#2849); two tokens are
// equal when they hold the same runes (or are both whitespace, with ws).
type tokenSeq struct {
	r  []rune
	t  []token
	ws bool
}

func (q tokenSeq) Len() int { return len(q.t) }
func (q tokenSeq) Eq(other seq, i, j int) bool {
	o := other.(tokenSeq)
	return tokenEq(q.r, q.t[i], o.r, o.t[j], q.ws)
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
