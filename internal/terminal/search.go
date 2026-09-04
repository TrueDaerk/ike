package terminal

// search.go — scrollback search (#1169), the explorer speed-search pattern
// (#1087) applied to the terminal's history: `/` while the pane is scrolled
// into scrollback opens a one-line search field on the pane's bottom row, and
// typing jumps incrementally to the nearest match ABOVE the anchored view —
// searching history usually goes backward, so the scan walks upward from
// where the search opened, wrapping to the newest match when nothing older
// matches. ctrl+p / up step further back, ctrl+n / down forward, both with
// wrap; matches on the visible rows are reverse-video highlighted and the
// field carries a `3/17` counter. Matching is the in-pane search's smartcase
// substring (ui.SmartCaseContains, #2461) over the plain line text — no
// regex.
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
//
// The field itself is the shared ui.LineSearch (#2461); what is the
// terminal's own is the anchor the jump scans from, the restore-on-esc
// offset, and the match memo.

import (
	"image/color"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/ui"
)

// termSearch is the open scrollback search: the shared prompt state, the
// scroll offset at activation (esc restores it), the anchor line jumps scan
// upward from, and the current match's virtual line (-1 while none) —
// tracked as a line rather than an index because new output keeps shifting
// the match list under it.
type termSearch struct {
	ui.LineSearch
	prevScroll int // scroll offset when the search opened; esc returns here
	anchor     int // bottom-most visible virtual line at activation
	curLine    int // virtual line of the current match, -1 without one

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

// OpenSearch implements the pane's Searchable capability (#2409): the
// shared find chord opens copy mode's own search when copy mode holds the
// keyboard (#2162), and the scrollback search otherwise. It reports false
// under an alt-screen or mouse-reporting child, which owns its own find.
func (m *Model) OpenSearch() bool {
	if m.copy != nil {
		m.openCopySearch()
		return true
	}
	return m.StartSearch()
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
		curLine:    -1,
	}
	m.search.Start()
}

// searchKey feeds one key to the open search. Every key is consumed while the
// field is open — own chords (match navigation) take priority and are handled
// first; anything left over goes to the shared prompt for accept/cancel and
// the line editing (#1882), which never falls through to the shell either way
// (searchKey's caller discards the result regardless).
func (m *Model) searchKey(msg tea.KeyPressMsg) {
	s := m.search
	switch {
	case msg.String() == "ctrl+n" || msg.Code == tea.KeyDown:
		m.searchStep(1)
		return
	case msg.String() == "ctrl+p" || msg.Code == tea.KeyUp:
		m.searchStep(-1)
		return
	case ui.PrevMatchChord(msg.String()):
		// The shared match-step chord (#2410): the field owns the keyboard
		// while it is open, so cmd+g / cmd+shift+g are answered here.
		m.searchStep(-1)
		return
	case ui.NextMatchChord(msg.String()):
		m.searchStep(1)
		return
	}
	_, changed, action := s.Key(msg)
	switch action {
	case ui.SearchCancel:
		// Cancel: the view returns to where the search opened (clamped — new
		// output may have grown the scrollback meanwhile).
		m.search = nil
		m.scroll = clamp(s.prevScroll, 0, m.sess.ScrollbackLen())
	case ui.SearchAccept:
		m.search = nil // accept: the view stays on the match
	default:
		if changed {
			m.searchJump()
		}
	}
}

// searchPaste routes a pasted block into the open field (#1882) and re-jumps
// like a typed edit.
func (m *Model) searchPaste(text string) {
	if m.search.Paste(text) {
		m.searchJump()
	}
}

// searchMatches returns the virtual line indices — over [scrollback ++
// screen] — whose text contains the query (smartcase), ascending, and keeps
// the shared state's match list on it. Empty without an open search or with
// an empty query.
func (m Model) searchMatches() []int {
	s := m.search
	if s == nil || s.Text == "" || m.sess == nil {
		return nil
	}
	total := m.sess.ScrollbackLen() + m.h
	ver := m.sess.version.Load()
	if s.memoValid && s.memoQuery == s.Text && s.memoVersion == ver && s.memoTotal == total {
		return s.Matches
	}
	var out []int
	for v := 0; v < total; v++ {
		if ui.SmartCaseContains(s.Text, m.sess.LineText(v)) {
			out = append(out, v)
		}
	}
	// The version was read before the scan: a mutation racing it bumps the
	// live counter past ver, so the stale memo recomputes on the next call —
	// a newer grid can never be served under an older key (the #803 render
	// cache argument).
	s.memoValid, s.memoQuery, s.memoVersion, s.memoTotal = true, s.Text, ver, total
	s.Matches = out
	s.Locate(s.curLine)
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
	s.Wrapped = false // an edited query starts a fresh walk (#2410)
	if s.Text == "" {
		s.curLine, s.Cur = -1, -1
		m.scroll = clamp(s.prevScroll, 0, m.sess.ScrollbackLen())
		return
	}
	matches := m.searchMatches()
	if len(matches) == 0 {
		s.curLine, s.Cur = -1, -1
		return
	}
	pick := matches[len(matches)-1] // wrap target: the newest match
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i] <= s.anchor {
			pick = matches[i]
			break
		}
	}
	s.curLine = pick
	s.Locate(pick)
	m.searchShow(pick)
}

// searchStep moves to the next (dir > 0, toward newer) or previous (toward
// older) match relative to the current one, wrapping around.
func (m *Model) searchStep(dir int) ui.MatchStep {
	s := m.search
	matches := m.searchMatches()
	if s == nil {
		return ui.NoStep
	}
	if len(matches) == 0 {
		s.Wrapped = false
		return ui.NoMatches()
	}
	if s.curLine < 0 {
		m.searchJump()
		return s.Stat()
	}
	val, st := s.StepFrom(s.curLine, dir)
	s.curLine = val
	m.searchShow(val)
	return st
}

// NextMatch implements the pane's match-step capability (#2410): cmd+g steps
// copy mode's accepted search when copy mode holds the keyboard (#2162) and
// the scrollback search otherwise, in both cases leaving the query line as it
// was. With neither open the chord is not ours — it stays with the shell.
func (m *Model) NextMatch() ui.MatchStep { return m.stepSearch(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.stepSearch(-1) }

func (m *Model) stepSearch(delta int) ui.MatchStep {
	if m.copy != nil {
		return m.copyStepMatch(delta)
	}
	if m.search == nil {
		return ui.NoStep
	}
	return m.searchStep(delta)
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
	if s == nil || s.Text == "" {
		return
	}
	for i := range rows {
		q, text := ui.SmartCaseFold(s.Text, m.sess.LineText(firstVirtual+i))
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

// searchLine renders the search input for the pane's bottom row — the shared
// prompt row (#2461): slash prefix, the query with a block cursor, and a dim
// `3/17` counter (or `no matches` in the Error colour).
func (m Model) searchLine() string {
	s := m.search
	m.searchMatches() // refresh the memo, and Cur with it
	var errCol, dimCol color.Color = lipgloss.Red, lipgloss.Color("245")
	if m.pal != nil {
		errCol, dimCol = m.pal.Error, m.pal.InlayHint
	}
	line := s.LineStyled(lipgloss.NewStyle().Foreground(dimCol), lipgloss.NewStyle().Foreground(errCol))
	w := m.w
	if w < 1 {
		w = 1
	}
	return ansi.Truncate(line, w, "…")
}
