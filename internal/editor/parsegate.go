package editor

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// parsegate.go bounds the syntax parses in flight for one view (#2770).
//
// Every buffer change schedules a parse command, and bubbletea runs commands
// concurrently: typing faster than a parse completes (a few thousand lines
// of HTML take ~100 ms — host grammar plus every injected <style>/<script>
// fragment) used to pile up one parse goroutine per keystroke, all of them
// CGo-bound, all but the last producing a result the version guard would
// drop. The event loop then competed with a dozen parsers for the CPU, and
// each keystroke waited seconds for its frame.
//
// The gate runs one parse at a time per view and always parses the newest
// snapshot. parseCmd stores the snapshot it took as the gate's latest and
// returns a command; commands take turns on the work lock, and each one, on
// its turn, parses whatever snapshot is pending — the newest — or yields
// nil (bubbletea skips nil messages) when an earlier turn already parsed
// it. A burst of N keystrokes during one parse therefore costs two parses,
// the running one and one of the text as it stands when it finishes, both
// delivered; the N-2 superseded snapshots are never parsed. Waiting
// commands are parked goroutines, not parsers. The lock is released when a
// parse finishes — not when its result is delivered — so a result routed
// nowhere (the view's key changed under it) cannot wedge the gate.

// parseSnapshot is what one parse needs: the buffer lines at scheduling time
// and the keys the result travels under.
type parseSnapshot struct {
	key      string
	langPath string
	version  int
	lines    []string
}

// parseGate is shared by every copy of a view's Model (a pointer field, like
// sbcache), so the pending snapshot and the work lock survive the value-copy
// Update cycle.
type parseGate struct {
	mu     sync.Mutex // guards latest
	work   sync.Mutex // held by the one command parsing
	latest *parseSnapshot
}

// schedule stores snap as the newest pending snapshot and returns the
// command that parses it — or, when an earlier command's turn already
// parsed a newer snapshot, a command yielding nil. parse turns one snapshot
// into its result message.
func (g *parseGate) schedule(snap parseSnapshot, parse func(parseSnapshot) tea.Msg) tea.Cmd {
	if g == nil {
		return func() tea.Msg { return parse(snap) }
	}
	g.mu.Lock()
	g.latest = &snap
	g.mu.Unlock()
	return func() tea.Msg {
		g.work.Lock()
		defer g.work.Unlock()
		g.mu.Lock()
		next := g.latest
		g.latest = nil
		g.mu.Unlock()
		if next == nil {
			// A command ahead of this one took the newest snapshot: this
			// one's is superseded and there is nothing left to parse.
			return nil
		}
		return parse(*next)
	}
}

// pending reports whether a snapshot waits for its turn (tests).
func (g *parseGate) pending() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.latest != nil
}
