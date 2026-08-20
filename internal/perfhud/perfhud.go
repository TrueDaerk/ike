// Package perfhud collects IKE's own runtime cost for the in-app performance
// HUD (#1999): bubbletea message rates by coarse category, per-pane render
// time, frame cost, goroutines, heap and RSS — plus a short rolling history so
// a spike stays visible after the fact.
//
// The collector is deliberately a package-level singleton (like the render-cost
// gauge in internal/app): the root model is a value type copied on every
// message, so measurement state cannot live in it. Everything runs on the
// bubbletea goroutine — Update counts, render attributes, the HUD's own tick
// samples — but the mutex is kept anyway so a probe from a Cmd goroutine cannot
// corrupt the maps.
//
// Cheap when hidden is the design constraint (the #1095–#1101 lesson): every
// hook is guarded by an atomic bool the caller checks first, so a disabled HUD
// costs one atomic load per message and per pane per frame — no allocation, no
// map touch, no timing syscall. The per-type message breakdown and the
// per-interval runtime.ReadMemStats only ever run while the HUD is on.
package perfhud

import (
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

// maxTypes caps the per-message-type breakdown a sample carries: the point is
// naming the loudest source, not enumerating every message.
const maxTypes = 4

// maxPanes caps the per-pane render breakdown a sample carries.
const maxPanes = 8

// defaultHistory is the rolling-history length before SetHistory says
// otherwise: 60 samples at the default 1s interval — one minute.
const defaultHistory = 60

// Sample is one interval's aggregate: rates over the window that ended at At,
// plus the runtime gauges read at that instant.
type Sample struct {
	At     time.Time     // end of the window
	Window time.Duration // wall time the rates are divided by
	// Live reports whether message/render collection was on for the window.
	// A snapshot taken with the HUD off still carries honest runtime gauges,
	// but its rates are meaningless — the consumer says so instead of
	// printing zeros as if they were measured.
	Live bool

	Msgs    uint64            // messages counted in the window
	MsgRate float64           // messages per second
	Rates   [CatCount]float64 // per-category messages per second
	Types   []TypeRate        // loudest concrete message types

	Frames    int           // full frames composed in the window
	FrameRate float64       // frames per second
	FrameAvg  time.Duration // mean full-frame composition cost
	FrameMax  time.Duration // worst full-frame composition cost
	Panes     []PaneCost    // per-pane render cost, most expensive first

	Goroutines int           // runtime.NumGoroutine
	Timers     int           // armed app tickers/timers, supplied by the caller
	GCs        uint32        // collections during the window
	GCPause    time.Duration // stop-the-world pause accumulated during it
	HeapInuse  uint64        // live heap
	HeapSys    uint64        // heap address space obtained from the OS
	RSS        uint64        // resident set size, 0 when unavailable
	RSSPeak    bool          // RSS is the process peak, not the current value
}

// TypeRate is one concrete message type's share of a window.
type TypeRate struct {
	Type string  // reflect type name, e.g. "app.followTickMsg"
	N    uint64  // occurrences in the window
	Rate float64 // per second
}

// PaneCost is one pane's render cost over a window.
type PaneCost struct {
	Key    string        // pane registry key, e.g. "editor:2"
	Frames int           // frames the pane was rendered in
	Avg    time.Duration // mean cost per frame
	Total  time.Duration // summed cost over the window
}

// Collector aggregates one process's metrics. Use Default for the live one;
// New exists so tests never share state.
type Collector struct {
	// on gates every hook. Loaded (not locked) by the callers in the hot
	// paths, so a disabled HUD never touches the mutex.
	on atomic.Bool

	mu        sync.Mutex
	now       func() time.Time
	start     time.Time // window start; zero until the first enable/sample
	counts    [CatCount]uint64
	byType    map[string]uint64
	catOf     map[string]Category // classification cache, kept across windows
	frames    int
	frameSum  time.Duration
	frameMax  time.Duration
	paneSum   map[string]time.Duration
	paneN     map[string]int
	hist      []Sample
	histCap   int
	lastGC    uint32
	lastPause uint64
}

// New returns an idle collector with the default history length.
func New() *Collector {
	return &Collector{
		now:     time.Now,
		histCap: defaultHistory,
		byType:  map[string]uint64{},
		catOf:   map[string]Category{},
		paneSum: map[string]time.Duration{},
		paneN:   map[string]int{},
	}
}

var std = New()

// Default returns the process-wide collector the app hooks feed.
func Default() *Collector { return std }

// Enabled reports whether collection is on. Hot-path callers check this before
// calling any recording hook, so the off cost is a single atomic load.
func Enabled() bool { return std.on.Load() }

// Enabled reports whether collection is on for c.
func (c *Collector) Enabled() bool { return c.on.Load() }

// SetEnabled turns collection on or off. Enabling starts a fresh window and
// drops the previous history: rates spanning the off period would be fiction.
func (c *Collector) SetEnabled(on bool) {
	c.mu.Lock()
	if on && !c.on.Load() {
		c.resetWindow(c.now())
		c.hist = nil
	}
	c.mu.Unlock()
	c.on.Store(on)
}

// SetHistory sets how many samples the rolling history keeps; n < 1 is ignored.
// An already longer history is trimmed to the new bound.
func (c *Collector) SetHistory(n int) {
	if n < 1 {
		return
	}
	c.mu.Lock()
	c.histCap = n
	if len(c.hist) > n {
		c.hist = append(c.hist[:0], c.hist[len(c.hist)-n:]...)
	}
	c.mu.Unlock()
}

// SetNow injects the clock (tests); nil restores time.Now.
func (c *Collector) SetNow(f func() time.Time) {
	if f == nil {
		f = time.Now
	}
	c.mu.Lock()
	c.now = f
	c.mu.Unlock()
}

// Reset returns c to its idle state: collection off, window and history empty.
// The classification cache survives — it is pure derived data.
func (c *Collector) Reset() {
	c.on.Store(false)
	c.mu.Lock()
	c.resetWindow(time.Time{})
	c.hist = nil
	c.histCap = defaultHistory
	c.lastGC, c.lastPause = 0, 0
	c.mu.Unlock()
}

// Count records one dispatched message under its coarse category and its
// concrete type. Callers guard with Enabled; the check repeats here so a
// direct call cannot start accumulating behind a hidden HUD.
func (c *Collector) Count(msg tea.Msg) {
	if !c.on.Load() {
		return
	}
	name := typeName(msg)
	c.mu.Lock()
	cat, ok := c.catOf[name]
	if !ok {
		cat = classify(msg, name)
		c.catOf[name] = cat
	}
	c.counts[cat]++
	c.byType[name]++
	c.mu.Unlock()
}

// RecordFrame records one full-frame composition cost.
func (c *Collector) RecordFrame(d time.Duration) {
	if !c.on.Load() {
		return
	}
	c.mu.Lock()
	c.frames++
	c.frameSum += d
	if d > c.frameMax {
		c.frameMax = d
	}
	c.mu.Unlock()
}

// RecordPane attributes render time to one pane key. A pane rendered more than
// once in a window simply accumulates; Frames counts the renders, so the mean
// stays per render.
func (c *Collector) RecordPane(key string, d time.Duration) {
	if !c.on.Load() {
		return
	}
	c.mu.Lock()
	c.paneSum[key] += d
	c.paneN[key]++
	c.mu.Unlock()
}

// Sample closes the current window, appends the resulting Sample to the
// history and starts a new window. timers is the caller's count of armed
// tickers — only the app knows which of its debounce timers are in flight.
//
// It works with collection off too, so the snapshot command can report honest
// runtime gauges without turning the HUD on; the Sample is marked not Live.
func (c *Collector) Sample(timers int) Sample {
	c.mu.Lock()
	now := c.now()
	if c.start.IsZero() {
		c.start = now
	}
	window := now.Sub(c.start)
	secs := window.Seconds()
	if secs < 0 {
		secs = 0 // a zero-length window reports counts, not rates
	}
	s := Sample{
		At:       now,
		Window:   window,
		Live:     c.on.Load(),
		Frames:   c.frames,
		FrameMax: c.frameMax,
		Timers:   timers,
	}
	for i, n := range c.counts {
		s.Msgs += n
		if secs > 0 {
			s.Rates[i] = float64(n) / secs
		}
	}
	if secs > 0 {
		s.MsgRate = float64(s.Msgs) / secs
		s.FrameRate = float64(c.frames) / secs
	}
	if c.frames > 0 {
		s.FrameAvg = c.frameSum / time.Duration(c.frames)
	}
	s.Types = topTypes(c.byType, secs)
	s.Panes = topPanes(c.paneSum, c.paneN)
	c.resetWindow(now)
	lastGC, lastPause := c.lastGC, c.lastPause
	c.mu.Unlock()

	// Runtime gauges are read outside the window arithmetic: ReadMemStats
	// briefly stops the world, so it runs once per interval and only while
	// someone is looking.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s.Goroutines = runtime.NumGoroutine()
	s.HeapInuse = ms.HeapInuse
	s.HeapSys = ms.HeapSys
	s.RSS, s.RSSPeak = rss()
	if lastGC != 0 || lastPause != 0 {
		// The first sample has no predecessor to subtract from, so it
		// reports no GC activity rather than the process lifetime's.
		s.GCs = ms.NumGC - lastGC
		s.GCPause = time.Duration(ms.PauseTotalNs - lastPause)
	}

	c.mu.Lock()
	c.lastGC, c.lastPause = ms.NumGC, ms.PauseTotalNs
	c.hist = append(c.hist, s)
	if len(c.hist) > c.histCap {
		c.hist = append(c.hist[:0], c.hist[len(c.hist)-c.histCap:]...)
	}
	c.mu.Unlock()
	return s
}

// Latest returns the most recent sample; ok is false before the first one.
func (c *Collector) Latest() (Sample, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.hist) == 0 {
		return Sample{}, false
	}
	return c.hist[len(c.hist)-1], true
}

// History returns a copy of the rolling history, oldest first.
func (c *Collector) History() []Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Sample, len(c.hist))
	copy(out, c.hist)
	return out
}

// resetWindow clears the per-window accumulators and restarts the clock. The
// caller holds the mutex.
func (c *Collector) resetWindow(at time.Time) {
	c.start = at
	c.counts = [CatCount]uint64{}
	clear(c.byType)
	clear(c.paneSum)
	clear(c.paneN)
	c.frames, c.frameSum, c.frameMax = 0, 0, 0
}

// topTypes renders the loudest message types of a window, most frequent first
// and name-ordered on ties so the HUD does not flicker between equal rows.
func topTypes(counts map[string]uint64, secs float64) []TypeRate {
	if len(counts) == 0 {
		return nil
	}
	out := make([]TypeRate, 0, len(counts))
	for name, n := range counts {
		tr := TypeRate{Type: name, N: n}
		if secs > 0 {
			tr.Rate = float64(n) / secs
		}
		out = append(out, tr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Type < out[j].Type
	})
	if len(out) > maxTypes {
		out = out[:maxTypes]
	}
	return out
}

// topPanes renders the per-pane render costs of a window, most expensive
// first (by total time), key-ordered on ties.
func topPanes(sum map[string]time.Duration, n map[string]int) []PaneCost {
	if len(sum) == 0 {
		return nil
	}
	out := make([]PaneCost, 0, len(sum))
	for key, total := range sum {
		p := PaneCost{Key: key, Frames: n[key], Total: total}
		if p.Frames > 0 {
			p.Avg = total / time.Duration(p.Frames)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > maxPanes {
		out = out[:maxPanes]
	}
	return out
}

// SetEnabled toggles the process-wide collector.
func SetEnabled(on bool) { std.SetEnabled(on) }

// SetHistory sets the process-wide collector's history length.
func SetHistory(n int) { std.SetHistory(n) }

// Count records one message on the process-wide collector.
func Count(msg tea.Msg) { std.Count(msg) }

// RecordFrame records one frame cost on the process-wide collector.
func RecordFrame(d time.Duration) { std.RecordFrame(d) }

// RecordPane attributes render time on the process-wide collector.
func RecordPane(key string, d time.Duration) { std.RecordPane(key, d) }

// Collect closes the process-wide collector's window and returns the sample
// (the package-level spelling of (*Collector).Sample, which the Sample type
// name occupies here).
func Collect(timers int) Sample { return std.Sample(timers) }

// Latest returns the process-wide collector's most recent sample.
func Latest() (Sample, bool) { return std.Latest() }

// History returns the process-wide collector's rolling history.
func History() []Sample { return std.History() }

// Reset returns the process-wide collector to its idle state (tests).
func Reset() { std.Reset() }
