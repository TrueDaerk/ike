package keymap

import (
	"strings"
	"unicode"
)

// deadchords.go sharpens the reachability classes into an actionable verdict
// for the running setup (#2161). Classify answers "is this chord terminal-
// dependent?"; the keymap doctor's dead-binding report needs the next
// question answered: on *this* platform, in *this* terminal, does the chord
// reach the program at all? The Fragile class lumps together chords that
// merely need a setting (cmd+* on a Kitty-protocol terminal) with chords that
// are certainly swallowed here (plain ctrl+F-keys on macOS), and only the
// second kind is worth rebinding proactively.
//
// Probed truth wins over every rule below, in both directions: a chord that
// probed missing is Dead with the collapse evidence, and a chord that probed
// delivered is never called dead however grim the platform rules look.

// Deliverability is a chord's verdict for one platform/terminal pair.
type Deliverability int

const (
	// Live chords reach the program here: Delivered by the static rules, or
	// probed delivered in this terminal.
	Live Deliverability = iota
	// AtRisk chords are terminal-, configuration- or protocol-dependent
	// (the Fragile class) without a known-dead pattern matching: they may
	// well work, and they may silently not.
	AtRisk
	// Dead chords are known not to arrive in this setup — probed missing, a
	// documented platform interception, or a terminal that consumes them.
	Dead
)

// String renders the verdict for reports.
func (d Deliverability) String() string {
	switch d {
	case AtRisk:
		return "at risk"
	case Dead:
		return "dead"
	}
	return "live"
}

// TerminalEnv is the setup a verdict is derived for: the platform IKE was
// built for and the terminal identity the probe store is keyed by.
type TerminalEnv struct {
	GOOS     string
	Terminal string
}

// DetectTerminalEnv reads the running setup — the same terminal identity the
// probe store uses, so a stored run and a static verdict talk about the same
// terminal.
func DetectTerminalEnv(getenv func(string) string) TerminalEnv {
	return TerminalEnv{GOOS: GOOS, Terminal: TerminalID(getenv)}
}

// optionComposingTerminals are the macOS terminals whose *default* setting
// composes the Option layer of the keyboard layout into text (opt+b → "∫")
// instead of sending a meta-prefixed or Kitty-encoded chord. macOS applies
// that layer before any escape sequence exists, so an alt chord on a
// character key never reaches the program until the user flips the terminal's
// option-as-alt setting (see the keybindings wiki). tmux is deliberately
// absent: the outer emulator decides, and the identity does not say which.
var optionComposingTerminals = map[string]bool{
	"apple_terminal": true, // Terminal.app
	"iterm.app":      true,
	"ghostty":        true,
	"kitty":          true,
	"wezterm":        true,
	"alacritty":      true,
	"vscode":         true,
}

// ComposesOption reports whether this terminal swallows alt chords on
// character keys by default.
func (e TerminalEnv) ComposesOption() bool {
	return e.GOOS == "darwin" && optionComposingTerminals[strings.ToLower(e.Terminal)]
}

// Deliverability judges a chord for this setup and explains a non-Live
// verdict in one phrase. A multi-step chord takes the worst verdict of its
// steps: it is only as reachable as its weakest key.
func (e TerminalEnv) Deliverability(c Chord) (Deliverability, string) {
	if got, missing := ChordProbedMissing(c); missing {
		if got != "" {
			return Dead, "probed missing in this terminal (arrives as " + got + ")"
		}
		return Dead, "probed missing in this terminal"
	}
	if Classify(c) == Undetectable {
		return Dead, ReachabilityNote(c)
	}
	for _, k := range c.Steps {
		if _, probed := ProbeVerdict(k.String()); probed {
			continue // probed delivered: measured reality beats the rules
		}
		if why, dead := e.deadKey(k); dead {
			return Dead, why
		}
	}
	if Classify(c) != Delivered {
		return AtRisk, ReachabilityNote(c)
	}
	return Live, ""
}

// deadKey applies the known-dead patterns to a single step. Every pattern
// here is a chord the static table already calls Fragile — this only says
// which of those fragile chords are certainly gone in this setup.
func (e TerminalEnv) deadKey(k Key) (string, bool) {
	switch {
	case e.GOOS == "darwin" && k.Mods == ModCtrl && isFunctionKey(k.Base):
		// macOS system shortcuts (ctrl+F2 "move focus to menu bar", …) claim
		// the plain ctrl+F-keys before the terminal sees them (#1374).
		return "macOS system shortcuts swallow plain ctrl+F-keys before the terminal sees them", true
	case strings.EqualFold(e.Terminal, "tmux") && k.Mods == ModCtrl && k.Base == "tab":
		// tmux consumes ctrl+tab for its own tab switching and never
		// forwards it, whatever the outer emulator would have sent.
		return "tmux consumes ctrl+tab and never forwards it", true
	case e.ComposesOption() && composedByOption(k):
		return e.Terminal + " composes Option into text by default — " + k.String() +
			" arrives as a character, not a chord (enable option-as-alt)", true
	}
	return "", false
}

// composedByOption reports whether the Option layer eats this key: an alt
// chord on a printable character key, without Cmd (Cmd suppresses the Option
// layer and rides the Kitty protocol instead). CSI-parameter-encoded keys —
// arrows, nav keys, function keys — carry their modifier bitset in the legacy
// encoding and arrive regardless of the option-as-alt setting.
func composedByOption(k Key) bool {
	if !k.Has(ModAlt) || k.Has(ModMeta) || csiParamEncoded(k.Base) {
		return false
	}
	r := []rune(k.Base)
	return len(r) == 1 && unicode.IsPrint(r[0])
}
