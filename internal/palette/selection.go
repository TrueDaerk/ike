package palette

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// selection.go is the palette's highlighted-row plumbing (#2252), the
// selection twin of live.go: a SelectionMode is told when the highlight lands
// on a different row, through a debounce, so walking a long list with the
// arrow keys does not fire one request per step. The intention popup resolves
// the highlighted action's edit this way and renders it as a diff preview.

// SelectionDebounce is how long the highlight must rest on a row before its
// mode is asked about it — long enough to skip the rows a held arrow key
// races through, short enough that a deliberate stop feels immediate.
const SelectionDebounce = 120 * time.Millisecond

// SelectionMode is an optional Mode extension for rows whose selection has a
// cost: SelectionChanged is called through the debounce with the row that
// settled, and returns the command that fetches whatever it needs. It must not
// act on the row — only look at it.
type SelectionMode interface {
	Mode
	SelectionChanged(sel Item, cx Context) tea.Cmd
}

// SelectionTickMsg is the selection debounce firing; Gen pins it to the move
// that scheduled it, so a tick for a row the highlight has already left is
// dropped.
type SelectionTickMsg struct{ Gen int }

// SelectionKick schedules the debounce tick when the active mode watches its
// selection. Every path that can move the highlight — the arrow keys, the page
// jumps, a click, a fresh result list — calls it, and each call invalidates
// the previous tick. It is exported because opening the popup is a selection
// change too: the first row is highlighted from the start, and the app kicks
// the tick right after it opens the box.
func (p *Palette) SelectionKick() tea.Cmd {
	m, _ := p.mode()
	if _, ok := m.(SelectionMode); !ok {
		return nil
	}
	p.selGen++
	gen := p.selGen
	return tea.Tick(SelectionDebounce, func(time.Time) tea.Msg { return SelectionTickMsg{Gen: gen} })
}

// queryKick is what every query-editing path returns: a live mode's re-query
// debounce (#295) plus, for a selection-watching mode, the selection one — a
// new result list lands the highlight on a different row, which is a move like
// any other (#2252).
func (p *Palette) queryKick() tea.Cmd {
	return tea.Batch(p.liveKick(), p.SelectionKick())
}

// SelectionTick handles a debounce tick: still open, still the latest move,
// still a selection mode with a row under the highlight — then the mode is
// told which row settled.
func (p *Palette) SelectionTick(msg SelectionTickMsg) tea.Cmd {
	if !p.open || msg.Gen != p.selGen {
		return nil
	}
	m, _ := p.mode()
	sm, ok := m.(SelectionMode)
	if !ok {
		return nil
	}
	sel, ok := p.focusedItem()
	if !ok {
		return nil
	}
	return sm.SelectionChanged(sel, p.cx)
}

// FooterMode is an optional Mode extension (#2252): a locked mode that renders
// its own lines under the result list, separated by the same dim rule as the
// query row. The intention popup puts the highlighted action's diff preview
// there; every other mode returns nothing and keeps the plain list layout.
type FooterMode interface {
	// Footer returns the lines to render for the selected row, already
	// clipped to width. Nil renders nothing at all — no rule, no gap.
	Footer(sel Item, width int) []string
}

// footerLines asks a locked FooterMode for the selected row's extra lines. An
// unlocked palette, another mode or an empty list renders nothing.
func (p *Palette) footerLines(width int) []string {
	fm, ok := p.locked.(FooterMode)
	if !ok {
		return nil
	}
	sel, ok := p.focusedItem()
	if !ok {
		return nil
	}
	return fm.Footer(sel, width)
}
