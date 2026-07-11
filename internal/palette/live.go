package palette

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// live.go is the palette's asynchronous-mode plumbing (0250 phase 2, #295).
// A LiveMode's rows come from an external source (an LSP request) instead of
// a local walk: every query edit schedules a debounce tick; when the tick
// fires with the query unchanged, QueryChanged builds the request command.
// Fresh rows land in the mode's cache (however it stores them) and the host
// calls Refresh so the visible list recomputes.

// LiveDebounce is how long typing must pause before a live mode re-queries
// its source — long enough to coalesce a burst of keystrokes, short enough
// to feel immediate.
const LiveDebounce = 150 * time.Millisecond

// LiveMode is an optional Mode extension for asynchronous sources. Results
// keeps serving the cached rows; QueryChanged issues the re-query for a
// settled query and is only called through the debounce.
type LiveMode interface {
	Mode
	QueryChanged(query string, cx Context) tea.Cmd
}

// LiveTickMsg is the debounce timer firing; Gen pins it to the edit burst
// that scheduled it, so stale ticks (the query changed again) are dropped.
type LiveTickMsg struct{ Gen int }

// liveKick schedules the debounce tick when the active mode is live. Called
// from every query-edit path; each call invalidates the previous tick.
func (p *Palette) liveKick() tea.Cmd {
	m, _ := p.mode()
	if _, ok := m.(LiveMode); !ok {
		return nil
	}
	p.liveGen++
	gen := p.liveGen
	return tea.Tick(LiveDebounce, func(time.Time) tea.Msg { return LiveTickMsg{Gen: gen} })
}

// LiveTick handles a debounce tick: still open, still the latest edit, still
// a live mode — then the mode re-queries its source.
func (p *Palette) LiveTick(msg LiveTickMsg) tea.Cmd {
	if !p.open || msg.Gen != p.liveGen {
		return nil
	}
	m, body := p.mode()
	live, ok := m.(LiveMode)
	if !ok {
		return nil
	}
	return live.QueryChanged(body, p.cx)
}

// Refresh recomputes the visible rows after a live mode cached fresh results
// (a no-op while closed). The selection resets like any query edit.
func (p *Palette) Refresh() {
	if !p.open {
		return
	}
	p.recompute()
}
