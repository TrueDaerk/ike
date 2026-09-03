package ui

// speedsearch.go is the shared type-ahead ("speed search") layer for modal
// pickers (#2111). JetBrains popups all narrow as you type; IKE's overlays
// only arrow-walked, so a repository with forty labels meant walking a modal
// row by row. SpeedSearch gives any picker that renders a list of row labels
// the same behaviour in one place:
//
//   - printable keys go into the query at its caret and narrow the visible
//     rows live,
//   - the narrowing keeps the picker's own row order (the query filters, it
//     does not re-rank) so a row never jumps around under the cursor,
//   - backspace deletes the rune before the caret, but only while a query is
//     running — with an empty query it falls through to whatever the picker
//     assigned it (the mutation pickers clear their selection with it),
//   - space is never consumed, because the pickers toggle the row under the
//     cursor with it,
//   - esc is left to the caller, which clears the query first and only closes
//     the modal on the second press (EscClears).
//
// A running query is a one-line text input, so it edits like one (#2360): it
// carries a caret and every editing key runs through EditKey, which is what
// gives it left/right, the word motions (alt+left/ctrl+left,
// alt+right/ctrl+right), word deletion (alt+backspace/ctrl+w) and
// super+backspace. Before #2360 the query was append-only and rejected every
// modifier chord, so a user deleting a word inside a picker's type-ahead had
// the chord fall through to the keymap and logged as unbound.
//
// The type is a value: a picker embeds one, resets it when it opens, and asks
// Filter for the rows it should render.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// SpeedSearch is one picker's type-ahead query. The zero value is an idle
// search that matches everything.
type SpeedSearch struct {
	field Field // the query text and its caret (#2459)
}

// Query returns the typed text, "" when the search is idle.
func (s *SpeedSearch) Query() string { return s.field.Text }

// Cursor returns the caret's rune index into the query (#2360), for a host
// that renders the query itself instead of through Hint.
func (s *SpeedSearch) Cursor() int { return s.field.Cur }

// Active reports whether a query is narrowing the rows.
func (s *SpeedSearch) Active() bool { return !s.field.Empty() }

// Reset drops the query. Pickers call it when they open, so a modal never
// inherits the previous one's type-ahead.
func (s *SpeedSearch) Reset() { s.field.Clear() }

// EscClears is esc's first job: it drops a running query and reports true, so
// the caller closes the modal only on the second press.
func (s *SpeedSearch) EscClears() bool {
	if s.field.Empty() {
		return false
	}
	s.Reset()
	return true
}

// ssReserved reports the keys a running query leaves to its host even though
// EditKey would take them (#2360). space toggles the row under the cursor;
// plain delete is the pickers' "clear this row / this selection"; home/end
// and their cmd+arrow aliases are the list extremes every host routes through
// ListNav — a type-ahead must not swallow the way out of a long list.
//
// Matched on Code + Mod like EditKey itself (#2459), so the Command key is
// reserved in every spelling a terminal reports it in (super+, meta+) rather
// than only in the one msg.String() happened to produce.
func ssReserved(msg tea.KeyPressMsg) bool {
	mod := msg.Mod &^ tea.ModShift
	isCmd := mod == tea.ModSuper || mod == tea.ModMeta
	switch {
	case msg.Code == tea.KeySpace && msg.Mod == 0:
		return true
	case msg.Code == tea.KeyDelete && msg.Mod == 0:
		return true
	case msg.Code == tea.KeyHome && msg.Mod == 0, msg.Code == tea.KeyEnd && msg.Mod == 0:
		return true
	case isCmd && (msg.Code == tea.KeyLeft || msg.Code == tea.KeyRight):
		return true
	}
	return false
}

// Key feeds one key to the search and reports whether it took the key and
// whether the query changed. A caller that sees changed must re-clamp its
// cursor — the visible row set just moved.
//
// An idle search only starts on a printable rune: with no query running every
// other key — backspace, the arrows, the chords — belongs to the picker,
// which is what keeps "backspace clears the selection" working. A running
// query is a text input and edits through EditKey, minus the ssReserved keys
// the host binds to list actions.
func (s *SpeedSearch) Key(msg tea.KeyPressMsg) (handled, changed bool) {
	if s.field.Empty() {
		if !ssTypable(msg) {
			return false, false
		}
		s.field.Set(msg.Text)
		return true, true
	}
	if ssReserved(msg) {
		return false, false
	}
	if Typing(msg) && !ssTypable(msg) {
		// A printable the search will not take — a space, a tab — must not
		// reach EditKey either, which would happily insert it. Only plain
		// typing is screened, since that is all EditKey inserts.
		return false, false
	}
	return s.field.Key(msg)
}

// ssTypable reports whether a key press is a plain printable rune the query
// takes: text, no modifier chord, no whitespace (space toggles the row under
// the cursor, and a tab is nobody's search term).
func ssTypable(msg tea.KeyPressMsg) bool {
	return Typing(msg) && !strings.ContainsAny(msg.Text, " \t\r\n")
}

// Matches reports whether text passes the query. The test is a
// case-insensitive substring, deliberately not the fuzzy subsequence the
// palette and the issue-list match use: a type-ahead is read as literal
// typing, and a subsequence match turns "bro" into a hit on "Filter by label
// (the filter's label section)" — narrowing that surprises rather than helps.
func (s *SpeedSearch) Matches(text string) bool {
	if s.field.Empty() {
		return true
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(s.field.Text))
}

// Filter returns the indices of rows the query matches, in row order. An idle
// search returns every index, so callers need no special case.
func (s *SpeedSearch) Filter(rows []string) []int {
	out := make([]int, 0, len(rows))
	for i, row := range rows {
		if s.Matches(row) {
			out = append(out, i)
		}
	}
	return out
}

// Narrow keeps the entries of items whose row label matches the query, in
// their original order. label maps one entry to the text the query is matched
// against, so a picker can search a chip, a login or a whole sentence.
func Narrow[T any](s *SpeedSearch, items []T, label func(T) string) []T {
	if !s.Active() {
		return items
	}
	out := make([]T, 0, len(items))
	for _, it := range items {
		if s.Matches(label(it)) {
			out = append(out, it)
		}
	}
	return out
}

// NarrowStrings is Narrow for a plain row-label slice.
func NarrowStrings(s *SpeedSearch, rows []string) []string {
	return Narrow(s, rows, func(r string) string { return r })
}

// Hint is the query rendered for a modal heading, "" while the search is
// idle. The block marks where the next rune lands, so the type-ahead reads as
// an input rather than as part of the title; since the caret moves (#2360) it
// sits wherever the caret is, not always at the end.
func (s *SpeedSearch) Hint() string {
	if s.field.Empty() {
		return ""
	}
	r := s.field.Runes()
	cur := s.field.Cur
	if cur < 0 {
		cur = 0
	}
	if cur > len(r) {
		cur = len(r)
	}
	return "/" + string(r[:cur]) + "▏" + string(r[cur:])
}
