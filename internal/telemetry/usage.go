package telemetry

// usage.go is the second read-back of the usage log (#2552): where report.go
// answers "how long did I work where", this file answers "what did I do" —
// which commands ran and from where, which chords found no binding in which
// context, how often each palette mode was dismissed rather than picked
// from, and which long-running operations and slow dispatches cost the most.
// Until now every one of those questions needed jq on the command line.
//
// The aggregates ride on the same streaming, per-file-cached reader as the
// time report: scanFile buckets every event by its own local calendar day,
// independent of the project span it falls into (pre-v3 logs have no session
// marker at all, and a usage question does not care which project it was).
// Everything stays local and read-only.

import (
	"sort"
	"strconv"
	"time"
)

// KeyStatusUnbound is the key-event status meaning "no binding matched".
const KeyStatusUnbound = "unbound"

// Op phases, as the recorder writes them.
const (
	OpPhaseStart    = "start"
	OpPhaseOK       = "ok"
	OpPhaseError    = "error"
	OpPhaseCanceled = "canceled"
)

// ChordKey identifies one unbound chord in one focus context.
type ChordKey struct {
	Context string
	Chord   string
}

// UnboundStat counts one chord's misses in one context. Removed names the
// default a user unbind override took away (#2539), so a report can tell
// "never bound" from "removed by config".
type UnboundStat struct {
	N       int
	Removed string
}

// PaletteStat is one palette mode's outcomes: picks, dismissals, and the
// dismissals split by what the box held when esc was pressed.
type PaletteStat struct {
	Picks     int
	Dismissed int
	// WithQuery / NoQuery split the dismissals by whether anything was typed.
	WithQuery int
	NoQuery   int
	// NoResults counts the fruitless searches: typed something, matched
	// nothing, gave up (query_len > 0 and results == 0; v6+ only).
	NoResults int
	// OpenMs sums how long the dismissed boxes stood open.
	OpenMs int64
}

// OpStat is one operation id's lifecycle counts and end-phase timings.
type OpStat struct {
	Started  int
	OK       int
	Errors   int
	Canceled int
	// Ended counts the end phases that carried a duration; TotalMs / MaxMs
	// are over those.
	Ended   int
	TotalMs int64
	MaxMs   int64
}

// SlowStat is one command id's dispatches that carried outcome fields: a
// failed dispatch, or one at or above CommandSlowThreshold (#2408).
type SlowStat struct {
	N       int
	Failed  int
	TotalMs int64
	MaxMs   int64
}

// UsageDay is one local calendar day's usage aggregate.
type UsageDay struct {
	// Commands maps a command id to its dispatch count per source.
	Commands map[string]map[string]int
	Unbound  map[ChordKey]*UnboundStat
	Palette  map[string]*PaletteStat
	Ops      map[string]*OpStat
	Slow     map[string]*SlowStat
}

// NewUsageDay returns an empty day bucket (exported for tests that build a
// report by hand).
func NewUsageDay() *UsageDay {
	return &UsageDay{
		Commands: map[string]map[string]int{},
		Unbound:  map[ChordKey]*UnboundStat{},
		Palette:  map[string]*PaletteStat{},
		Ops:      map[string]*OpStat{},
		Slow:     map[string]*SlowStat{},
	}
}

// usageDay returns the day bucket, creating it on the way.
func usageDay(usage map[string]*UsageDay, day string) *UsageDay {
	d := usage[day]
	if d == nil {
		d = NewUsageDay()
		usage[day] = d
	}
	return d
}

// addUsage folds one event into the day's usage aggregate. v1 files carry
// internal dispatches under the command type, so those are filtered on
// source (see the wiki's version history).
func addUsage(d *UsageDay, ev *Event) {
	switch ev.Type {
	case TypeCommand:
		id := ev.Data["id"]
		src := ev.Data["source"]
		if id == "" || src == SourceInternal {
			return
		}
		bySrc := d.Commands[id]
		if bySrc == nil {
			bySrc = map[string]int{}
			d.Commands[id] = bySrc
		}
		bySrc[src]++
		ms, hasMs := parseMs(ev.Data["ms"])
		failed := ev.Data["ok"] == "false"
		if !hasMs && !failed {
			return
		}
		s := d.Slow[id]
		if s == nil {
			s = &SlowStat{}
			d.Slow[id] = s
		}
		s.N++
		if failed {
			s.Failed++
		}
		s.TotalMs += ms
		if ms > s.MaxMs {
			s.MaxMs = ms
		}
	case TypeKey:
		if ev.Data["status"] != KeyStatusUnbound {
			return
		}
		chord := ev.Data["chord"]
		if chord == "" {
			return
		}
		k := ChordKey{Context: ev.Data["context"], Chord: chord}
		s := d.Unbound[k]
		if s == nil {
			s = &UnboundStat{}
			d.Unbound[k] = s
		}
		s.N++
		if cmd := ev.Data["command"]; cmd != "" {
			s.Removed = cmd
		}
	case TypePalettePick:
		paletteStat(d, ev.Data["mode"]).Picks++
	case TypePaletteDismiss:
		p := paletteStat(d, ev.Data["mode"])
		p.Dismissed++
		qlen, _ := strconv.Atoi(ev.Data["query_len"])
		if qlen > 0 {
			p.WithQuery++
			if res, ok := ev.Data["results"]; ok && res == "0" {
				p.NoResults++
			}
		} else {
			p.NoQuery++
		}
		if ms, ok := parseMs(ev.Data["ms"]); ok {
			p.OpenMs += ms
		}
	case TypeOp:
		id := ev.Data["id"]
		if id == "" {
			return
		}
		s := d.Ops[id]
		if s == nil {
			s = &OpStat{}
			d.Ops[id] = s
		}
		switch ev.Data["phase"] {
		case OpPhaseStart:
			s.Started++
			return
		case OpPhaseOK:
			s.OK++
		case OpPhaseError:
			s.Errors++
		case OpPhaseCanceled:
			s.Canceled++
		default:
			// The project.switch "lsp" warm-up phase (#2492) and any future
			// extra phase: neither a start nor an end of the transaction.
			return
		}
		if ms, ok := parseMs(ev.Data["ms"]); ok {
			s.Ended++
			s.TotalMs += ms
			if ms > s.MaxMs {
				s.MaxMs = ms
			}
		}
	}
}

// paletteStat returns the mode's bucket, creating it on the way.
func paletteStat(d *UsageDay, mode string) *PaletteStat {
	p := d.Palette[mode]
	if p == nil {
		p = &PaletteStat{}
		d.Palette[mode] = p
	}
	return p
}

// parseMs reads a millisecond field; a missing or malformed one reports
// !ok rather than zero, since "not recorded" is a different answer.
func parseMs(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil || ms < 0 {
		return 0, false
	}
	return ms, true
}

// mergeUsage folds one file's usage days into the report.
func mergeUsage(rep *Report, usage map[string]*UsageDay) {
	for day, src := range usage {
		mergeUsageDay(usageDay(rep.Usage, day), src)
	}
}

// mergeUsageDay folds src into dst.
func mergeUsageDay(dst, src *UsageDay) {
	for id, bySrc := range src.Commands {
		m := dst.Commands[id]
		if m == nil {
			m = map[string]int{}
			dst.Commands[id] = m
		}
		for s, n := range bySrc {
			m[s] += n
		}
	}
	for k, s := range src.Unbound {
		u := dst.Unbound[k]
		if u == nil {
			u = &UnboundStat{}
			dst.Unbound[k] = u
		}
		u.N += s.N
		if s.Removed != "" {
			u.Removed = s.Removed
		}
	}
	for mode, s := range src.Palette {
		p := paletteStat(dst, mode)
		p.Picks += s.Picks
		p.Dismissed += s.Dismissed
		p.WithQuery += s.WithQuery
		p.NoQuery += s.NoQuery
		p.NoResults += s.NoResults
		p.OpenMs += s.OpenMs
	}
	for id, s := range src.Ops {
		o := dst.Ops[id]
		if o == nil {
			o = &OpStat{}
			dst.Ops[id] = o
		}
		o.Started += s.Started
		o.OK += s.OK
		o.Errors += s.Errors
		o.Canceled += s.Canceled
		o.Ended += s.Ended
		o.TotalMs += s.TotalMs
		if s.MaxMs > o.MaxMs {
			o.MaxMs = s.MaxMs
		}
	}
	for id, s := range src.Slow {
		o := dst.Slow[id]
		if o == nil {
			o = &SlowStat{}
			dst.Slow[id] = o
		}
		o.N += s.N
		o.Failed += s.Failed
		o.TotalMs += s.TotalMs
		if s.MaxMs > o.MaxMs {
			o.MaxMs = s.MaxMs
		}
	}
}

// CommandUsage is one command id over a range, split by dispatch source.
type CommandUsage struct {
	ID      string
	N       int
	Keybind int
	Palette int
	Menu    int
	Mouse   int
	// Other counts sources the schema does not name (a future one).
	Other int
}

// UnboundUsage is one chord that found no binding in one context.
type UnboundUsage struct {
	Context string
	Chord   string
	N       int
	// Removed names the default a user override unbound, or is empty.
	Removed string
}

// PaletteUsage is one palette mode's dismissal picture over a range.
type PaletteUsage struct {
	Mode      string
	Opens     int // picks + dismissals
	Picks     int
	Dismissed int
	WithQuery int
	NoQuery   int
	NoResults int
	// Rate is Dismissed / Opens, 0 when nothing was opened.
	Rate float64
	// AvgOpen is the mean time a dismissed box stood open.
	AvgOpen time.Duration
}

// OpUsage is one operation id's lifecycle over a range.
type OpUsage struct {
	ID       string
	Started  int
	OK       int
	Errors   int
	Canceled int
	// Avg and Max are over the end phases that carried a duration.
	Avg time.Duration
	Max time.Duration
}

// SlowCommand is one command id whose dispatches were slow or failed.
type SlowCommand struct {
	ID     string
	N      int
	Failed int
	Avg    time.Duration
	Max    time.Duration
}

// UsageSummary is the whole usage picture over a date range: every slice
// most-frequent (or slowest) first, ties broken by name so equal reports
// compare stably.
type UsageSummary struct {
	Commands []CommandUsage
	Unbound  []UnboundUsage
	Palette  []PaletteUsage
	Ops      []OpUsage
	Slow     []SlowCommand
}

// Empty reports whether the range holds nothing at all.
func (s UsageSummary) Empty() bool {
	return len(s.Commands) == 0 && len(s.Unbound) == 0 && len(s.Palette) == 0 &&
		len(s.Ops) == 0 && len(s.Slow) == 0
}

// UsageRange aggregates the usage over the inclusive day range [from, to].
func (r *Report) UsageRange(from, to time.Time) UsageSummary {
	var out UsageSummary
	if r == nil {
		return out
	}
	acc := NewUsageDay()
	for _, day := range dayKeys(from, to) {
		if d := r.Usage[day]; d != nil {
			mergeUsageDay(acc, d)
		}
	}
	for id, bySrc := range acc.Commands {
		c := CommandUsage{ID: id}
		for src, n := range bySrc {
			c.N += n
			switch src {
			case SourceKeybind:
				c.Keybind += n
			case SourcePalette:
				c.Palette += n
			case SourceMenu:
				c.Menu += n
			case SourceMouse:
				c.Mouse += n
			default:
				c.Other += n
			}
		}
		out.Commands = append(out.Commands, c)
	}
	sort.Slice(out.Commands, func(i, j int) bool {
		if out.Commands[i].N != out.Commands[j].N {
			return out.Commands[i].N > out.Commands[j].N
		}
		return out.Commands[i].ID < out.Commands[j].ID
	})
	for k, s := range acc.Unbound {
		out.Unbound = append(out.Unbound, UnboundUsage{Context: k.Context, Chord: k.Chord, N: s.N, Removed: s.Removed})
	}
	sort.Slice(out.Unbound, func(i, j int) bool {
		a, b := out.Unbound[i], out.Unbound[j]
		if a.N != b.N {
			return a.N > b.N
		}
		if a.Context != b.Context {
			return a.Context < b.Context
		}
		return a.Chord < b.Chord
	})
	for mode, s := range acc.Palette {
		p := PaletteUsage{Mode: mode, Opens: s.Picks + s.Dismissed, Picks: s.Picks, Dismissed: s.Dismissed,
			WithQuery: s.WithQuery, NoQuery: s.NoQuery, NoResults: s.NoResults}
		if p.Opens > 0 {
			p.Rate = float64(p.Dismissed) / float64(p.Opens)
		}
		if s.Dismissed > 0 {
			p.AvgOpen = time.Duration(s.OpenMs/int64(s.Dismissed)) * time.Millisecond
		}
		out.Palette = append(out.Palette, p)
	}
	sort.Slice(out.Palette, func(i, j int) bool {
		a, b := out.Palette[i], out.Palette[j]
		if a.Dismissed != b.Dismissed {
			return a.Dismissed > b.Dismissed
		}
		return a.Mode < b.Mode
	})
	for id, s := range acc.Ops {
		o := OpUsage{ID: id, Started: s.Started, OK: s.OK, Errors: s.Errors, Canceled: s.Canceled,
			Max: time.Duration(s.MaxMs) * time.Millisecond}
		if s.Ended > 0 {
			o.Avg = time.Duration(s.TotalMs/int64(s.Ended)) * time.Millisecond
		}
		out.Ops = append(out.Ops, o)
	}
	sort.Slice(out.Ops, func(i, j int) bool {
		a, b := out.Ops[i], out.Ops[j]
		if a.Max != b.Max {
			return a.Max > b.Max
		}
		return a.ID < b.ID
	})
	for id, s := range acc.Slow {
		c := SlowCommand{ID: id, N: s.N, Failed: s.Failed, Max: time.Duration(s.MaxMs) * time.Millisecond}
		if s.N > 0 {
			c.Avg = time.Duration(s.TotalMs/int64(s.N)) * time.Millisecond
		}
		out.Slow = append(out.Slow, c)
	}
	sort.Slice(out.Slow, func(i, j int) bool {
		a, b := out.Slow[i], out.Slow[j]
		if a.N != b.N {
			return a.N > b.N
		}
		return a.ID < b.ID
	})
	return out
}

// FormatMs renders a duration for a latency column: "12ms", "1.4s", "2m 3s".
func FormatMs(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		return strconv.FormatInt(int64(d/time.Millisecond), 10) + "ms"
	case d < time.Minute:
		return strconv.FormatFloat(float64(d)/float64(time.Second), 'f', 1, 64) + "s"
	}
	return FormatDuration(d)
}
