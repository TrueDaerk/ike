package keymap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// probestore.go persists keymap-doctor runs (#2080) as per-terminal
// reachability overrides: one verdict set per terminal identity, stored in
// the user config dir, installed into the classifier at startup so
// Classify/ReachabilityNote prefer probed truth over the static table.

// TerminalProbe is one terminal's stored probe run.
type TerminalProbe struct {
	// Probed is when the run finished (RFC 3339), informational only.
	Probed  string        `json:"probed,omitempty"`
	Results []ProbeResult `json:"results"`
}

// ProbeStore is the on-disk override set, keyed by TerminalID.
type ProbeStore struct {
	Terminals map[string]TerminalProbe `json:"terminals"`
}

// TerminalID derives the store key for the running terminal. tmux comes
// first: it swallows and rewrites chords regardless of the outer emulator,
// so a run inside tmux must never be attributed to (or read back in) the
// bare terminal. Outside tmux the emulator identity ($TERM_PROGRAM) beats
// the generic $TERM.
func TerminalID(getenv func(string) string) string {
	if getenv("TMUX") != "" {
		return "tmux"
	}
	if p := getenv("TERM_PROGRAM"); p != "" {
		return p
	}
	if t := getenv("TERM"); t != "" {
		return t
	}
	return "unknown"
}

// ProbeStorePath is the store file location: the same seam every other
// user-scoped state file uses ($IKE_CONFIG_DIR, else ~/.ike).
func ProbeStorePath() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "keyprobe.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "keyprobe.json")
}

// LoadProbeStore reads the store; a missing, empty or malformed file yields
// an empty store (state files never block startup).
func LoadProbeStore(path string) ProbeStore {
	st := ProbeStore{Terminals: map[string]TerminalProbe{}}
	if path == "" {
		return st
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, &st); err != nil || st.Terminals == nil {
		st.Terminals = map[string]TerminalProbe{}
	}
	return st
}

// Save writes the store atomically-enough for state files (write then rename
// is overkill here; a torn file just reads back empty).
func (s ProbeStore) Save(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Set replaces one terminal's run. Results are stored sorted by chord for a
// stable file.
func (s *ProbeStore) Set(terminal, probed string, results []ProbeResult) {
	if s.Terminals == nil {
		s.Terminals = map[string]TerminalProbe{}
	}
	rs := append([]ProbeResult(nil), results...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].Chord < rs[j].Chord })
	s.Terminals[terminal] = TerminalProbe{Probed: probed, Results: rs}
}

// Clear drops one terminal's run.
func (s *ProbeStore) Clear(terminal string) { delete(s.Terminals, terminal) }

// Results returns the stored verdicts for a terminal (nil when none).
func (s ProbeStore) Results(terminal string) []ProbeResult {
	return s.Terminals[terminal].Results
}

// TerminalIDs lists the terminals with stored runs, sorted.
func (s ProbeStore) TerminalIDs() []string {
	out := make([]string, 0, len(s.Terminals))
	for t := range s.Terminals {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// probeVerdicts is the installed per-terminal truth, keyed by chord string.
// Like GOOS it is set once at startup (and after a doctor run) from the tea
// loop, so it is unguarded by design.
var probeVerdicts map[string]ProbeResult

// SetProbeVerdicts installs the running terminal's probe verdicts;
// Classify and ReachabilityNote consult them before the static rules.
// nil (or empty) clears the installation.
func SetProbeVerdicts(results []ProbeResult) {
	if len(results) == 0 {
		probeVerdicts = nil
		return
	}
	m := make(map[string]ProbeResult, len(results))
	for _, r := range results {
		m[r.Chord] = r
	}
	probeVerdicts = m
}

// ProbeVerdict returns the installed verdict for a single chord-step string.
func ProbeVerdict(chord string) (ProbeResult, bool) {
	r, ok := probeVerdicts[chord]
	return r, ok
}

// ChordProbedMissing reports whether any step of the chord probed missing in
// this terminal, with the collapse evidence when there is any — the signal
// the settings list flags prominently (#2080).
func ChordProbedMissing(c Chord) (got string, missing bool) {
	if r, ok := probeVerdicts[c.String()]; ok && !r.Delivered {
		return r.Got, true
	}
	for _, k := range c.Steps {
		if r, ok := probeVerdicts[k.String()]; ok && !r.Delivered {
			return r.Got, true
		}
	}
	return "", false
}
