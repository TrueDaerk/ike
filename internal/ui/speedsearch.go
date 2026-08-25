package ui

// speedsearch.go is the shared type-ahead ("speed search") layer for modal
// pickers (#2111). JetBrains popups all narrow as you type; IKE's overlays
// only arrow-walked, so a repository with forty labels meant walking a modal
// row by row. SpeedSearch gives any picker that renders a list of row labels
// the same behaviour in one place:
//
//   - printable keys append to the query and narrow the visible rows live,
//   - the narrowing keeps the picker's own row order (the query filters, it
//     does not re-rank) so a row never jumps around under the cursor,
//   - backspace deletes the last query rune, but only while a query is
//     running — with an empty query it falls through to whatever the picker
//     assigned it (the mutation pickers clear their selection with it),
//   - space is never consumed, because the pickers toggle the row under the
//     cursor with it,
//   - esc is left to the caller, which clears the query first and only closes
//     the modal on the second press (EscClears).
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
	query string
}

// Query returns the typed text, "" when the search is idle.
func (s *SpeedSearch) Query() string { return s.query }

// Active reports whether a query is narrowing the rows.
func (s *SpeedSearch) Active() bool { return s.query != "" }

// Reset drops the query. Pickers call it when they open, so a modal never
// inherits the previous one's type-ahead.
func (s *SpeedSearch) Reset() { s.query = "" }

// EscClears is esc's first job: it drops a running query and reports true, so
// the caller closes the modal only on the second press.
func (s *SpeedSearch) EscClears() bool {
	if s.query == "" {
		return false
	}
	s.query = ""
	return true
}

// Key feeds one key to the search. It consumes printable runes (never space,
// never a modifier chord) and backspace while a query is running, and reports
// whether it took the key and whether the query changed. A caller that sees
// changed must re-clamp its cursor — the visible row set just moved.
func (s *SpeedSearch) Key(msg tea.KeyPressMsg) (handled, changed bool) {
	switch msg.String() {
	case "backspace":
		if s.query == "" {
			return false, false
		}
		r := []rune(s.query)
		s.query = string(r[:len(r)-1])
		return true, true
	case "space", " ":
		// The pickers toggle the row under the cursor with space.
		return false, false
	}
	if msg.Text == "" || msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper|tea.ModMeta) != 0 {
		return false, false
	}
	text := msg.Text
	if strings.ContainsAny(text, " \t\r\n") {
		return false, false
	}
	s.query += text
	return true, true
}

// Matches reports whether text passes the query. The test is a
// case-insensitive substring, deliberately not the fuzzy subsequence the
// palette and the issue-list match use: a type-ahead is read as literal
// typing, and a subsequence match turns "bro" into a hit on "Filter by label
// (the filter's label section)" — narrowing that surprises rather than helps.
func (s *SpeedSearch) Matches(text string) bool {
	if s.query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(s.query))
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
// idle. The trailing block marks where the next rune lands, so the type-ahead
// reads as an input rather than as part of the title.
func (s *SpeedSearch) Hint() string {
	if s.query == "" {
		return ""
	}
	return "/" + s.query + "▏"
}
