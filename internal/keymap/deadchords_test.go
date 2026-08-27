package keymap

import (
	"strings"
	"testing"
)

// withGOOS runs f with the platform pinned, restoring it afterwards.
func withGOOS(t *testing.T, goos string, f func()) {
	t.Helper()
	prev := GOOS
	defer func() { GOOS = prev }()
	GOOS = goos
	f()
}

// TestDeliverabilityDarwinCtrlFunctionKeys (#2161): plain ctrl+F-keys are
// swallowed by macOS system shortcuts, so they are dead — not merely fragile —
// while the shifted variants stay live and every ctrl+fN is live off macOS.
func TestDeliverabilityDarwinCtrlFunctionKeys(t *testing.T) {
	SetProbeVerdicts(nil)
	withGOOS(t, "darwin", func() {
		env := TerminalEnv{GOOS: "darwin", Terminal: "Apple_Terminal"}
		class, why := env.Deliverability(MustParseChord("ctrl+f8"))
		if class != Dead {
			t.Fatalf("darwin ctrl+f8 = %v, want Dead", class)
		}
		if !strings.Contains(why, "macOS system shortcuts") {
			t.Fatalf("reason %q must name the interception", why)
		}
		if class, _ := env.Deliverability(MustParseChord("ctrl+shift+f10")); class != Live {
			t.Fatalf("darwin ctrl+shift+f10 = %v, want Live", class)
		}
	})
	withGOOS(t, "linux", func() {
		env := TerminalEnv{GOOS: "linux", Terminal: "xterm"}
		if class, _ := env.Deliverability(MustParseChord("ctrl+f8")); class != Live {
			t.Fatalf("linux ctrl+f8 = %v, want Live", class)
		}
	})
}

// TestDeliverabilityOptionComposition (#2161): on a macOS terminal that
// composes the Option layer by default, alt chords on character keys are
// dead — the program receives a rune, not a chord — while alt on
// CSI-parameter-encoded keys (F-keys, arrows) still arrives.
func TestDeliverabilityOptionComposition(t *testing.T) {
	SetProbeVerdicts(nil)
	withGOOS(t, "darwin", func() {
		env := TerminalEnv{GOOS: "darwin", Terminal: "Ghostty"}
		if !env.ComposesOption() {
			t.Fatal("Ghostty on darwin composes Option by default")
		}
		class, why := env.Deliverability(MustParseChord("alt+b"))
		if class != Dead {
			t.Fatalf("alt+b under composition = %v, want Dead", class)
		}
		if !strings.Contains(why, "Option") || !strings.Contains(why, "Ghostty") {
			t.Fatalf("reason %q must name the terminal and the Option layer", why)
		}
		if class, _ := env.Deliverability(MustParseChord("alt+f7")); class != AtRisk {
			t.Fatalf("alt+f7 under composition = %v, want AtRisk (CSI-parameter encoded)", class)
		}
		// Cmd suppresses the Option layer; cmd+alt chords ride the Kitty
		// protocol instead and are merely at risk.
		if class, _ := env.Deliverability(MustParseChord("cmd+alt+b")); class != AtRisk {
			t.Fatalf("cmd+alt+b under composition = %v, want AtRisk", class)
		}
		// An unknown terminal identity says nothing about its Option
		// setting: at risk, not condemned.
		unknown := TerminalEnv{GOOS: "darwin", Terminal: "unknown"}
		if class, _ := unknown.Deliverability(MustParseChord("alt+b")); class != AtRisk {
			t.Fatalf("alt+b in an unknown terminal = %v, want AtRisk", class)
		}
	})
}

// TestDeliverabilityTmuxCtrlTab (#2161): tmux eats ctrl+tab whatever hosts it.
func TestDeliverabilityTmuxCtrlTab(t *testing.T) {
	SetProbeVerdicts(nil)
	withGOOS(t, "linux", func() {
		tmux := TerminalEnv{GOOS: "linux", Terminal: "tmux"}
		class, why := tmux.Deliverability(MustParseChord("ctrl+tab"))
		if class != Dead || !strings.Contains(why, "tmux") {
			t.Fatalf("ctrl+tab in tmux = %v (%q), want Dead", class, why)
		}
		other := TerminalEnv{GOOS: "linux", Terminal: "xterm"}
		if class, _ := other.Deliverability(MustParseChord("ctrl+tab")); class != AtRisk {
			t.Fatalf("ctrl+tab outside tmux = %v, want AtRisk", class)
		}
	})
}

// TestDeliverabilityProbedTruthWins (#2161): a probe run beats every rule in
// both directions — measured delivery rescues a condemned chord, measured
// silence condemns one the rules like.
func TestDeliverabilityProbedTruthWins(t *testing.T) {
	defer SetProbeVerdicts(nil)
	withGOOS(t, "darwin", func() {
		env := TerminalEnv{GOOS: "darwin", Terminal: "Ghostty"}
		SetProbeVerdicts([]ProbeResult{
			{Chord: "alt+b", Delivered: true},
			{Chord: "ctrl+f8", Delivered: true},
			{Chord: "ctrl+s", Delivered: false, Got: "s"},
		})
		if class, _ := env.Deliverability(MustParseChord("alt+b")); class != Live {
			t.Fatalf("probed-delivered alt+b = %v, want Live", class)
		}
		if class, _ := env.Deliverability(MustParseChord("ctrl+f8")); class != Live {
			t.Fatalf("probed-delivered ctrl+f8 = %v, want Live", class)
		}
		class, why := env.Deliverability(MustParseChord("ctrl+s"))
		if class != Dead || !strings.Contains(why, "arrives as s") {
			t.Fatalf("probed-missing ctrl+s = %v (%q), want Dead with evidence", class, why)
		}
	})
}

// TestDeliverabilityUndetectable (#2161): a bare-modifier tap cannot be seen
// with key-press events only, so it is dead wherever it is bound.
func TestDeliverabilityUndetectable(t *testing.T) {
	SetProbeVerdicts(nil)
	env := TerminalEnv{GOOS: "linux", Terminal: "xterm"}
	class, why := env.Deliverability(MustParseChord("shift shift"))
	if class != Dead || !strings.Contains(why, "key-up") {
		t.Fatalf("shift shift = %v (%q), want Dead", class, why)
	}
}

// TestDetectTerminalEnv (#2161): the audit and the probe store agree on which
// terminal they are talking about.
func TestDetectTerminalEnv(t *testing.T) {
	env := DetectTerminalEnv(func(k string) string {
		if k == "TERM_PROGRAM" {
			return "iTerm.app"
		}
		return ""
	})
	if env.Terminal != "iTerm.app" || env.GOOS != GOOS {
		t.Fatalf("DetectTerminalEnv = %+v", env)
	}
}
