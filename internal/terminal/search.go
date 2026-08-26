package terminal

// search.go — scrollback search (#1169), the explorer speed-search pattern
// (#1087) applied to the terminal's history: `/` while the pane is scrolled
// into scrollback opens a one-line search field on the pane's bottom row, and
// typing jumps incrementally to the nearest match ABOVE the anchored view —
// searching history usually goes backward, so the scan walks upward from
// where the search opened, wrapping to the newest match when nothing older
// matches. ctrl+p / up step further back, ctrl+n / down forward, both with
// wrap; matches on the visible rows are reverse-video highlighted and the
// field carries a `3/17` counter. Matching is case-insensitive contains over
// the plain line text — no regex.
//
// Capture is deliberately narrow: only the scrolled plain-shell state owns
// `/`. At the live view the key is everyday shell input (`ls /tmp`), and in
// alt-screen or mouse-reporting children (vim, lazygit) it belongs to the
// child's own search — those states always pass `/` through (#96/#226
// routing states). Enter `shift+pgup`/wheel scrollback first, then `/`.
//
// While the field is open it owns the keyboard, like the explorer's: enter
// accepts (the view stays where the search put it), esc cancels (the scroll
// offset returns to where the search opened), backspace edits, and every
// other key is consumed — no silent passthrough into the shell mid-query.

import (
	"image/color"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/ui"
)

// termSearch is the open scrollback search: the query typed so far, the
// scroll offset at activation (esc restores it), the anchor line jumps scan
// upward from, and the current match's virtual line (-1 while none).
type termSearch struct {
	query      string
	pos        int // rune cursor position within query (#1882)
	prevScroll int // scroll offset when the search opened; esc returns here
	anchor     int // bottom-most visible virtual line at activation
	cur        int // virtual line of the current match, -1 without one

	// Match memo (#2163): searchMatches used to rescan the whole scrollback
	// — 10k LineText calls, each taking gridMu and allocating twice — and
	// searchLine runs it per *frame*. With a busy shell repainting at the
	// coalescer's rate that pinned a core and contended the feed loop for as
	// long as the field was open. Keyed by query, grid version and total
	// line count, the same shape as the session's render cache; held behind
	// the search pointer so value-model copies share it.
	memoValid   bool
	memoQuery   string
	memoVersion uint64
	memoTotal   int
	memo        []int
}

// Searching reports whether the scrollback search field is open.
func (m Model) Searching() bool { return m.search != nil }

// StartSearch opens the scrollback search from the app-side cmd+f entry point
// (#1504). Unlike `/` (which only captures while scrolled — the live shell
// needs the key for paths), an explicit chord carries intent, so it opens
// from the live view too; esc then returns to the live view (prevScroll 0).
// It reports false — leaving the chord with the child — under an alt-screen
// or mouse-reporting child (vim, lazygit own their find), and true without
// reopening when the field is already up.
func (m *Model) StartSearch() bool {
	if m.sess == nil || m.sess.AltScreen() || m.sess.WantsMouse() {
		return false
	}
	if m.search == nil {
		m.startSearch()
	}
	return true
}

// searchCaptures reports whether `/` opens the search in the current state:
// only a plain shell scrolled into scrollback — never the live view (the
// shell needs `/` for paths) and never an alt-screen or mouse-reporting child
// (vim/lazygit own their `/`).
func (m Model) searchCaptures() bool {
	return m.sess != nil && m.scroll > 0 &&
		!m.sess.AltScreen() && !m.sess.WantsMouse()
}

// startSearch opens the search field, anchored on the current view.
func (m *Model) startSearch() {
	sb := m.sess.ScrollbackLen()
	m.search = &termSearch{
		prevScroll: m.scroll,
		anchor:     sb - m.scroll + m.h - 1,
		cur:        -1,
	}
}

// searchKey feeds one key to the open search. Every key is consumed while the
// field is open — own chords (match navigation, accept/cancel) take priority
// and are handled first; anything left over goes to ui.EditKey for cursor
// movement, word motions and deletion (#1882), which never falls through to
// the shell either way (searchKey's caller discards the result regardless).
func (m *Model) searchKey(msg tea.KeyPressMsg) {
	s := m.search
	switch {
	case msg.Code == tea.KeyEscape:
		// Cancel: the view returns to where the search opened (clamped — new
		// output may have grown the scrollback meanwhile).
		m.search = nil
		m.scroll = clamp(s.prevScroll, 0, m.sess.ScrollbackLen())
		return
	case msg.Code == tea.KeyEnter:
		m.search = nil // accept: the view stays on the match
		return
	case msg.String() == "ctrl+n" || msg.Code == tea.KeyDown:
		m.searchStep(1)
		return
	case msg.String() == "ctrl+p" || msg.Code == tea.KeyUp:
		m.searchStep(-1)
		return
	}
	if out, ncur, handled, changed := ui.EditKey(msg, s.query, s.pos); handled {
		s.query, s.pos = out, ncur
		if changed {
			m.searchJump()
		}
	}
}

// searchMatches returns the virtual line indices — over [scrollback ++
// screen] — whose text contains the query, case-insensitively, ascending.
// Empty without an open search or with an empty query.
func (m Model) searchMatches() []int {
	s := m.search
	if s == nil || s.query == "" || m.sess == nil {
		return nil
	}
	total := m.sess.ScrollbackLen() + m.h
	ver := m.sess.version.Load()
	if s.memoValid && s.memoQuery == s.query && s.memoVersion == ver && s.memoTotal == total {
		return s.memo
	}
	q := strings.ToLower(s.query)
	var out []int
	for v := 0; v < total; v++ {
		if strings.Contains(strings.ToLower(m.sess.LineText(v)), q) {
			out = append(out, v)
		}
	}
	// The version was read before the scan: a mutation racing it bumps the
	// live counter past ver, so the stale memo recomputes on the next call —
	// a newer grid can never be served under an older key (the #803 render
	// cache argument).
	s.memoValid, s.memoQuery, s.memoVersion, s.memoTotal, s.memo = true, s.query, ver, total, out
	return out
}

// searchJump re-resolves the current match after a query edit, always from
// the stable anchor (the view bottom when the search opened), so growing and
// shrinking the query is deterministic: the nearest match at or above the
// anchor wins (history search goes backward); when every match lies below,
// the scan wraps to the newest (bottom-most) match. No match leaves the view
// put — the field shows the miss. An empty query returns to the anchor view.
func (m *Model) searchJump() {
	s := m.search
	if s == nil {
		return
	}
	if s.query == "" {
		s.cur = -1
		m.scroll = clamp(s.prevScroll, 0, m.sess.ScrollbackLen())
		return
	}
	matches := m.searchMatches()
	if len(matches) == 0 {
		s.cur = -1
		return
	}
	pick := matches[len(matches)-1] // wrap target: the newest match
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i] <= s.anchor {
			pick = matches[i]
			break
		}
	}
	s.cur = pick
	m.searchShow(pick)
}

// searchStep moves to the next (dir > 0, toward newer) or previous (toward
// older) match relative to the current one, wrapping around.
func (m *Model) searchStep(dir int) {
	s := m.search
	matches := m.searchMatches()
	if s == nil || len(matches) == 0 {
		return
	}
	if s.cur < 0 {
		m.searchJump()
		return
	}
	if dir > 0 {
		for _, v := range matches {
			if v > s.cur {
				s.cur = v
				m.searchShow(v)
				return
			}
		}
		s.cur = matches[0] // wrap to the oldest match
	} else {
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i] < s.cur {
				s.cur = matches[i]
				m.searchShow(matches[i])
				return
			}
		}
		s.cur = matches[len(matches)-1] // wrap to the newest match
	}
	m.searchShow(s.cur)
}

// searchShow scrolls the view so virtual line v sits near the middle.
func (m *Model) searchShow(v int) {
	sb := m.sess.ScrollbackLen()
	m.scroll = clamp(sb-v+m.h/2, 0, sb)
}

// searchHighlight reverse-videos every query occurrence on the visible rows;
// firstVirtual is the virtual line index rendered at rows[0].
func (m Model) searchHighlight(rows []string, firstVirtual int) {
	s := m.search
	if s == nil || s.query == "" {
		return
	}
	q := strings.ToLower(s.query)
	for i := range rows {
		text := strings.ToLower(m.sess.LineText(firstVirtual + i))
		off := 0
		for {
			idx := strings.Index(text[off:], q)
			if idx < 0 {
				break
			}
			from := utf8.RuneCountInString(text[:off+idx])
			to := from + utf8.RuneCountInString(q)
			rows[i] = reverseSpan(rows[i], from, to)
			off += idx + len(q)
		}
	}
}

// searchLine renders the search input for the pane's bottom row, mirroring
// the explorer's field (#1087): slash prefix, the query with a block cursor,
// and a dim `3/17` counter (or `no matches` in the Error colour).
func (m Model) searchLine() string {
	s := m.search
	line := "/" + ui.CursorView(s.query, s.pos)
	counter := ""
	if s.query != "" {
		matches := m.searchMatches()
		var errCol, dimCol color.Color = lipgloss.Red, lipgloss.Color("245")
		if m.pal != nil {
			errCol, dimCol = m.pal.Error, m.pal.InlayHint
		}
		if len(matches) == 0 {
			counter = lipgloss.NewStyle().Foreground(errCol).Render("  no matches")
		} else {
			cur := 0
			for i, v := range matches {
				if v == s.cur {
					cur = i + 1
					break
				}
			}
			counter = lipgloss.NewStyle().Foreground(dimCol).
				Render("  " + strconv.Itoa(cur) + "/" + strconv.Itoa(len(matches)))
		}
	}
	w := m.w
	if w < 1 {
		w = 1
	}
	return ansi.Truncate(line+counter, w, "…")
}
