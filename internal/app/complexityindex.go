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
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				globs = append(globs, p)
			}
		}
		def.Exclude = globs
	}
	return def
}

// complexityOptions maps the typed [complexity] section to the index's
// options.
func complexityOptions(c config.Complexity) complexity.Options {
	return complexity.Options{Enabled: c.Enabled, Exclude: append([]string(nil), c.Exclude...)}
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
		p.SetWarn(cfg.Complexity.CCNWarn, cfg.Complexity.CognitiveWarn, cfg.Complexity.CommentDensityWarn)
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
// file and function counts it left behind and whether it stopped at the
// file cap. The phase is error when the build has no syntax tree at all
// (the walk can never fill), ok otherwise. It runs on the index's timer
// goroutine, so it touches nothing but the recorder. No path travels.
func complexityScanRecorder(r *telemetry.Recorder) func(complexity.Stats) {
	return func(s complexity.Stats) {
		phase := telemetry.OpPhaseOK
		if s.Unavailable {
			phase = telemetry.OpPhaseError
		}
		r.Op(telemetry.OpComplexityScan, phase, map[string]string{
			"ms":        strconv.FormatInt(s.LastScan.Milliseconds(), 10),
			"files":     strconv.Itoa(s.Files),
			"functions": strconv.Itoa(s.Functions),
			"truncated": strconv.FormatBool(s.Truncated),
		})
	}
}
