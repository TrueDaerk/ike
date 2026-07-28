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
	caret   uint64
	entries map[lineKey]string
}

func newLineCache() *lineCacheStore {
	return &lineCacheStore{entries: make(map[lineKey]string)}
}

// syncEpoch drops every entry when the render epoch or the caret state has moved
// since the cache was last valid, so a stale body is never returned. Called once
// per View.
func (m *Model) syncEpoch() {
	if m.lineCache == nil {
		m.lineCache = newLineCache()
	}
	caret := m.caretState()
	if m.lineCache.epoch != m.renderEpoch || m.lineCache.caret != caret ||
		len(m.lineCache.entries) > lineCacheCap {
		clear(m.lineCache.entries)
		m.lineCache.epoch, m.lineCache.caret = m.renderEpoch, caret
	}
}

// caretState folds everything about the caret set that a rendered line body
// depends on: the primary cursor (its line draws the cursor cell and shows raw
// markdown source), the selection (mode + anchor), and the secondary carets
// (#145). The render epoch alone used to stand for all of this, but it is only
// bumped from Update — so a *mouse*-driven cursor or selection change left both
// caches valid and the pane served the previous frame, drawing the caret at its
// old position while typing went to the new one (#1327). Deriving the state
// instead of bumping a counter keeps every future mouse entry point correct
// without it having to remember, and costs nothing when nothing moved: an
// unchanged caret keeps the cache warm, so vertical scrolling still hits it.
func (m Model) caretState() uint64 {
	const prime = 1099511628211
	h := uint64(1469598103934665603)
	mix := func(v uint64) { h ^= v; h *= prime }
	mix(uint64(m.cursor.Line))
	mix(uint64(m.cursor.Col))
	mix(uint64(m.mode))
	if m.mode.IsVisual() {
		mix(uint64(m.anchor.Line))
		mix(uint64(m.anchor.Col))
	}
	if m.focused {
		mix(1)
	}
	for _, c := range m.carets {
		mix(uint64(c.pos.Line))
		mix(uint64(c.pos.Col))
	}
	return h
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
	// The caret set is a direct render input that no epoch bump stands for on
	// the mouse paths (#1327): without it a click or drag produced an
	// identical version and the pane reused its previous frame.
	mix(m.caretState())
	mix(uint64(m.view.Top))
	mix(uint64(m.view.Left))
	mix(uint64(m.height))
	if m.bpSource != nil {
		for _, ln := range m.bpSource(m.path) {
			mix(uint64(ln) + 1)
		}
	}
	return h
}
