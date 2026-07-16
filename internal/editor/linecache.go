package editor

// linecache.go memoizes rendered line bodies so a vertical scroll — which shifts
// which buffer lines are visible but changes no line's *content* (renderSpan
// never reads view.Top) — reuses them instead of re-highlighting every visible
// line each frame (#614).
//
// Correctness rests on renderEpoch: a counter bumped on every mutation that can
// change a line body (edits, cursor/selection moves, resize, horizontal scroll,
// theme/config, and — via the Update choke point — every decoration message:
// syntax, semantic, diagnostics, git marks, occurrences, inlay hints). Vertical
// scroll deliberately does NOT bump it. Each cache entry records the epoch it was
// built at; a lookup at a newer epoch misses and recomputes. The gutter
// (line numbers, diagnostic/git/breakpoint/paused signs) is rendered fresh every
// frame outside this cache, so those decorations can never go stale from it.
//
// The store is a pointer so the many value-copies of a Model that share one view
// (each Update returns a fresh Model value) share one cache, while a genuinely
// separate view — created by New or reset by ShareDocumentWith — gets its own,
// avoiding cross-view collisions on shared documents (#142).

// lineKey identifies a rendered span: the buffer line plus the column window
// (from, to) so soft-wrap segments and horizontal-scroll offsets key distinctly.
type lineKey struct {
	line, from, to, width int
}

// lineCacheCap bounds the entry count; past it the store is cleared (scrolling a
// very large file would otherwise retain a body per line visited).
const lineCacheCap = 4096

type lineCacheStore struct {
	epoch   uint64
	entries map[lineKey]string
}

func newLineCache() *lineCacheStore {
	return &lineCacheStore{entries: make(map[lineKey]string)}
}

// syncEpoch drops every entry when the render epoch has moved since the cache was
// last valid, so a stale body is never returned. Called once per View.
func (m *Model) syncEpoch() {
	if m.lineCache == nil {
		m.lineCache = newLineCache()
	}
	if m.lineCache.epoch != m.renderEpoch || len(m.lineCache.entries) > lineCacheCap {
		clear(m.lineCache.entries)
		m.lineCache.epoch = m.renderEpoch
	}
}

// cachedSpan returns the memoized body for key and whether it was present. The
// caller must have run syncEpoch this frame, so a hit is guaranteed current.
func (m Model) cachedSpan(key lineKey) (string, bool) {
	if m.lineCache == nil {
		return "", false
	}
	body, ok := m.lineCache.entries[key]
	return body, ok
}

// storeSpan memoizes a freshly rendered body.
func (m Model) storeSpan(key lineKey, body string) {
	if m.lineCache != nil {
		m.lineCache.entries[key] = body
	}
}

// bumpRender invalidates the line cache by advancing the render epoch. Called
// from every mutation that can change a rendered line body — but NOT from
// vertical scroll, whose whole point is to keep the cache warm.
func (m *Model) bumpRender() { m.renderEpoch++ }

// RenderVersion is a complete identity of everything View() renders (#615), for
// the pane-level View cache: renderEpoch (line bodies + all Update-routed and
// direct body mutations) folded with the inputs it deliberately omits — the
// vertical scroll position and viewport height (which change *which* lines show
// but not their bodies) and the breakpoint set (external, read fresh each frame
// via bpSource, so hashed here). Two equal versions render byte-identical.
func (m Model) RenderVersion() uint64 {
	const prime = 1099511628211
	h := uint64(1469598103934665603)
	mix := func(v uint64) { h ^= v; h *= prime }
	mix(m.renderEpoch)
	mix(uint64(m.view.Top))
	mix(uint64(m.height))
	if m.bpSource != nil {
		for _, ln := range m.bpSource(m.path) {
			mix(uint64(ln) + 1)
		}
	}
	return h
}
