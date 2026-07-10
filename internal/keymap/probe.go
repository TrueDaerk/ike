package keymap

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
)

// probe.go is the report format of the terminal reality probe (0081/10,
// cmd/keyprobe): the probe prints one machine-parseable line per default
// chord, and ParseProbeReport reads them back so a captured run can be turned
// into reachability overrides (or asserted in tests).

// ProbeResult is one chord's outcome in a probe run.
type ProbeResult struct {
	Chord     string
	Delivered bool
	// Got is what the terminal actually sent when it differs from the chord
	// (e.g. ctrl+shift+z arriving as ctrl+z) — the collapse evidence.
	Got string
}

// probePrefix marks report lines; anything else in the output is UI noise.
const probePrefix = "PROBE\t"

// FormatProbeResult renders one report line: PROBE\t<chord>\t<delivered|missing>[\tgot=<key>].
func FormatProbeResult(r ProbeResult) string {
	state := "missing"
	if r.Delivered {
		state = "delivered"
	}
	line := probePrefix + r.Chord + "\t" + state
	if r.Got != "" {
		line += "\tgot=" + r.Got
	}
	return line
}

// ParseProbeReport reads a probe run's output (interleaved with any UI
// noise) into results, sorted by chord for determinism.
func ParseProbeReport(r io.Reader) ([]ProbeResult, error) {
	var out []ProbeResult
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, probePrefix) {
			continue
		}
		fields := strings.Split(strings.TrimPrefix(line, probePrefix), "\t")
		if len(fields) < 2 {
			return nil, fmt.Errorf("probe: malformed line %q", line)
		}
		res := ProbeResult{Chord: fields[0], Delivered: fields[1] == "delivered"}
		for _, f := range fields[2:] {
			if v, ok := strings.CutPrefix(f, "got="); ok {
				res.Got = v
			}
		}
		out = append(out, res)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chord < out[j].Chord })
	return out, nil
}

// ProbeTargets lists the distinct single-step default chords the probe asks
// for, sorted. Multi-step chords are exercised implicitly through their first
// step; bare-modifier chords are skipped (undetectable by construction).
func ProbeTargets() []string {
	seen := map[string]bool{}
	for _, b := range Defaults(PresetJetBrains) {
		for _, k := range b.Chord.Steps {
			if bareModifiers[k.Base] {
				continue
			}
			seen[k.String()] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
