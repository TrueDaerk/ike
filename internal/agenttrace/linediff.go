package agenttrace

import "strings"

// linediff.go is the small line diff the reconstruction (diffs.go) needs
// where the transcript recorded no patch: an old/new string applied to the
// content read earlier, a Write against the session's earlier content. The
// shared engine (internal/diff) lives in a package with a UI half, which this
// pure package does not import; the hunks here only feed the +N −M counts
// and the fragment view, the app re-diffs whole contents with the engine.

// diffContext is the unchanged lines a hunk carries on each side.
const diffContext = 3

// maxDiffRounds bounds the Myers search like the engine's cap: two sides
// divergent beyond it diff as delete-all / insert-all of the middle.
const maxDiffRounds = 1024

type lineOp byte

const (
	opEqual lineOp = iota
	opDelete
	opInsert
)

// splitLines splits text into lines; a trailing newline ends the last line
// instead of opening an empty one.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// lineScript is the edit script turning a into b: one op per line of a
// (equal / delete) and per line of b (insert), in order.
func lineScript(a, b []string) []lineOp {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	out := make([]lineOp, 0, len(a)+len(b))
	for i := 0; i < pre; i++ {
		out = append(out, opEqual)
	}
	out = append(out, myers(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for i := 0; i < suf; i++ {
		out = append(out, opEqual)
	}
	return out
}

// myers is the greedy O(ND) script of the trimmed middle.
func myers(a, b []string) []lineOp {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		out := make([]lineOp, 0, n+m)
		for i := 0; i < n; i++ {
			out = append(out, opDelete)
		}
		for i := 0; i < m; i++ {
			out = append(out, opInsert)
		}
		return out
	}
	max := n + m
	off := max
	v := make([]int, 2*max+2)
	var trace [][]int
	found := false
	for d := 0; d <= max && d <= maxDiffRounds; d++ {
		// Each round keeps the window of v it can read back: k in [-d, d].
		trace = append(trace, append([]int(nil), v[off-d:off+d+1]...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		// Too divergent: delete-all / insert-all, like the engine's cap.
		return append(myers(a, nil), myers(nil, b)...)
	}
	// Walk the snapshots back from (n, m).
	var rev []lineOp
	x, y := n, m
	for d := len(trace) - 1; d > 0; d-- {
		vv := trace[d]
		k := x - y
		var pk int
		if k == -d || (k != d && vv[d+k-1] < vv[d+k+1]) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := vv[d+pk]
		py := px - pk
		for x > px && y > py {
			rev = append(rev, opEqual)
			x--
			y--
		}
		if x == px {
			rev = append(rev, opInsert)
		} else {
			rev = append(rev, opDelete)
		}
		x, y = px, py
	}
	for x > 0 && y > 0 {
		rev = append(rev, opEqual)
		x--
		y--
	}
	out := make([]lineOp, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out
}

// lineHunks groups the diff of a → b into unified hunks with diffContext
// lines around each change; firstOld and firstNew are the 1-based numbers of
// a's and b's first lines.
func lineHunks(a, b []string, firstOld, firstNew int) []DiffHunk {
	ops := lineScript(a, b)
	type row struct {
		op   lineOp
		text string
		ai   int // index into a (equal / delete), else the a index it precedes
		bi   int
	}
	rows := make([]row, 0, len(ops))
	ai, bi := 0, 0
	for _, op := range ops {
		switch op {
		case opEqual:
			rows = append(rows, row{op, a[ai], ai, bi})
			ai++
			bi++
		case opDelete:
			rows = append(rows, row{op, a[ai], ai, bi})
			ai++
		case opInsert:
			rows = append(rows, row{op, b[bi], ai, bi})
			bi++
		}
	}
	var out []DiffHunk
	for i := 0; i < len(rows); {
		if rows[i].op == opEqual {
			i++
			continue
		}
		start := max(0, i-diffContext)
		end := i
		// Extend over changes closer than two contexts apart.
		for j := i; j < len(rows); j++ {
			if rows[j].op != opEqual {
				end = j + 1
				continue
			}
			if j-end >= 2*diffContext {
				break
			}
		}
		stop := min(len(rows), end+diffContext)
		h := DiffHunk{OldStart: firstOld + rows[start].ai, NewStart: firstNew + rows[start].bi}
		for _, r := range rows[start:stop] {
			switch r.op {
			case opEqual:
				h.Lines = append(h.Lines, " "+r.text)
				h.OldLines++
				h.NewLines++
			case opDelete:
				h.Lines = append(h.Lines, "-"+r.text)
				h.OldLines++
			case opInsert:
				h.Lines = append(h.Lines, "+"+r.text)
				h.NewLines++
			}
		}
		out = append(out, h)
		i = stop
	}
	return out
}
