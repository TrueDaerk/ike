package keymap

// probesession.go is the shared probing engine (#2080): the raw-key matching
// `cmd/keyprobe` pioneered — direct hits, the shifted-chord collapse rule,
// mouse nav buttons as ordinary targets — lifted into the package so the
// in-app keymap doctor and the standalone binary run the identical session.

// ProbeSession tracks one probe run over a fixed target set. Matching is
// order-independent: any delivered chord is recorded against whichever
// target it satisfies, whenever it arrives.
type ProbeSession struct {
	targets []string
	hit     map[string]string // target chord -> what arrived ("" until seen)
	skipped map[string]bool
	last    string
}

// NewProbeSession starts a session over the given targets (normally
// ProbeTargets()).
func NewProbeSession(targets []string) *ProbeSession {
	s := &ProbeSession{targets: targets, hit: make(map[string]string, len(targets)), skipped: map[string]bool{}}
	for _, t := range targets {
		s.hit[t] = ""
	}
	return s
}

// HandleKey records one arrived key (keyboard chord step or a mouse nav
// button converted through FromMouseButton) against the targets.
func (s *ProbeSession) HandleKey(k Key) {
	s.last = k.String()
	if _, want := s.hit[s.last]; want {
		s.hit[s.last] = s.last
	}
	// Collapse evidence: a shifted chord arriving as its unshifted twin (the
	// classic ctrl+shift+z → ctrl+z) is recorded against the shifted target —
	// in addition to any direct hit, since the receiver cannot tell them apart.
	for _, t := range s.targets {
		if t != s.last && s.hit[t] == "" && shiftStripped(t) == s.last {
			s.hit[t] = s.last
		}
	}
}

// shiftStripped removes the first "shift+" from a chord string.
func shiftStripped(chord string) string {
	const mod = "shift+"
	for i := 0; i+len(mod) <= len(chord); i++ {
		if chord[i:i+len(mod)] == mod {
			return chord[:i] + chord[i+len(mod):]
		}
	}
	return chord
}

// SkipNext marks the first still-pending target as skipped and returns its
// chord ("" when nothing is pending). A skipped target yields no verdict at
// all — unlike "missing", which asserts the chord was tried and never arrived.
func (s *ProbeSession) SkipNext() string {
	t := s.Next()
	if t != "" {
		s.skipped[t] = true
	}
	return t
}

// Next returns the first target that is neither hit nor skipped ("" when the
// run is exhausted) — the natural cursor for the doctor's highlight.
func (s *ProbeSession) Next() string {
	for _, t := range s.targets {
		if s.hit[t] == "" && !s.skipped[t] {
			return t
		}
	}
	return ""
}

// Targets returns the session's target chords in list order.
func (s *ProbeSession) Targets() []string { return s.targets }

// Hit returns what arrived for a target: the target itself on a direct hit,
// another key on a collapse, "" while unseen.
func (s *ProbeSession) Hit(target string) string { return s.hit[target] }

// Skipped reports whether the target was skipped.
func (s *ProbeSession) Skipped(target string) bool { return s.skipped[target] }

// Last returns the most recently arrived key ("" before the first).
func (s *ProbeSession) Last() string { return s.last }

// Counts summarises progress: delivered (direct hits), collapsed, skipped and
// still-pending targets.
func (s *ProbeSession) Counts() (delivered, collapsed, skipped, pending int) {
	for _, t := range s.targets {
		switch got := s.hit[t]; {
		case got == t:
			delivered++
		case got != "":
			collapsed++
		case s.skipped[t]:
			skipped++
		default:
			pending++
		}
	}
	return
}

// Results renders the session as probe verdicts, in target order. Skipped
// targets are excluded — the run makes no claim about them. An unpressed,
// unskipped target is reported missing, exactly like `cmd/keyprobe` on
// ctrl+d: finishing asserts every listed chord was tried.
func (s *ProbeSession) Results() []ProbeResult {
	out := make([]ProbeResult, 0, len(s.targets))
	for _, t := range s.targets {
		if s.skipped[t] {
			continue
		}
		r := ProbeResult{Chord: t, Delivered: s.hit[t] == t}
		if got := s.hit[t]; got != "" && got != t {
			r.Got = got
		}
		out = append(out, r)
	}
	return out
}
