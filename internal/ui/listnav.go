package ui

// listnav.go is the shared selection-list navigation layer (#1666). Before it
// every selectable list — palette, finder, the locations views, settings
// pages, the marketplace, the pickers — rolled its own cursor math, so paging
// existed in some views and not others and no list wrapped. These primitives
// fix the semantics in one place:
//
//   - single steps wrap around (down on the last entry lands on the first),
//   - page jumps clamp at the ends (the vim/fzf convention — wrapping a page
//     jump is disorienting),
//   - home/end go to the extremes,
//   - the scroll window follows the selection.

// ClampIndex returns i confined to [0, n-1], or 0 for an empty list.
func ClampIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// StepIndex returns i moved by delta single steps over n entries, wrapping at
// both ends: stepping down off the last entry lands on the first, stepping up
// off the first lands on the last. An empty list stays at 0.
func StepIndex(i, delta, n int) int {
	if n <= 0 {
		return 0
	}
	return ((i+delta)%n + n) % n
}

// PageIndex returns i moved by delta pages of size page over n entries,
// clamped to the ends. A page below 1 falls back to a single row so a
// mis-sized viewport still moves the cursor.
func PageIndex(i, delta, n, page int) int {
	if n <= 0 {
		return 0
	}
	if page < 1 {
		page = 1
	}
	return ClampIndex(i+delta*page, n)
}

// ScrollToShow returns the scroll window's new top row so that row sel stays
// inside a height-row window over n rows. It never scrolls past the end and
// never returns a negative offset.
func ScrollToShow(top, sel, height, n int) int {
	return ScrollToShowOff(top, sel, height, n, 0)
}

// ScrollToShowOff is ScrollToShow with a scrolloff margin (#2041): the window
// keeps off rows visible above and below sel wherever the list allows it, so
// navigating downwards moves the window on as soon as the cursor reaches the
// off-th-last visible row instead of only at the very edge (vim's
// 'scrolloff'). The margin is capped at (height-1)/2 so it can always be
// honoured on both sides, and it is clipped against the list ends — the first
// and last rows still sit flush against the window edge.
func ScrollToShowOff(top, sel, height, n, off int) int {
	if height < 1 {
		return 0
	}
	if off < 0 {
		off = 0
	}
	if max := (height - 1) / 2; off > max {
		off = max
	}
	lo, hi := sel-off, sel+off
	if lo < 0 {
		lo = 0
	}
	if hi > n-1 {
		hi = n - 1
	}
	if lo < top {
		top = lo
	}
	if hi >= top+height {
		top = hi - height + 1
	}
	if max := n - height; top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	return top
}

// NavKeys selects which key aliases ListNav recognises. Views differ in what
// they can spare: a list behind a text query cannot claim j/k or home/end,
// a modal picker can.
type NavKeys uint8

const (
	// NavArrows is up/down plus pgup/pgdown.
	NavArrows NavKeys = 1 << iota
	// NavEmacs is ctrl+n / ctrl+p.
	NavEmacs
	// NavVim is the j/k step aliases.
	NavVim
	// NavVimExtremes is g/G. Kept apart from NavVim because lists whose
	// single-letter keys are actions (the settings pages) can spare j/k but
	// not every letter.
	NavVimExtremes
	// NavHomeEnd is home/end. Lists behind a text input leave these to the
	// query cursor.
	NavHomeEnd
)

// NavDefault is the set for a list that owns its keys but has no text query:
// arrows, page keys, ctrl+n/ctrl+p and home/end.
const NavDefault = NavArrows | NavEmacs | NavHomeEnd

// NavFull adds the vim aliases on top of NavDefault.
const NavFull = NavDefault | NavVim | NavVimExtremes

// ListNav routes one key against a selection cursor over n entries and
// reports whether it consumed the key. page is the visible row count — the
// jump size for pgup/pgdown. Single steps wrap, page jumps clamp.
func ListNav(key string, sel *int, n, page int, keys NavKeys) bool {
	if n <= 0 {
		return false
	}
	has := func(k NavKeys) bool { return keys&k != 0 }
	switch key {
	case "up":
		if !has(NavArrows) {
			return false
		}
		*sel = StepIndex(*sel, -1, n)
	case "down":
		if !has(NavArrows) {
			return false
		}
		*sel = StepIndex(*sel, 1, n)
	case "k":
		if !has(NavVim) {
			return false
		}
		*sel = StepIndex(*sel, -1, n)
	case "j":
		if !has(NavVim) {
			return false
		}
		*sel = StepIndex(*sel, 1, n)
	case "ctrl+p":
		if !has(NavEmacs) {
			return false
		}
		*sel = StepIndex(*sel, -1, n)
	case "ctrl+n":
		if !has(NavEmacs) {
			return false
		}
		*sel = StepIndex(*sel, 1, n)
	case "pgup":
		if !has(NavArrows) {
			return false
		}
		*sel = PageIndex(*sel, -1, n, page)
	case "pgdown":
		if !has(NavArrows) {
			return false
		}
		*sel = PageIndex(*sel, 1, n, page)
	case "home":
		if !has(NavHomeEnd) {
			return false
		}
		*sel = 0
	case "end":
		if !has(NavHomeEnd) {
			return false
		}
		*sel = n - 1
	case "g":
		if !has(NavVimExtremes) {
			return false
		}
		*sel = 0
	case "G":
		if !has(NavVimExtremes) {
			return false
		}
		*sel = n - 1
	default:
		return false
	}
	return true
}
