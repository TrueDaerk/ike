package keymap

// reachability.go is the ground-truth table of Roadmap 0081/10: per chord,
// does the terminal actually deliver it to a TUI? Downstream work keys off
// these classes — default primaries are picked from them (#14), the
// discoverability layer (#15) labels fragile chords honestly, and the status
// matrix (#16) reports them. Terminal truth beats aspiration: the classes
// come from the probe (cmd/keyprobe) run against real terminals, plus the
// documented platform rules below.

// Reachability classifies whether a chord reaches the program.
type Reachability int

const (
	// Delivered chords arrive in every mainstream terminal without special
	// configuration: plain keys, ctrl+letter, function keys, shift+fN.
	Delivered Reachability = iota
	// Fragile chords depend on the terminal, its configuration or an
	// extended keyboard protocol: Cmd-modified keys (OS/terminal menus
	// intercept many; the rest need the Kitty protocol), alt-modified keys
	// (option-as-meta), ctrl+tab (terminal-eaten — tmux never sends it),
	// ctrl+shift+letter (indistinguishable from ctrl+letter without the
	// Kitty protocol's disambiguation).
	Fragile
	// Undetectable chords cannot be seen at all with key-press events only:
	// bare-modifier taps ("shift shift") need key-up reporting.
	Undetectable
)

// String renders the class for reports and the status matrix.
func (r Reachability) String() string {
	switch r {
	case Fragile:
		return "fragile"
	case Undetectable:
		return "undetectable"
	}
	return "delivered"
}

// reachabilityOverrides pins chords whose class the general rules cannot
// derive — the probe's empirical findings land here.
var reachabilityOverrides = map[string]Reachability{
	// tmux (and several terminal emulators) consume ctrl+tab for their own
	// tab switching and never forward it; probed 2026-07 (tmux 3.x: not
	// delivered even with the Kitty protocol announced by the client).
	"ctrl+tab": Fragile,
}

// bareModifiers are key bases that are themselves modifiers; a chord step on
// one of them (the "shift shift" double-tap) needs key-up events.
var bareModifiers = map[string]bool{"shift": true, "ctrl": true, "alt": true, "cmd": true, "meta": true}

// Classify reports the reachability class of a chord under the documented
// terminal rules and the probe's overrides. Multi-step chords take the worst
// class of their steps (a chord is only as reachable as its weakest key).
func Classify(c Chord) Reachability {
	if r, ok := reachabilityOverrides[c.String()]; ok {
		return r
	}
	worst := Delivered
	for _, k := range c.Steps {
		if r := classifyKey(k); r > worst {
			worst = r
		}
	}
	return worst
}

// classifyKey applies the single-key rules.
func classifyKey(k Key) Reachability {
	if bareModifiers[k.Base] {
		return Undetectable
	}
	if r, ok := reachabilityOverrides[k.String()]; ok {
		return r
	}
	switch {
	case k.Has(ModMeta):
		// Cmd chords: macOS terminals forward them only with the Kitty
		// keyboard protocol, and the OS/terminal menu intercepts several
		// (cmd+q/w/t/…) regardless.
		return Fragile
	case k.Has(ModAlt):
		// Alt chords need option-as-meta (macOS) or an emulator that encodes
		// them; delivery is configuration-dependent.
		return Fragile
	case k.Has(ModCtrl) && k.Has(ModShift) && !csiParamEncoded(k.Base):
		// ctrl+shift+letter collapses onto ctrl+letter without the Kitty
		// protocol's shifted-key disambiguation. CSI-parameter keys are
		// exempt: their legacy encoding carries the modifier bitset
		// (CSI 5;6~ for ctrl+shift+pgup), so no collapse happens.
		return Fragile
	}
	return Delivered
}

// csiParamEncoded reports whether a base key is transmitted as a CSI (or SS3)
// sequence with a modifier parameter in the legacy encoding — arrows,
// home/end, page keys, insert/delete and the function keys. Modifiers on
// these keys arrive distinguishably in every mainstream terminal, unlike
// modifiers on character keys (which need the Kitty protocol) or on the
// C0-mapped keys (enter, tab, space, esc, backspace).
func csiParamEncoded(base string) bool {
	switch base {
	case "up", "down", "left", "right", "home", "end", "pgup", "pgdown", "insert", "delete":
		return true
	}
	if len(base) >= 2 && base[0] == 'f' {
		for _, c := range base[1:] {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	return false
}

// ReachabilityNote explains a non-delivered class in one phrase, for honest
// labelling (#15) and the status matrix (#16).
func ReachabilityNote(c Chord) string {
	switch {
	case Classify(c) == Undetectable:
		return "needs key-up events (bare modifier tap)"
	case c.String() == "ctrl+tab":
		return "terminal-eaten (tmux and friends never forward it)"
	}
	for _, k := range c.Steps {
		switch {
		case k.Has(ModMeta):
			return "Cmd needs the Kitty keyboard protocol; the OS/terminal menu intercepts several"
		case k.Has(ModAlt):
			return "needs option-as-meta / meta-encoding in the terminal"
		case k.Has(ModCtrl) && k.Has(ModShift) && !csiParamEncoded(k.Base):
			return "collapses onto the unshifted ctrl chord without the Kitty protocol"
		}
	}
	return ""
}

// ReachabilityReport lists every distinct default chord with its class, in
// table order — the persisted ground-truth view (wiki + status matrix).
func ReachabilityReport() []struct {
	Chord string
	Class Reachability
	Note  string
} {
	seen := map[string]bool{}
	var out []struct {
		Chord string
		Class Reachability
		Note  string
	}
	for _, b := range Defaults(PresetJetBrains) {
		s := b.Chord.String()
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, struct {
			Chord string
			Class Reachability
			Note  string
		}{s, Classify(b.Chord), ReachabilityNote(b.Chord)})
	}
	return out
}
