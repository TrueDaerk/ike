package ui

// linesearch.go is the shared in-pane "/" search (#2461): the state and the
// behaviour of a one-line incremental search that jumps a viewer to its
// matches. Before it, seven panes — the diff and markdown viewers, the HTTP
// response pane, the notebook and hex viewers, the terminal's scrollback
// search and the explorer's speed search — each carried their own
// query/caret/open/matches/cur/wrapped tuple and their own copy of "keep the
// current match on the nearest surviving row" and "an edited query starts a
// fresh walk" (#2410). Same shape seven times, and seven places for the rules
// to drift: two of them were not live, two had no caret, one rendered its
// counter by hand.
//
// The type owns what every pane agrees on:
//
//   - the query is a Field (#2459), so it edits like every other one-line
//     input — caret, word motions, kills, paste;
//   - Open says whether the prompt holds the keyboard; the query and its
//     matches survive enter, so n / N and cmd+g keep stepping them;
//   - Matches are the *positions* the query hits, ascending — rows, lines,
//     cells, byte offsets, whatever the pane steps in — and Cur indexes them;
//   - typing narrows live: SetMatches keeps the current match on the nearest
//     surviving position and drops the wrap marker;
//   - the prompt row reads the same everywhere: the slash, the query with its
//     caret, and the "3/17" / "1/12 (wrapped)" / "no matches" counter.
//
// The panes keep what differs: how a match is found (a smartcase substring for
// most, a compiled pattern for the HTTP pane, a byte sequence for the hex
// viewer), how the view moves to it, and whether esc restores a saved anchor.
// A pane whose matches carry more than a position (a span, a cell/line pair)
// holds that detail in a slice aligned with Matches.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SearchAction is what a key did to the prompt beyond editing it.
type SearchAction int

const (
	// SearchNone: the key edited the query (or was not the search's to take).
	SearchNone SearchAction = iota
	// SearchAccept: enter closed the prompt; the query and its matches stay
	// for n / N and the match-step chord.
	SearchAccept
	// SearchCancel: esc closed the prompt and dropped the search. A pane with
	// a saved anchor restores it on this action.
	SearchCancel
)

// LineSearch is one pane's in-pane search. The zero value is a closed search
// with no query; Cur is only meaningful while it indexes Matches.
//
// The caret is Field.Cur; Cur here is the current match. The two are
// deliberately named alike — every pane called both "cur" — and a host only
// ever touches the caret through Field.
type LineSearch struct {
	Field
	Open    bool  // the prompt holds the keyboard
	Matches []int // positions the query matches, ascending
	Cur     int   // index into Matches, -1 (or out of range) with none
	Wrapped bool  // the last step came back around an end (#2410)
}

// Active reports whether a search exists at all — the prompt is open or a
// query is applied — which is when the match-step chord is the pane's.
func (s *LineSearch) Active() bool { return s.Open || s.Text != "" }

// Miss reports whether a non-empty query matches nothing.
func (s *LineSearch) Miss() bool { return s.Text != "" && len(s.Matches) == 0 }

// Start opens the prompt with the caret at the end of the last query, so
// refining a search is an edit rather than a retype.
func (s *LineSearch) Start() {
	s.Open = true
	s.Field.Cur = s.Len()
}

// Close leaves the prompt, keeping the query and its matches.
func (s *LineSearch) Close() { s.Open = false }

// Reset drops the search entirely: prompt closed, no query, no matches.
func (s *LineSearch) Reset() { *s = LineSearch{Cur: -1} }

// Key feeds one key to the open prompt. enter and esc are reported as
// actions (enter closes and keeps, esc resets); everything else edits through
// Field, and changed says the query differs — the signal to recompute the
// matches. A changed query also drops the wrap marker, which describes one
// step rather than the search (#2410).
//
// Caller chords win: a pane with its own bindings on the prompt (the
// terminal's ctrl+n / ctrl+p, a copy chord) runs them first and hands Key what
// is left.
func (s *LineSearch) Key(msg tea.KeyPressMsg) (handled, changed bool, action SearchAction) {
	switch msg.Code {
	case tea.KeyEscape:
		s.Reset()
		return true, false, SearchCancel
	case tea.KeyEnter:
		s.Close()
		return true, false, SearchAccept
	}
	handled, changed = s.Field.Key(msg)
	if changed {
		s.Wrapped = false
	}
	return handled, changed, SearchNone
}

// Paste inserts a pasted block into the query (the field's paste, #1273) and
// reports whether it changed — a changed query is recomputed like a typed one.
func (s *LineSearch) Paste(text string) (changed bool) {
	if changed = s.Field.Paste(text); changed {
		s.Wrapped = false
	}
	return changed
}

// SetMatches replaces the match list. The current match stays on the nearest
// surviving position at or after the one it was on — so incremental typing
// does not throw the view around — falling back to the first match; the wrap
// marker is dropped because an edited query starts a fresh walk (#2410).
func (s *LineSearch) SetMatches(matches []int) {
	prev := -1
	if p, ok := s.Current(); ok {
		prev = p
	}
	s.Matches = matches
	s.Wrapped = false
	s.Cur = -1
	for i, p := range matches {
		if p >= prev {
			s.Cur = i
			break
		}
	}
	if s.Cur < 0 && len(matches) > 0 {
		s.Cur = 0
	}
}

// Recompute scans n candidates, keeps those hit reports and installs them
// through SetMatches. An empty query matches nothing, so hit never runs for
// it.
func (s *LineSearch) Recompute(n int, hit func(i int) bool) {
	var out []int
	if s.Text != "" {
		for i := 0; i < n; i++ {
			if hit(i) {
				out = append(out, i)
			}
		}
	}
	s.SetMatches(out)
}

// Apply picks the match enter lands on: the first at or after pos (the top of
// the view, the cursor row), wrapping to the first match when pos sits past
// the last. It reports whether there is a match to land on.
func (s *LineSearch) Apply(pos int) bool {
	if len(s.Matches) == 0 {
		s.Cur = -1
		return false
	}
	s.Cur = 0
	for i, p := range s.Matches {
		if p >= pos {
			s.Cur = i
			break
		}
	}
	return true
}

// Step moves the current match by delta, wrapping at both ends, and reports
// where it landed for the match-step chord (#2410). From "no current match"
// it steps to the first going forward and the last going backward.
func (s *LineSearch) Step(delta int) MatchStep {
	if len(s.Matches) == 0 {
		s.Wrapped = false
		return NoMatches()
	}
	s.Cur, s.Wrapped = StepWrap(s.Cur, len(s.Matches), delta)
	return Stepped(s.Cur, len(s.Matches), s.Wrapped)
}

// StepFrom steps relative to a position rather than the current index — the
// shape the explorer and the terminal step in, where "where I am" is a row or
// a line the user may have moved since the last step. It returns the position
// stepped to and the outcome; Cur follows.
func (s *LineSearch) StepFrom(pos, delta int) (val int, st MatchStep) {
	val, idx, wrapped, ok := StepSorted(s.Matches, pos, delta)
	if !ok {
		s.Cur, s.Wrapped = -1, false
		return 0, NoMatches()
	}
	s.Cur, s.Wrapped = idx, wrapped
	return val, Stepped(idx, len(s.Matches), wrapped)
}

// Locate points Cur at the match sitting on pos, or at none when pos is no
// match — for the panes whose cursor moves on its own between steps, so the
// counter reads "3/17" when the cursor is on the third match and "0/17" when
// it has wandered off.
func (s *LineSearch) Locate(pos int) bool {
	for i, p := range s.Matches {
		if p == pos {
			s.Cur = i
			return true
		}
		if p > pos {
			break
		}
	}
	s.Cur = -1
	return false
}

// Current returns the current match's position.
func (s *LineSearch) Current() (pos int, ok bool) {
	if s.Cur < 0 || s.Cur >= len(s.Matches) {
		return 0, false
	}
	return s.Matches[s.Cur], true
}

// Stat is where the search stands without moving it — what a step reports
// when the pane jumped rather than stepped.
func (s *LineSearch) Stat() MatchStep {
	if len(s.Matches) == 0 {
		return NoMatches()
	}
	idx := s.Cur
	if idx < 0 || idx >= len(s.Matches) {
		idx = 0
	}
	return Stepped(idx, len(s.Matches), s.Wrapped)
}

// Counter renders the counter every prompt row carries: "3/17", "1/12
// (wrapped)" or "no matches"; "" while the query is empty, when there is
// nothing to count.
func (s *LineSearch) Counter() string {
	if s.Text == "" {
		return ""
	}
	if len(s.Matches) == 0 {
		return "no matches"
	}
	return MatchCounter(s.Cur+1, len(s.Matches), s.Wrapped)
}

// Line renders the prompt row: the slash prefix, the query with its caret
// while the prompt is open (plain once enter applied it), and the counter two
// cells on, faint. The pane prepends its own margin and truncates to its
// width.
func (s *LineSearch) Line() string {
	dim := lipgloss.NewStyle().Faint(true)
	return s.LineStyled(dim, dim)
}

// LineStyled is Line with the pane's own colours: dim for the counter, miss
// for "no matches" — the terminal and the explorer paint the miss in the
// theme's Error colour.
func (s *LineSearch) LineStyled(dim, miss lipgloss.Style) string {
	line := "/"
	if s.Open {
		line += s.Field.View()
	} else {
		line += s.Text
	}
	switch c := s.Counter(); {
	case c == "":
	case len(s.Matches) == 0:
		line += miss.Render("  " + c)
	default:
		line += dim.Render("  " + c)
	}
	return line
}

// SmartCaseContains is the in-pane search's one matching rule, the editor's
// "/" smartcase (#257): an all-lowercase pattern folds case, any uppercase
// rune makes it exact. An empty pattern matches nothing — an empty query is
// no search.
func SmartCaseContains(pattern, text string) bool {
	if pattern == "" {
		return false
	}
	p, t := SmartCaseFold(pattern, text)
	return strings.Contains(t, p)
}

// SmartCaseFold returns the pair SmartCaseContains compares — text lowercased
// under an all-lowercase pattern, both untouched otherwise — for a highlighter
// that needs the match *offsets* and not just the verdict.
func SmartCaseFold(pattern, text string) (p, t string) {
	if pattern == strings.ToLower(pattern) {
		return pattern, strings.ToLower(text)
	}
	return pattern, text
}
