package ui

// matchstep.go is the shared half of the match-step chord (#2410): cmd+g and
// cmd+shift+g step through the matches of whatever search or filter the
// focused pane has open, without the input losing focus and without the query
// being applied first. Every pane phrases "one step" differently — a scroll,
// a tree cursor, a grid row — so the panes keep their own stepping code and
// only agree on this outcome type and on how a wrap is spelled.
//
// The counter helpers live here for the same reason the chord helpers live in
// findkey.go: the search line of eight panes was already rendering "3/17" by
// hand, and the wrap marker has to read identically in all of them or it
// reads as a bug in the one that differs.

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// MatchStep reports what one NextMatch / PrevMatch call did.
//
// Handled is the gate the root model reads: false means "this pane has no
// search open", and the chord keeps its older meaning (repeat the editor's
// in-file search, walk the retained find-in-path results). Handled with
// Total 0 is the deliberate no-op the acceptance criteria ask for — the pane
// owns the chord, there is simply nothing to step to, and the caller says so
// with a short hint instead of moving something unrelated.
type MatchStep struct {
	Handled bool
	Total   int  // matches the current query has
	Index   int  // 1-based position within them, 0 when Total is 0
	Wrapped bool // the step ran off an end and came back around

	// Cmd carries whatever the step has to emit — the DOM inspector jumps
	// the editor to the match it landed on. The caller batches it; a pane
	// with nothing to emit leaves it nil.
	Cmd tea.Cmd
}

// NoStep is the "not my chord" outcome: the pane has no search open.
var NoStep = MatchStep{}

// NoMatches is the handled no-op: the search is open, the query matches
// nothing.
func NoMatches() MatchStep { return MatchStep{Handled: true} }

// Stepped is the moved outcome. index is 0-based, as the panes hold it.
func Stepped(index, total int, wrapped bool) MatchStep {
	if total <= 0 {
		return NoMatches()
	}
	return MatchStep{Handled: true, Total: total, Index: index + 1, Wrapped: wrapped}
}

// StepWrap advances a 0-based cursor over n items by delta, wrapping at both
// ends and reporting the wrap — the one thing listnav's StepIndex does not
// say, and the thing the search line has to show (#2410). cur may be -1
// ("nothing current yet"), which steps to the first item going forward and
// the last going backward without counting as a wrap.
func StepWrap(cur, n, delta int) (next int, wrapped bool) {
	if n <= 0 {
		return -1, false
	}
	if cur < 0 || cur >= n {
		if delta < 0 {
			return n - 1, false
		}
		return 0, false
	}
	raw := cur + delta
	wrapped = raw < 0 || raw >= n
	return ((raw % n) + n) % n, wrapped
}

// StepOver walks cur over the subset of [0,n) that keep reports — the shape
// the list panes step in, where the row slice interleaves results with group
// headers and only the results count as matches (#2410). A cur sitting on a
// header (or past the end) is treated as the nearest result at or after it, so
// the first step lands where the eye already is. It returns the new row index
// and the outcome the search line reports.
func StepOver(cur, n, delta int, keep func(i int) bool) (next int, st MatchStep) {
	var hits []int
	for i := 0; i < n; i++ {
		if keep(i) {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		return cur, NoMatches()
	}
	at := len(hits) - 1
	for i, idx := range hits {
		if idx >= cur {
			at = i
			break
		}
	}
	pick, wrapped := StepWrap(at, len(hits), delta)
	return hits[pick], Stepped(pick, len(hits), wrapped)
}

// StepSorted advances over an ascending list of positions relative to pos —
// the shape the explorer, the terminal scrollback and copy mode all step in,
// where "where I am" is a row or line number rather than an index into the
// match list. It returns the chosen position, its 0-based index in list, and
// whether the step wrapped. ok is false for an empty list.
func StepSorted(list []int, pos, delta int) (val, idx int, wrapped, ok bool) {
	if len(list) == 0 {
		return 0, -1, false, false
	}
	if delta >= 0 {
		for i, v := range list {
			if v > pos {
				return v, i, false, true
			}
		}
		return list[0], 0, true, true
	}
	for i := len(list) - 1; i >= 0; i-- {
		if list[i] < pos {
			return list[i], i, false, true
		}
	}
	return list[len(list)-1], len(list) - 1, true, true
}

// MatchCounter renders the counter every pane's search line carries: "3/17",
// or "1/12 (wrapped)" for the step that came back around (#2410). index is
// 1-based; total 0 renders "no matches" — callers that colour that case
// differently check total themselves and skip this helper.
func MatchCounter(index, total int, wrapped bool) string {
	if total <= 0 {
		return "no matches"
	}
	s := strconv.Itoa(index) + "/" + strconv.Itoa(total)
	if wrapped {
		s += " (wrapped)"
	}
	return s
}
