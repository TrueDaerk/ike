package app

// complexityindex.go wires the project complexity index (Roadmap 0550,
// #2916) into the app: one index per project root, built beside the PHP
// declaration index in buildModel, walked from Init like the TODO index,
// re-measured from buffer saves and watcher events, reconfigured live from
// the [complexity] section on every config reload, and exposed to the
// epic's later surfaces (tool window, Problems feed, status segment)
// through ComplexityIndex. Measuring runs on the walk's goroutines; nothing
// in Update parses.

import (
	"strconv"
	"strings"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/telemetry"
)

// ComplexityChangedMsg says the complexity index reached a new generation:
// the walk finished, a saved or externally changed file was re-measured, or
// the index was dropped or rebuilt. Gen is the generation the next Snapshot
// carries; the index debounces, so a burst of changes arrives as one
// message. The surfaces of the later steps re-read Snapshot on it.
type ComplexityChangedMsg struct{ Gen int }

// ComplexityIndex returns the project's complexity index (never nil; a
// disabled index answers an empty snapshot and reports Enabled false).
func (m Model) ComplexityIndex() *complexity.Index { return m.complexityIndex }

// complexityOptionsFrom reads the [complexity] keys off the flat host config
// the model is built from — the typed global may not be published yet at
// construction — falling back to the schema defaults for a missing or
// malformed value. A flat list value is comma-separated.
func complexityOptionsFrom(cfg host.Config) complexity.Options {
	def := complexityOptions(config.Get().Complexity)
	if cfg == nil {
		return def
	}
	if v, ok := cfg.Get("complexity.enabled"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			def.Enabled = b
		}
	}
	if v, ok := cfg.Get("complexity.exclude"); ok {
		var globs []string
		for _, p := range splitGlobList(v) {
			if p = strings.TrimSpace(p); p != "" {
				globs = append(globs, p)
			}
		}
		def.Exclude = globs
	}
	return def
}

// splitGlobList splits a flat comma-joined glob list at the commas outside
// a {a,b} alternation (#3085): `**/*.{js,ts}` stays one glob, so the list
// read at construction equals the typed one a config reload hands
// Reconfigure — a mismatch there would rebuild the index cold on the first
// reload of an unrelated setting.
func splitGlobList(v string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range v {
		switch r {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, v[start:i])
				start = i + 1
			}
		}
	}
	return append(out, v[start:])
}

// complexityOptions maps the typed [complexity] section to the index's
// options.
func complexityOptions(c config.Complexity) complexity.Options {
	return complexity.Options{Enabled: c.Enabled, Exclude: append([]string(nil), c.Exclude...)}
}

// complexityRoot is the root complexity paths are project-relative to: the
// index's, "" without an index.
func (m *Model) complexityRoot() string {
	if m.complexityIndex == nil {
		return ""
	}
	return m.complexityIndex.Root()
}

// complexityResolver resolves a file's warning thresholds under c (#2960):
// the first complexity.overrides entry matching it, the globals otherwise.
// Every threshold consumer — the panel, the hints, the Problems feed, the
// Changes diff and the status segment — judges through one.
func (m *Model) complexityResolver(c config.Complexity) complexity.Resolver {
	return complexity.ResolverFor(c, m.complexityRoot())
}

// setComplexityPanelThresholds hands the tool window c's thresholds: the
// globals, and the resolver while there are overrides.
func (m *Model) setComplexityPanelThresholds(p *complexitypanel.Model, c config.Complexity) {
	var res complexity.Resolver
	if len(c.ThresholdOverrides()) > 0 {
		res = m.complexityResolver(c)
	}
	p.SetThresholds(complexity.Global(c), res)
}

// complexityThresholdsFor is path's resolved thresholds under c.
func (m *Model) complexityThresholdsFor(c config.Complexity, path string) complexity.Thresholds {
	return complexity.ThresholdsFor(c, m.complexityRoot(), canonicalPath(path))
}

// reconfigureComplexityIndex applies a reloaded [complexity] section live:
// the master switch drops or rebuilds the index, a changed exclude list
// rebuilds the walk.
func (m *Model) reconfigureComplexityIndex(cfg *config.Config) {
	if m.complexityIndex == nil || cfg == nil {
		return
	}
	m.complexityIndex.Reconfigure(complexityOptions(cfg.Complexity))
	// The tool window's warning thresholds follow the same reload (#2917).
	if p := m.complexityPanel(); p != nil {
		m.setComplexityPanelThresholds(p, cfg.Complexity)
	}
	// So do the Problems feed's rows (#2920): a moved threshold adds or
	// drops rows, a switched-off feed or index clears them — and the
	// baseline in effect (#2958): complexity.baseline off hands every
	// surface nil, on hands them the loaded file again.
	m.applyComplexityBaseline()
}

// complexityScanRecorder is the callback the complexity index reports a
// completed project walk through: one complexity.scan op event per walk —
// the initial one and every rebuild — carrying the walk's duration, the
// file and function counts it left behind, how many files it took from the
// persisted cache (#2967) and whether it stopped at the file cap; a damaged
// cache adds cache=corrupt, the one line the ignored file leaves behind. The phase is error when the build has no syntax tree at all
// (the walk can never fill), ok otherwise. It runs on the index's timer
// goroutine, so it touches nothing but the recorder. No path travels.
func complexityScanRecorder(r *telemetry.Recorder) func(complexity.Stats) {
	return func(s complexity.Stats) {
		phase := telemetry.OpPhaseOK
		if s.Unavailable {
			phase = telemetry.OpPhaseError
		}
		detail := map[string]string{
			"ms":        strconv.FormatInt(s.LastScan.Milliseconds(), 10),
			"files":     strconv.Itoa(s.Files),
			"functions": strconv.Itoa(s.Functions),
			"cached":    strconv.Itoa(s.Cached),
			"truncated": strconv.FormatBool(s.Truncated),
		}
		if s.CacheCorrupt {
			detail["cache"] = "corrupt"
		}
		r.Op(telemetry.OpComplexityScan, phase, detail)
	}
}
