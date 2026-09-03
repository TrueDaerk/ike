package editor

import (
	"math/rand"
	"sort"
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/mode"
)

// lineset.go holds the line-set commands (#2417): sort in its four flavours,
// unique, reverse, shuffle and sort-by-length. They are the palette/keybind
// half of the ":sort" family in sort.go — where the ex-command takes a range,
// these take the selection.
//
// Every one of them reorders (or thins) a contiguous block of lines: the lines
// the visual selection touches, or the whole buffer when there is no selection,
// which is what JetBrains' "Sort Lines" does too. The rewrite is a single Edit
// over the block, so one "u" reverts it, and the result is left selected
// linewise so a second command chains onto the same block.

// lineSetOp names the reordering a line-set command applies to its block.
type lineSetOp int

const (
	opSort           lineSetOp = iota // byte order, ascending
	opSortIgnoreCase                  // byte order over the lowercased line
	opSortNatural                     // digit runs compare numerically: a2 < a10
	opSortDescending                  // byte order, descending
	opSortByLength                    // by rune length, shortest first
	opUnique                          // drop repeats, keep the first occurrence
	opReverse                         // flip the block end to end
	opShuffle                         // random permutation
)

// shuffleFunc is math/rand's Shuffle, indirected so a test can pin the
// permutation instead of chasing a random one.
var shuffleFunc = rand.Shuffle

// lineSetBlock returns the line range a line-set command acts on: the lines the
// visual selection touches, or the whole buffer when nothing is selected.
func (m Model) lineSetBlock() (first, last int) {
	if m.mode.IsVisual() {
		return m.lineSpan()
	}
	return 0, m.buf.LineCount() - 1
}

// runLineSet applies op to the command's block and leaves the result selected.
// A no-op reordering changes nothing and records no undo step; the selection is
// still restored, so the block stays visible either way. Multi-caret collapses
// first — only the primary selection's line range takes part (#2417).
func (m *Model) runLineSet(op lineSetOp) {
	if m.insert.active {
		m.commitInsert()
	}
	m.collapseCarets()
	first, last := m.lineSetBlock()
	if last < first {
		return
	}

	lines := make([]string, 0, last-first+1)
	for i := first; i <= last; i++ {
		lines = append(lines, m.buf.Line(i))
	}
	out := applyLineSet(lines, op)

	if !equalLines(lines, out) {
		m.mutate(func(rec *history.Recorder) buffer.Position {
			rec.Apply(buffer.Edit{
				Range: buffer.Range{
					Start: buffer.Position{Line: first, Col: 0},
					End:   buffer.Position{Line: last, Col: m.buf.RuneLen(last)},
				},
				Text: strings.Join(out, "\n"),
			})
			return buffer.Position{Line: first, Col: 0}
		})
	}
	m.selectLineBlock(first, first+len(out)-1)
	m.scroll()
}

// selectLineBlock leaves a linewise visual selection covering [first, last],
// the block a line-set command just rewrote, so the next command in the family
// operates on the same lines.
func (m *Model) selectLineBlock(first, last int) {
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: first, Col: 0})
	m.enterVisual(mode.VisualLine)
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: last, Col: 0})
	m.desiredCol = m.cursor.Col
	m.emit(EventCursorMove)
}

// applyLineSet returns the reordered copy of lines. Every sorting flavour is
// stable, so lines that compare equal keep their original order.
func applyLineSet(lines []string, op lineSetOp) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	switch op {
	case opSort:
		sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	case opSortIgnoreCase:
		// Ties under the fold ("A" vs "a") fall back to the raw line, so the
		// order is a function of the text and not of where the lines started.
		sort.SliceStable(out, func(i, j int) bool {
			a, b := strings.ToLower(out[i]), strings.ToLower(out[j])
			if a != b {
				return a < b
			}
			return out[i] < out[j]
		})
	case opSortNatural:
		sort.SliceStable(out, func(i, j int) bool { return naturalLess(out[i], out[j]) })
	case opSortDescending:
		sort.SliceStable(out, func(i, j int) bool { return out[i] > out[j] })
	case opSortByLength:
		sort.SliceStable(out, func(i, j int) bool {
			a, b := len([]rune(out[i])), len([]rune(out[j]))
			if a != b {
				return a < b
			}
			return out[i] < out[j]
		})
	case opUnique:
		out = uniqueLines(out)
	case opReverse:
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	case opShuffle:
		shuffleFunc(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	}
	return out
}

// uniqueLines drops every repeat of a line already seen, keeping the first
// occurrence and the surviving lines' original order — unlike ":sort u", which
// only collapses neighbours because sorting has already grouped them.
func uniqueLines(lines []string) []string {
	seen := make(map[string]struct{}, len(lines))
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if _, dup := seen[l]; dup {
			continue
		}
		seen[l] = struct{}{}
		out = append(out, l)
	}
	return out
}

// naturalLess is the "natural" (human) order: the strings are walked in
// parallel and a digit run on both sides compares as a number, so "file2" sorts
// before "file10". Leading zeros only break a tie between otherwise equal
// numbers ("a01" after "a1"), keeping the order total.
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if asciiDigit(ca) && asciiDigit(cb) {
			na, ia := digitRun(a, i)
			nb, jb := digitRun(b, j)
			if na != nb {
				return na < nb
			}
			// Equal values, different spellings ("1" vs "01"): the shorter
			// (fewer leading zeros) run sorts first.
			if ia-i != jb-j {
				return ia-i < jb-j
			}
			i, j = ia, jb
			continue
		}
		if ca != cb {
			return ca < cb
		}
		i, j = i+1, j+1
	}
	return len(a)-i < len(b)-j
}

// digitRun reads the decimal run starting at i and returns its value together
// with the index just past it. The value saturates instead of wrapping, so an
// absurdly long digit run still orders after every shorter number.
func digitRun(s string, i int) (int64, int) {
	var n int64
	for ; i < len(s) && asciiDigit(s[i]); i++ {
		if n > (1<<62)/10 {
			n = 1 << 62
			continue
		}
		n = n*10 + int64(s[i]-'0')
	}
	return n, i
}

// asciiDigit reports whether b is an ASCII decimal digit. It is the byte twin
// of increment.go's rune-taking isDigit, kept separate so naturalLess' parallel
// walk stays on bytes.
func asciiDigit(b byte) bool { return b >= '0' && b <= '9' }
